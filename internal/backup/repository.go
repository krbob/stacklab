package backup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"stacklab/internal/atomicfile"
)

var safeID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,100}$`)
var snapshotID = regexp.MustCompile(`^[a-f0-9]{8,64}$`)

// CommandRunner is injectable for failure and cancellation testing. It never
// receives command text assembled for a shell, and its output is never logged.
type CommandRunner interface {
	Run(context.Context, string, []string, []string, string) ([]byte, error)
}
type nativeRunner struct{}

func (nativeRunner) Run(ctx context.Context, name string, args, env []string, dir string) ([]byte, error) {
	return nativeRunner{}.RunProgress(ctx, name, args, env, dir, nil)
}
func (nativeRunner) RunProgress(ctx context.Context, name string, args, env []string, dir string, callback progressCallback) ([]byte, error) {
	if filepath.Base(name) == "restic" && commandIn(args, "backup") {
		transferCtx, cancel := context.WithCancelCause(ctx)
		defer cancel(nil)
		watchdog := newTransferWatchdog(time.Now())
		var finalizing atomic.Bool
		timer := time.AfterFunc(transferProgressTimeout, func() { cancel(transferStallError(finalizing.Load())) })
		defer timer.Stop()
		original := callback
		callback = func(progress transferProgress) {
			if watchdog.observe(time.Now(), progress) {
				finalizing.Store(watchdog.finalizing)
				timer.Reset(time.Until(watchdog.deadline))
			}
			if original != nil {
				original(progress)
			}
		}
		ctx = transferCtx
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = env
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = 10 * time.Second
	done := make(chan struct{})
	defer close(done)
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		signal := syscall.SIGKILL
		pid := -cmd.Process.Pid
		if filepath.Base(name) == "restic" {
			signal, pid = syscall.SIGINT, cmd.Process.Pid
			go func(group int) {
				timer := time.NewTimer(5 * time.Second)
				defer timer.Stop()
				select {
				case <-done:
					return
				case <-timer.C:
					_ = syscall.Kill(-group, syscall.SIGKILL)
				}
			}(cmd.Process.Pid)
		}
		err := syscall.Kill(pid, signal)
		if err == syscall.ESRCH {
			return os.ErrProcessDone
		}
		return err
	}
	output := progressBuffer{callback: callback}
	cmd.Stdout = &output
	diagnostic := cappedBuffer{}
	cmd.Stderr = &diagnostic
	if err := cmd.Run(); err != nil {
		classified := classifyCommandError(name, err, diagnostic.String())
		if ctx.Err() != nil {
			var commandErr *repositoryCommandError
			// Cancellation can arrive after the check has already established
			// corrupt data. Preserve that concrete evidence; an interrupted
			// inconclusive check still records only the context failure.
			if errors.As(classified, &commandErr) && commandErr.Code == "integrity_failed" {
				return nil, classified
			}
			return nil, commandContextError(context.Cause(ctx))
		}
		// Remote backends may echo credential-bearing URLs or rclone configuration.
		// Never include raw subprocess output in errors, events, or API responses.
		return nil, classified
	}
	output.finish()
	if output.output.overflow {
		return nil, fmt.Errorf("%s response exceeded 16 MiB", filepath.Base(name))
	}
	return output.output.Bytes(), nil
}

type cappedBuffer struct {
	bytes.Buffer
	overflow bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	available := (16 << 20) - b.Len()
	if available < len(p) {
		b.overflow = true
		p = p[:max(0, available)]
	}
	_, _ = b.Buffer.Write(p)
	return n, nil
}

func normalizeDestinationInput(in *DestinationInput) {
	if in.CheckSchedule.Frequency == "" {
		in.CheckSchedule.Frequency = "manual"
	}
	if in.PruneSchedule.Frequency == "" {
		in.PruneSchedule.Frequency = "manual"
	}
	if in.CheckReadData == "" {
		in.CheckReadData = "metadata"
	}
}

func validateDestination(in DestinationInput) error {
	if err := validateSchedule(in.CheckSchedule); err != nil {
		return err
	}
	if err := validateSchedule(in.PruneSchedule); err != nil {
		return err
	}
	if in.CheckReadData != "" && in.CheckReadData != "metadata" && in.CheckReadData != "all" && in.CheckReadData != "subset" {
		return fmt.Errorf("%w: check_read_data must be metadata, all, or subset", ErrInvalid)
	}
	if len(strings.TrimSpace(in.Name)) == 0 || len(in.Name) > 200 {
		return fmt.Errorf("%w: destination name is required (max 200 characters)", ErrInvalid)
	}
	if in.Repository == "" || strings.ContainsAny(in.Repository, "\x00\r\n") {
		return fmt.Errorf("%w: repository is required", ErrInvalid)
	}
	switch in.Kind {
	case "local":
		if !filepath.IsAbs(in.Repository) || filepath.Clean(in.Repository) == "/" {
			return fmt.Errorf("%w: local repository requires an absolute directory", ErrInvalid)
		}
		if strings.TrimSpace(in.ExpectedFilesystem) == "" {
			return fmt.Errorf("%w: expected_filesystem is required for local destinations", ErrInvalid)
		}
	case "s3":
		if !strings.HasPrefix(in.Repository, "s3:https://") {
			return fmt.Errorf("%w: S3 repository must use s3:https://endpoint/bucket/prefix", ErrInvalid)
		}
		u, err := url.Parse(strings.TrimPrefix(in.Repository, "s3:"))
		if err != nil || u.User != nil || u.Host == "" || strings.Trim(u.Path, "/") == "" || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("%w: S3 repository must not contain credentials, query strings, or fragments", ErrInvalid)
		}
	case "sftp":
		// URL syntax only: no arbitrary SSH command/configuration injection.
		u, err := url.Parse(strings.TrimPrefix(in.Repository, "sftp:"))
		if !strings.HasPrefix(in.Repository, "sftp://") || err != nil || u.Scheme != "" || u.Hostname() == "" || u.User == nil || u.User.Username() == "" || u.Path == "" {
			return fmt.Errorf("%w: SFTP repository must use sftp://user@host//absolute/path (or /home-relative/path)", ErrInvalid)
		}
		if _, present := u.User.Password(); present || u.RawQuery != "" || u.Fragment != "" || strings.HasPrefix(u.Hostname(), "-") || !regexp.MustCompile(`^[A-Za-z0-9_.-]+$`).MatchString(u.User.Username()) {
			return fmt.Errorf("%w: unsupported SFTP address", ErrInvalid)
		}
	case "rclone":
		if !regexp.MustCompile(`^rclone:[A-Za-z0-9_-]+:[^\r\n]+$`).MatchString(in.Repository) {
			return fmt.Errorf("%w: rclone repository must use rclone:remote:path", ErrInvalid)
		}
	default:
		return fmt.Errorf("%w: unsupported destination kind", ErrInvalid)
	}
	return nil
}

func (s *Service) secretDirectory(id string) string {
	return filepath.Join(s.directory, "credentials", id)
}
func (s *Service) readSecrets(id string) (Secrets, error) {
	var secret Secrets
	data, err := os.ReadFile(filepath.Join(s.secretDirectory(id), "credentials.json"))
	if err != nil {
		return secret, fmt.Errorf("repository credentials unavailable")
	}
	if err = json.Unmarshal(data, &secret); err != nil {
		return secret, fmt.Errorf("repository credentials invalid")
	}
	return secret, nil
}
func (s *Service) writeSecrets(id string, secret Secrets) error {
	if secret.Password == "" || strings.ContainsAny(secret.Password, "\r\n\x00") {
		return fmt.Errorf("%w: repository password must be nonempty and single-line", ErrInvalid)
	}
	dir := s.secretDirectory(id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(secret)
	if err != nil {
		return err
	}
	// Auxiliary files never contain shell fragments; restic/ssh read them directly.
	for _, entry := range []struct{ name, value string }{{"password", secret.Password}, {"ssh_key", secret.SSHPrivateKey}, {"known_hosts", secret.SSHKnownHosts}, {"rclone.conf", secret.RcloneConfig}} {
		if err := atomicfile.WriteStringMode(filepath.Join(dir, entry.name), entry.value, ".credential-*", 0o600); err != nil {
			return err
		}
	}
	return atomicfile.WriteBytesMode(filepath.Join(dir, "credentials.json"), data, ".credential-*", 0o600)
}

func cleanProcessEnvironment() []string {
	// Inherited RESTIC_*, RCLONE_*, AWS_* or SSH configuration must never change
	// repository selection or enable external password hooks.
	keys := []string{"PATH", "HOME", "TMPDIR", "LANG", "LC_ALL", "TZ", "SSL_CERT_FILE", "SSL_CERT_DIR"}
	env := []string{}
	for _, key := range keys {
		if value, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+value)
		}
	}
	return env
}
func shellWord(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
func (s *Service) restic(ctx context.Context, d Destination, dir string, args ...string) ([]byte, error) {
	if len(args) > 0 && (args[0] == "snapshots" || args[0] == "cat" || args[0] == "dump" || args[0] == "ls" || args[0] == "stats" || args[0] == "init") {
		var cancel context.CancelFunc
		budget := 90 * time.Second
		if verification, _ := ctx.Value(verificationContextKey{}).(bool); verification {
			budget = 15 * time.Minute
		}
		ctx, cancel = context.WithTimeout(ctx, budget)
		defer cancel()
	}
	if err := s.validateAvailability(d); err != nil {
		return nil, err
	}
	secret, err := s.readSecrets(d.ID)
	if err != nil {
		return nil, err
	}
	env := cleanProcessEnvironment()
	env = append(env, "RESTIC_PASSWORD_FILE="+filepath.Join(s.secretDirectory(d.ID), "password"), "RESTIC_CACHE_DIR="+filepath.Join(s.directory, "cache", d.ID))
	argv := []string{"--repo", d.Repository, "--json"}
	switch d.Kind {
	case "s3":
		env = append(env, "AWS_ACCESS_KEY_ID="+secret.AccessKeyID, "AWS_SECRET_ACCESS_KEY="+secret.SecretAccessKey, "AWS_SESSION_TOKEN="+secret.SessionToken, "AWS_DEFAULT_REGION="+d.Region)
		if d.Region != "" {
			argv = append(argv, "-o", "s3.region="+d.Region)
		}
	case "rclone":
		env = append(env, "RCLONE_CONFIG="+filepath.Join(s.secretDirectory(d.ID), "rclone.conf"))
	case "sftp":
		u, _ := url.Parse(strings.TrimPrefix(d.Repository, "sftp:"))
		ssh := []string{"ssh", "-F", "/dev/null", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "IdentitiesOnly=yes", "-o", "UserKnownHostsFile=" + filepath.Join(s.secretDirectory(d.ID), "known_hosts"), "-i", filepath.Join(s.secretDirectory(d.ID), "ssh_key")}
		if u.Port() != "" {
			ssh = append(ssh, "-p", u.Port())
		}
		ssh = append(ssh, u.User.Username()+"@"+u.Hostname(), "-s", "sftp")
		quoted := []string{}
		for _, arg := range ssh {
			quoted = append(quoted, shellWord(arg))
		}
		argv = append(argv, "-o", "sftp.command="+strings.Join(quoted, " "))
	}
	argv = append(argv, args...)
	if callback, ok := ctx.Value(progressKey{}).(progressCallback); ok {
		if runner, streaming := s.runner.(progressCommandRunner); streaming {
			return retryRepositoryLock(ctx, func() ([]byte, error) { return runner.RunProgress(ctx, "restic", argv, env, dir, callback) })
		}
		output, err := retryRepositoryLock(ctx, func() ([]byte, error) { return s.runner.Run(ctx, "restic", argv, env, dir) })
		if err == nil {
			for _, line := range bytes.Split(output, []byte("\n")) {
				if progress, recognized, _ := decodeTransferProgress(line); recognized {
					callback(progress)
				}
			}
		}
		return output, err
	}
	return retryRepositoryLock(ctx, func() ([]byte, error) { return s.runner.Run(ctx, "restic", argv, env, dir) })
}

func (s *Service) validateAvailability(d Destination) error {
	if d.Kind != "local" {
		return nil
	}
	repository, err := filepath.EvalSymlinks(d.Repository)
	missing := os.IsNotExist(err)
	if missing {
		repository, err = filepath.EvalSymlinks(filepath.Dir(d.Repository))
	} // init may create last directory only
	if err != nil {
		return fmt.Errorf("local repository parent is unavailable")
	}
	expectedPath := filepath.Clean(d.Repository)
	if missing {
		expectedPath = filepath.Dir(expectedPath)
	}
	if repository != expectedPath {
		return fmt.Errorf("local repository path changed through a symbolic link")
	}
	identity, err := FilesystemIdentity(repository)
	if err != nil {
		return err
	}
	if identity != d.ExpectedFilesystem {
		return fmt.Errorf("local repository filesystem changed or mount is absent (expected %s, found %s)", d.ExpectedFilesystem, identity)
	}
	return nil
}

// FilesystemIdentity returns the device identity displayed during destination
// setup. Revalidation before EVERY repository operation prevents absent mounts
// from silently falling through to the host root filesystem.
func FilesystemIdentity(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", fmt.Errorf("filesystem identification unsupported")
	}
	return strconv.FormatUint(uint64(stat.Dev), 10), nil
}

// beginRepositoryRead pins credentials and configuration while a browser reads
// immutable repository data. Reads may overlap each other, but mutations and
// configuration changes cannot begin until all readers release their leases.
func (s *Service) beginRepositoryRead(ctx context.Context, id string) (Destination, func(), error) {
	if err := ctx.Err(); err != nil {
		return Destination{}, nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.initErr != nil {
		return Destination{}, nil, s.initErr
	}
	d, ok := s.state.Destinations[id]
	if !ok {
		return Destination{}, nil, ErrNotFound
	}
	key := repositoryKey(d)
	if s.repositoryJobs[key] != "" {
		return Destination{}, nil, ErrBusy
	}
	if s.repositoryReads == nil {
		s.repositoryReads = map[string]int{}
	}
	s.repositoryReads[key]++
	s.activeRepositoryReads++
	var once sync.Once
	release := func() {
		once.Do(func() {
			s.mu.Lock()
			s.repositoryReads[key]--
			if s.repositoryReads[key] == 0 {
				delete(s.repositoryReads, key)
			}
			s.activeRepositoryReads--
			s.mu.Unlock()
		})
	}
	return d, release, nil
}

func (s *Service) Snapshots(ctx context.Context, id string) ([]Snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	d, release, err := s.beginRepositoryRead(ctx, id)
	if err != nil {
		return nil, err
	}
	defer release()
	output, err := s.restic(ctx, d, "", "snapshots", "--tag", "stacklab")
	if err != nil {
		return nil, err
	}
	result := []Snapshot{}
	if err = json.Unmarshal(output, &result); err != nil {
		return nil, fmt.Errorf("invalid restic snapshot response")
	}
	if err = s.reconcileInventory(ctx, d, result); err != nil {
		return nil, err
	}
	s.mu.Lock()
	for i := range result {
		result[i].RestoreTestedAt = s.restoreTestedAtLocked(d, result[i].ID)
		result[i].RecoveryStatus, result[i].ClassifiedAt = "unknown", nil
		if classification, found := s.snapshotClassificationLocked(d.ID, result[i].ID); found {
			result[i].RecoveryStatus = classification.Status
			result[i].ClassifiedAt = &classification.ClassifiedAt
		}
	}
	s.mu.Unlock()
	return result, nil
}

func (s *Service) upload(ctx context.Context, d Destination, p Plan, run Run, stage string) (string, error) {
	// A process can die after restic commits a snapshot but before SQLite records
	// its ID. Recover by the immutable run tag before starting another upload.
	prior, lookupErr := s.restic(context.WithValue(ctx, verificationContextKey{}, true), d, "", "snapshots", "--tag", "stacklab,instance:"+s.state.InstanceID+",plan:"+p.ID+",run:"+run.ID)
	if lookupErr != nil {
		return "", lookupErr
	}
	var priorSnapshots []Snapshot
	if err := json.Unmarshal(prior, &priorSnapshots); err != nil {
		return "", fmt.Errorf("invalid restic snapshot lookup response")
	}
	for index := len(priorSnapshots) - 1; index >= 0; index-- {
		if snapshotID.MatchString(priorSnapshots[index].ID) {
			if err := s.verifyRecoveryObjects(ctx, d, priorSnapshots[index].ID, stage); err == nil {
				return priorSnapshots[index].ID, nil
			} else if !errors.Is(err, errIncompleteSnapshot) {
				return "", fmt.Errorf("existing snapshot verification unavailable: %w", err)
			}
		}
	}
	output, err := s.restic(ctx, d, stage, "backup", "--tag", "stacklab", "--tag", "instance:"+s.state.InstanceID, "--tag", "plan:"+p.ID, "--tag", "run:"+run.ID, "--host", "stacklab", "--time", run.CapturedAt.In(time.Local).Format("2006-01-02 15:04:05"), ".")
	if err != nil {
		return "", err
	}
	for _, line := range bytes.Split(output, []byte("\n")) {
		var event struct {
			SnapshotID string `json:"snapshot_id"`
		}
		if json.Unmarshal(line, &event) == nil && snapshotID.MatchString(event.SnapshotID) {
			return event.SnapshotID, nil
		}
	}
	return "", fmt.Errorf("restic did not report a completed snapshot")
}

func (s *Service) check(ctx context.Context, d Destination, policy string) error {
	args := []string{"check"}
	switch policy {
	case "", "metadata":
	case "all":
		args = append(args, "--read-data")
	case "subset":
		args = append(args, "--read-data-subset=10%")
	default:
		return fmt.Errorf("%w: read_data must be metadata, all, or subset", ErrInvalid)
	}
	_, err := s.restic(ctx, d, "", args...)
	return err
}

var errTransferStalled = errors.New("restic transfer exceeded its bounded progress timeout; check destination connectivity and retry")

type repositoryCommandError struct {
	Code     string
	ExitCode int
	message  string
	cause    error
}

func (e *repositoryCommandError) Error() string { return e.message }
func (e *repositoryCommandError) Unwrap() error { return e.cause }
func commandContextError(cause error) error {
	code, message := "timeout", "repository operation timed out; check connectivity and retry"
	if errors.Is(cause, context.Canceled) {
		code, message = "cancelled", "repository operation was cancelled"
	}
	if errors.Is(cause, errTransferStalled) {
		code, message = "unavailable", cause.Error()
	}
	return &repositoryCommandError{Code: code, ExitCode: -1, message: message + " (" + code + ")", cause: cause}
}
func commandIn(args []string, wanted string) bool {
	for _, arg := range args {
		if arg == wanted {
			return true
		}
	}
	return false
}

var resticEOFDiagnostic = regexp.MustCompile(`\beof\b`)
var resticHTTPFailureDiagnostic = regexp.MustCompile(`\b(?:http(?:/[^ ]+)?[ :]+|status(?: code)?[ :=]+)(?:500|502|503|504)\b`)
var resticMissingPackDiagnostic = regexp.MustCompile(`pack [a-f0-9]{8,64}: does not exist`)
var resticPackSizeDiagnostic = regexp.MustCompile(`pack [a-f0-9]{8,64}: unexpected file size:`)
var resticMissingBlobDiagnostic = regexp.MustCompile(`id [a-f0-9]{8,64} not found in repository`)
var resticInvalidNodeDiagnostic = regexp.MustCompile(`node .+ with invalid type `)
var resticPackErrorsDiagnostic = regexp.MustCompile(`pack [a-f0-9]{8,64} contains [0-9]+ errors:`)

// Restic versions emit either plain stderr or JSON error records whose message
// can contain several lines. Retry warnings describe a recoverable attempt,
// not the final outcome of the listing or read; terminal errors follow them.
func resticDiagnosticText(diagnostic string) string {
	var lines []string
	for _, line := range strings.Split(diagnostic, "\n") {
		var record struct {
			Message string `json:"message"`
		}
		if strings.HasPrefix(strings.TrimSpace(line), "{") && json.Unmarshal([]byte(line), &record) == nil && record.Message != "" {
			line = record.Message
		}
		for _, message := range strings.Split(strings.ToLower(line), "\n") {
			if strings.Contains(message, "returned error, retrying after") {
				continue
			}
			lines = append(lines, message)
		}
	}
	return strings.Join(lines, "\n")
}

func containsDiagnostic(diagnostic string, patterns ...string) bool {
	for _, pattern := range patterns {
		if strings.Contains(diagnostic, pattern) {
			return true
		}
	}
	return false
}

func resticTransportFailure(diagnostic string) bool {
	return containsDiagnostic(diagnostic, "connection refused", "connection reset", "connection closed", "connection lost", "no such host", "timeout", "timed out", "network is unreachable", "no route to host", "broken pipe", "server closed idle connection", "service unavailable", "serviceunavailable", "internal server error", "bad gateway", "slowdown", "requesttimeout") || resticEOFDiagnostic.MatchString(diagnostic) || resticHTTPFailureDiagnostic.MatchString(diagnostic)
}

func resticInventoryReadFailure(diagnostic string) bool {
	for _, line := range strings.Split(diagnostic, "\n") {
		if containsDiagnostic(line, "list(", "load indexes", "load index ", "load(<index/") && containsDiagnostic(line, "error", "failed") {
			return true
		}
		if !resticTransportFailure(line) && !containsDiagnostic(line, "permission denied", "access denied", "no space left on device", "quota exceeded") {
			continue
		}
		// These are reads of individual packs after inventory collection. An
		// unrelated failed download cannot invalidate a successful listing.
		if containsDiagnostic(line, "download error:", "load(<data/", "load(<pack/") {
			continue
		}
		// A naked EOF or an unrecognised read failure may still come from a
		// partial listing. Do not invent missing-object proof from it.
		return true
	}
	return false
}

func resticIntegrityFailure(diagnostic string, inventoryReadFailure bool) bool {
	// Authentication, decoded-object and hash errors establish bad repository
	// data even when another pack failed to download in the same check. Avoid the
	// generic "does not match": SSH host keys and TLS identities use it too.
	if resticPackSizeDiagnostic.MatchString(diagnostic) || resticInvalidNodeDiagnostic.MatchString(diagnostic) || containsDiagnostic(diagnostic, "ciphertext verification failed", "invalid data returned", "unexpected pack id", "pack id does not match", "pack header size does not match", "hash does not match", "checksum does not match", "index for pack contains gaps / overlapping blobs", "has nil blob list", "has null id", "has no subtree", "subtree id is null", "node with empty name") {
		return true
	}
	for _, line := range strings.Split(diagnostic, "\n") {
		// In the local and SFTP backends this means normal EOF before the
		// expected object length, rather than an interrupted connection.
		if strings.Contains(line, "file is too short") && containsDiagnostic(line, "load(<data/", "download error:", "pack ") {
			return true
		}
	}
	// Restic can report absent packs and index references after a failed partial
	// listing/index load. Without a successful read these are not decisive proof.
	return !inventoryReadFailure && (resticMissingPackDiagnostic.MatchString(diagnostic) || resticMissingBlobDiagnostic.MatchString(diagnostic) || containsDiagnostic(diagnostic, "not found in index", "pack is empty or not indexed", "is not contained in index or position is incorrect"))
}

func resticUnknownRepositoryFailure(diagnostic string) bool {
	treeErrors := false
	for _, line := range strings.Split(diagnostic, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "error for tree ") {
			treeErrors = true
			if separator := strings.Index(trimmed, ":"); separator >= 0 {
				trimmed = strings.TrimSpace(trimmed[separator+1:])
			}
			if trimmed == "" {
				continue
			}
		} else if trimmed == "" || strings.HasPrefix(trimmed, "the repository ") || strings.HasPrefix(trimmed, "fatal:") {
			treeErrors = false
		}
		// Missing references may be artifacts of a failed inventory read. They
		// are handled by resticIntegrityFailure, rather than promoted from an
		// availability failure to a new unresolved-repository condition here.
		if resticMissingBlobDiagnostic.MatchString(trimmed) || containsDiagnostic(trimmed, "not found in index", "is not contained in index or position is incorrect") {
			continue
		}
		if treeErrors && !resticTransportFailure(trimmed) && !containsDiagnostic(trimmed, "permission denied", "access denied", "no space left on device", "quota exceeded") {
			return true
		}
		if resticPackErrorsDiagnostic.MatchString(line) && !resticTransportFailure(line) && !containsDiagnostic(line, "permission denied", "access denied", "no space left on device", "quota exceeded") {
			return true
		}
	}
	return false
}

func classifyCommandError(name string, err error, diagnostic string) error {
	code := -1
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		code = exit.ExitCode()
	}
	lower := resticDiagnosticText(diagnostic)
	kind, message := "operation_failed", "operation failed; check destination access and retry"
	if filepath.Base(name) == "restic" {
		transportFailure := resticTransportFailure(lower)
		switch {
		case code == 3:
			kind, message = "incomplete_snapshot", "backup could not read all recovery objects; an incomplete snapshot may exist and is not a verified copy"
		case code == 10:
			kind, message = "repository_missing", "repository is missing or uninitialized; verify the address before initializing"
		case code == 11:
			kind, message = "repository_locked", "repository is locked by another operation; wait for it to finish and inspect stale locks before retrying"
		case code == 12:
			kind, message = "credentials_rejected", "repository rejected the encryption password; verify its saved credentials"
		case resticIntegrityFailure(lower, resticInventoryReadFailure(lower)):
			kind, message = "integrity_failed", "repository integrity verification failed; inspect the repository and preserve existing copies"
		case resticUnknownRepositoryFailure(lower):
			kind, message = "check_inconclusive", "repository check was inconclusive; preserve existing copies and run the required check before resuming maintenance"
		case strings.Contains(lower, "repository is already locked") || strings.Contains(lower, "unable to create lock"):
			kind, message = "repository_locked", "repository is locked by another operation; wait for it to finish and inspect stale locks before retrying"
		case strings.Contains(lower, "wrong password") || strings.Contains(lower, "no key found"):
			kind, message = "credentials_rejected", "repository rejected the encryption password; verify its saved credentials"
		case strings.Contains(lower, "no space left on device") || strings.Contains(lower, "quota exceeded"):
			kind, message = "storage_full", "destination storage or quota is full"
		case transportFailure:
			kind, message = "unavailable", "destination connection failed; check its address, network and availability"
		case strings.Contains(lower, "permission denied") || strings.Contains(lower, "access denied"):
			kind, message = "permission_denied", "destination denied access; verify transport credentials and directory permissions"
		case strings.Contains(lower, "is there a repository at") || strings.Contains(lower, "unable to open config file"):
			kind, message = "repository_missing", "repository is missing or uninitialized; verify the address before initializing"
		case containsDiagnostic(lower, "repository contains errors", "repository contains damaged pack files", "repository is damaged and must be repaired", "repository index is damaged and must be repaired"):
			// Restic prints these summaries for transport failures too. Only
			// unexplained summaries create a hold; they never prove corruption.
			kind, message = "check_inconclusive", "repository check was inconclusive; preserve existing copies and run the required check before resuming maintenance"
		}
	}
	// Diagnostics select fixed, actionable text only. Neither remote error bodies
	// nor paths, URLs, command arguments or credential material reach the API.
	return &repositoryCommandError{Code: kind, ExitCode: code, message: fmt.Sprintf("%s: %s (%s)", filepath.Base(name), message, kind)}
}
func retryRepositoryLock(ctx context.Context, call func() ([]byte, error)) ([]byte, error) {
	for attempt := 0; ; attempt++ {
		output, err := call()
		var diagnostic *repositoryCommandError
		if attempt >= 2 || !errors.As(err, &diagnostic) || diagnostic.Code != "repository_locked" {
			return output, err
		}
		timer := time.NewTimer(time.Duration(attempt+1) * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, commandContextError(ctx.Err())
		case <-timer.C:
		}
	}
}
