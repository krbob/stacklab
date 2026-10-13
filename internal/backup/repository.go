package backup

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
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
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = env
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = 5 * time.Second
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if err == syscall.ESRCH {
			return os.ErrProcessDone
		}
		return err
	}
	output := progressBuffer{callback: callback}
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// Remote backends may echo credential-bearing URLs or rclone configuration.
		// Never include raw subprocess output in errors, events, or API responses.
		return nil, fmt.Errorf("%s operation failed; verify repository access and credentials", filepath.Base(name))
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
			return fmt.Errorf("%w: SFTP repository must use sftp://user@host/absolute/path", ErrInvalid)
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
			return runner.RunProgress(ctx, "restic", argv, env, dir, callback)
		}
		output, err := s.runner.Run(ctx, "restic", argv, env, dir)
		if err == nil {
			for _, line := range bytes.Split(output, []byte("\n")) {
				if progress, recognized, _ := decodeTransferProgress(line); recognized {
					callback(progress)
				}
			}
		}
		return output, err
	}
	return s.runner.Run(ctx, "restic", argv, env, dir)
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
	if s.repositoryJob != "" {
		return Destination{}, nil, ErrBusy
	}
	for _, run := range s.state.Runs {
		if run.State == "running" || run.State == "queued" {
			return Destination{}, nil, ErrBusy
		}
	}
	s.activeRepositoryReads++
	var once sync.Once
	release := func() { once.Do(func() { s.mu.Lock(); s.activeRepositoryReads--; s.mu.Unlock() }) }
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
	return result, nil
}

func (s *Service) upload(ctx context.Context, d Destination, p Plan, run Run, stage string) (string, error) {
	// A process can die after restic commits a snapshot but before SQLite records
	// its ID. Recover by the immutable run tag before starting another upload.
	prior, lookupErr := s.restic(ctx, d, "", "snapshots", "--tag", "stacklab,instance:"+s.state.InstanceID+",plan:"+p.ID+",run:"+run.ID)
	if lookupErr != nil {
		return "", lookupErr
	}
	var priorSnapshots []Snapshot
	if err := json.Unmarshal(prior, &priorSnapshots); err != nil {
		return "", fmt.Errorf("invalid restic snapshot lookup response")
	}
	for index := len(priorSnapshots) - 1; index >= 0; index-- {
		if snapshotID.MatchString(priorSnapshots[index].ID) {
			return priorSnapshots[index].ID, nil
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
func (s *Service) retain(ctx context.Context, d Destination, planID string, policy Retention) error {
	if policy.Daily == 0 && policy.Weekly == 0 && policy.Monthly == 0 {
		return nil
	}
	// An AND tag list limits ownership to this plan. Stable host and group-by tags
	// avoid each randomly named prepared directory forming a separate group.
	args := []string{"forget", "--tag", "stacklab,instance:" + s.state.InstanceID + ",plan:" + planID, "--group-by", "host", "--keep-daily", strconv.Itoa(policy.Daily), "--keep-weekly", strconv.Itoa(policy.Weekly), "--keep-monthly", strconv.Itoa(policy.Monthly)}
	if d.PruneSchedule.Frequency == "" || d.PruneSchedule.Frequency == "manual" {
		args = append(args, "--prune")
	}
	_, err := s.restic(ctx, d, "", args...)
	return err
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
