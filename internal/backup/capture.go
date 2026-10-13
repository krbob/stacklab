package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"stacklab/internal/atomicfile"
	"stacklab/internal/limitedio"
	"stacklab/internal/stacks"
)

type stopJournal struct {
	Version     int      `json:"version"`
	RunID       string   `json:"run_id"`
	Writers     []Writer `json:"writers"`
	HoldStopped bool     `json:"hold_stopped,omitempty"`
}

func (c *NativeCapture) journalPath(runID string) string {
	return filepath.Join(c.cfg.DataDir, "backup", "journals", sourceID(runID, "", "", "")+".json")
}

func (c *NativeCapture) saveJournal(runID string, writers []Writer) error {
	return c.saveStopJournal(stopJournal{Version: 1, RunID: runID, Writers: writers})
}

func (c *NativeCapture) saveStopJournal(journal stopJournal) error {
	path := c.journalPath(journal.RunID)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.Marshal(journal)
	if err != nil {
		return err
	}
	return atomicfile.WriteBytesMode(path, data, ".journal-*", 0600)
}

func (c *NativeCapture) resume(ctx context.Context, journal stopJournal) error {
	var failures []error
	for _, writer := range journal.Writers {
		if !writer.Running {
			continue
		}
		if _, err := c.command(ctx, "docker", "start", writer.ContainerID); err != nil {
			failures = append(failures, fmt.Errorf("restart writer %s: %w", writer.ContainerID, err))
		}
	}
	if len(failures) > 0 {
		return errors.Join(failures...)
	}
	return c.removeJournal(journal.RunID)
}

func (c *NativeCapture) removeJournal(runID string) error {
	path := c.journalPath(runID)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func (c *NativeCapture) validJournal(journal stopJournal, filename string) bool {
	if journal.Version != 1 || journal.RunID == "" || filepath.Base(c.journalPath(journal.RunID)) != filename {
		return false
	}
	seen := map[string]bool{}
	for _, writer := range journal.Writers {
		id := writer.ContainerID
		if len(id) == 0 || len(id) > 128 || strings.HasPrefix(id, "-") || strings.ContainsAny(id, " /\\\x00\r\n\t") || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}

// Recover compensates durable stop intents before scheduler admission. Failed
// restarts keep their journal and must prevent a new capture from proceeding.
func (c *NativeCapture) Recover(ctx context.Context) error {
	dir := filepath.Join(c.cfg.DataDir, "backup", "journals")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var failures []error
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := limitedio.ReadFile(filepath.Join(dir, entry.Name()), 1024*1024)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		var journal stopJournal
		if entry.Type()&os.ModeSymlink != 0 {
			failures = append(failures, errors.New("backup service recovery journal must not be a symlink"))
			continue
		}
		if err = json.Unmarshal(data, &journal); err != nil || !c.validJournal(journal, entry.Name()) {
			failures = append(failures, errors.New("invalid backup service recovery journal"))
			continue
		}
		if journal.HoldStopped {
			if removeErr := c.removeJournal(journal.RunID); removeErr != nil {
				failures = append(failures, removeErr)
			}
			continue
		}
		if err = c.resume(ctx, journal); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func uniqueRunningWriters(sources []ManifestSource) []Writer {
	byID := map[string]Writer{}
	for _, source := range sources {
		for _, writer := range source.Writers {
			if writer.Running {
				byID[writer.ContainerID] = writer
			}
		}
	}
	writers := make([]Writer, 0, len(byID))
	for _, writer := range byID {
		writers = append(writers, writer)
	}
	sort.Slice(writers, func(i, j int) bool { return writers[i].ContainerID < writers[j].ContainerID })
	return writers
}

func configureCaptureProcess(cmd *exec.Cmd) {
	// SIGTERM lets sudo and the privileged helper relay cancellation to tar.
	// WaitDelay bounds a subprocess that does not honor graceful cancellation.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }
	cmd.WaitDelay = 5 * time.Second
}

func (c *NativeCapture) stopWriter(ctx context.Context, writer Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// A Docker stop request continues inside the daemon if its CLI is killed.
	// Wait for that bounded request before compensation can issue docker start.
	stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 40*time.Second)
	defer cancel()
	_, err := c.command(stopCtx, "docker", "stop", "--time", "30", writer.ContainerID)
	return err
}

func (c *NativeCapture) Prepare(ctx context.Context, plan Plan, runID, stagingDir string) (manifest Manifest, err error) {
	manifest = Manifest{Version: 1, RunID: runID, PlanID: plan.ID, Consistency: plan.Consistency, StacklabVersion: stacks.AppVersion}
	if len(plan.Sources) == 0 {
		return manifest, errors.New("backup requires at least one source")
	}
	if plan.Consistency != "files" && plan.Consistency != "stop" {
		return manifest, errors.New("unsupported backup consistency mode")
	}
	if err = c.Recover(ctx); err != nil {
		return manifest, err
	}
	sources, err := c.Preview(ctx, plan)
	if err != nil {
		return manifest, err
	}
	if err = os.MkdirAll(filepath.Join(stagingDir, "sources"), 0700); err != nil {
		return manifest, err
	}
	maxBytes := plan.StagingMaxBytes
	if maxBytes <= 0 {
		maxBytes = 10 * 1024 * 1024 * 1024
	}
	if err = checkCaptureSpace(stagingDir, maxBytes); err != nil {
		return manifest, err
	}
	captureCtx := ctx
	var resumeCapturedWriters func() error
	if plan.Consistency == "stop" {
		seconds := plan.StopTimeoutSeconds
		if seconds <= 0 {
			seconds = 300
		}
		var cancel context.CancelFunc
		captureCtx, cancel = context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
		defer cancel()
		writers := uniqueRunningWriters(sources)
		if len(writers) > 0 {
			if err = c.saveJournal(runID, writers); err != nil {
				return manifest, err
			}
			stoppedAt := time.Now()
			resumed := false
			resumeCapturedWriters = func() error {
				if resumed {
					return nil
				}
				resumeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				defer cancel()
				resumeErr := c.resume(resumeCtx, stopJournal{Version: 1, RunID: runID, Writers: writers})
				manifest.ServiceDowntimeSeconds = int64(time.Since(stoppedAt).Seconds())
				resumed = resumeErr == nil
				return resumeErr
			}
			defer func() {
				err = errors.Join(err, resumeCapturedWriters())
			}()
			for _, writer := range writers {
				if stopErr := c.stopWriter(captureCtx, writer); stopErr != nil {
					return manifest, fmt.Errorf("stop selected writer: %w", stopErr)
				}
			}
		}
	} else {
		manifest.Warnings = append(manifest.Warnings, "Live file capture is not an application-consistent database backup; use prepared dumps or stop writers.")
	}
	var used int64
	for index := range sources {
		if err = captureCtx.Err(); err != nil {
			return manifest, err
		}
		path := sources[index].OriginalPath
		cleanup := func() {}
		var scratch int64
		if sources[index].Source.Kind == "stacklab" {
			path, cleanup, scratch, err = c.prepareState(captureCtx, stagingDir, (maxBytes-used)/2)
			if err != nil {
				return manifest, err
			}
		}
		archive := filepath.Join(stagingDir, filepath.FromSlash(sources[index].SavedPath))
		var size int64
		size, err = c.archive(captureCtx, sources[index], path, archive, maxBytes-used-scratch)
		cleanup()
		if err != nil {
			return manifest, fmt.Errorf("capture %s: %w", sources[index].Source.ID, err)
		}
		sources[index].SizeBytes = size
		file, openErr := os.Open(archive)
		if openErr != nil {
			return manifest, openErr
		}
		validationErr := ValidateArchive(captureContextReader{ctx: captureCtx, reader: file}, "")
		if err = errors.Join(validationErr, file.Close()); err != nil {
			return manifest, fmt.Errorf("captured source uses unsupported recovery metadata: %w", err)
		}
		used += size
	}
	manifest.CapturedAt = time.Now().UTC()
	// Hashing the captured bytes does not require live writers to stay stopped.
	// Keep their downtime bounded by capture and archive validation only.
	if resumeCapturedWriters != nil {
		if err = resumeCapturedWriters(); err != nil {
			return manifest, err
		}
	}
	for index := range sources {
		archive := filepath.Join(stagingDir, filepath.FromSlash(sources[index].SavedPath))
		indexSize, indexErr := writeSourceIndex(ctx, archive, stagingDir, &sources[index], maxBytes-used)
		if indexErr != nil {
			return manifest, fmt.Errorf("index captured source: %w", indexErr)
		}
		used += indexSize
	}
	manifest.Sources = sources
	images := map[string]struct{}{}
	for _, source := range plan.Sources {
		if source.StackID != "" {
			compose, composeErr := c.compose(ctx, source.StackID)
			if composeErr == nil {
				for _, service := range compose.Services {
					if service.Image != "" {
						images[service.Image] = struct{}{}
					}
				}
			}
		}
	}
	for image := range images {
		manifest.Images = append(manifest.Images, image)
	}
	sort.Strings(manifest.Images)
	if len(manifest.Images) > 0 {
		manifest.Warnings = append(manifest.Warnings, "Image references are prerequisites, not saved image layers. External networks, build inputs and secrets may need separate recovery.")
	}
	return manifest, nil
}

type captureLimitWriter struct {
	file               *os.File
	remaining, written int64
	cancel             context.CancelFunc
	limited            bool
}

type captureContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r captureContextReader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(data)
}

func (w *captureLimitWriter) Write(data []byte) (int, error) {
	if int64(len(data)) > w.remaining {
		w.limited = true
		w.cancel()
		return 0, errors.New("prepared recovery set exceeds staging byte budget")
	}
	if err := checkCaptureSpace(filepath.Dir(w.file.Name()), int64(len(data))); err != nil {
		w.cancel()
		return 0, err
	}
	n, err := w.file.Write(data)
	w.remaining -= int64(n)
	w.written += int64(n)
	return n, err
}

func tarExecutable() string {
	if path, err := exec.LookPath("gtar"); err == nil {
		return path
	}
	return "tar"
}

func archiveArgs(source string, excludes []string) ([]string, error) {
	info, err := os.Stat(source)
	if err != nil {
		return nil, err
	}
	args := []string{"--create", "--file=-", "--format=pax", "--acls", "--xattrs", "--numeric-owner", "--sparse", "--one-file-system"}
	for _, exclude := range excludes {
		if strings.ContainsRune(exclude, 0) || filepath.IsAbs(exclude) || strings.Contains(exclude, "../") {
			return nil, errors.New("exclusions must be relative patterns")
		}
		args = append(args, "--exclude="+exclude)
	}
	if info.IsDir() {
		args = append(args, "--directory="+source, "--", ".")
	} else if info.Mode().IsRegular() {
		args = append(args, "--directory="+filepath.Dir(source), "--", filepath.Base(source))
	} else {
		return nil, errors.New("source must be a regular file or directory")
	}
	return args, nil
}

func (c *NativeCapture) archive(ctx context.Context, source ManifestSource, path, target string, budget int64) (int64, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var name string
	var args []string
	if c.cfg.BackupHelperPath != "" && source.Source.Kind != "stacklab" {
		name = c.cfg.BackupHelperPath
		args = []string{"archive"}
		if source.VolumeName != "" {
			args = append(args, "--volume", source.VolumeName)
		} else {
			args = append(args, "--path", path)
		}
		for _, exclude := range source.Source.Excludes {
			args = append(args, "--exclude", exclude)
		}
		if c.cfg.BackupUseSudo {
			args = append([]string{"-n", name}, args...)
			name = "sudo"
		}
	} else {
		var err error
		args, err = archiveArgs(path, source.Source.Excludes)
		if err != nil {
			return 0, fmt.Errorf("source inaccessible; configure the backup helper for protected data: %w", err)
		}
		name = tarExecutable()
	}
	file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return 0, err
	}
	writer := &captureLimitWriter{file: file, remaining: budget, cancel: cancel}
	cmd := exec.CommandContext(ctx, name, args...)
	configureCaptureProcess(cmd)
	cmd.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin", "LC_ALL=C"}
	cmd.Stdout = writer
	stderr := limitedio.NewBuffer(32 * 1024)
	cmd.Stderr = stderr
	runErr := cmd.Run()
	syncErr := file.Sync()
	closeErr := file.Close()
	if runErr != nil {
		_ = os.Remove(target)
		if writer.limited {
			return writer.written, errors.New("staging budget exceeded")
		}
		return writer.written, fmt.Errorf("native archive failed: %s: %w", strings.TrimSpace(stderr.String()), runErr)
	}
	return writer.written, errors.Join(syncErr, closeErr)
}

func copyStateFile(ctx context.Context, source, target string, remaining *int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return err
	}
	if info.Size() > *remaining {
		return errors.New("private state exceeds staging budget")
	}
	if err = checkCaptureSpace(filepath.Dir(target), info.Size()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return err
	}
	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(output, io.LimitReader(captureContextReader{ctx: ctx, reader: input}, *remaining+1))
	*remaining -= written
	if *remaining < 0 {
		copyErr = errors.Join(copyErr, errors.New("private state exceeds staging budget"))
	}
	return errors.Join(copyErr, output.Close())
}

func (c *NativeCapture) prepareState(ctx context.Context, stagingDir string, budget int64) (string, func(), int64, error) {
	if c.sqliteBackup == nil {
		return "", nil, 0, errors.New("SQLite online backup is unavailable")
	}
	var databaseBytes int64
	for _, dbPath := range []string{c.cfg.DatabasePath, c.cfg.DatabasePath + "-wal"} {
		if info, err := os.Stat(dbPath); err == nil {
			databaseBytes += info.Size()
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", nil, 0, err
		}
	}
	if databaseBytes > budget {
		return "", nil, 0, errors.New("SQLite snapshot exceeds staging budget")
	}
	path, err := os.MkdirTemp(stagingDir, "state-")
	if err != nil {
		return "", nil, 0, err
	}
	cleanup := func() { _ = os.RemoveAll(path) }
	if err = c.sqliteBackup(ctx, filepath.Join(path, "stacklab.db")); err != nil {
		cleanup()
		return "", nil, 0, err
	}
	remaining := budget
	if info, statErr := os.Stat(filepath.Join(path, "stacklab.db")); statErr != nil {
		cleanup()
		return "", nil, 0, statErr
	} else {
		remaining -= info.Size()
	}
	if remaining < 0 {
		cleanup()
		return "", nil, 0, errors.New("SQLite snapshot exceeds staging budget")
	}
	stateRoot, _ := filepath.Abs(c.cfg.DataDir)
	database, _ := filepath.Abs(c.cfg.DatabasePath)
	err = filepath.WalkDir(stateRoot, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, _ := filepath.Rel(stateRoot, current)
		if rel == "." {
			return nil
		}
		if rel == filepath.Join("backup", "staging") || rel == filepath.Join("backup", "cache") || rel == filepath.Join("backup", "journals") || rel == filepath.Join("backup", "restores") || pathContains(path, current) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if current == database || current == database+"-wal" || current == database+"-shm" {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("private state symlink is unsupported: %s", rel)
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		return copyStateFile(ctx, current, filepath.Join(path, "private-state", rel), &remaining)
	})
	if err != nil {
		cleanup()
		return "", nil, 0, err
	}
	serviceConfig := c.serviceConfigPath
	if serviceConfig == "" {
		serviceConfig = "/etc/stacklab/stacklab.env"
	}
	if _, statErr := os.Stat(serviceConfig); statErr == nil {
		if err = copyStateFile(ctx, serviceConfig, filepath.Join(path, "stacklab.env"), &remaining); errors.Is(err, os.ErrPermission) && c.cfg.BackupHelperPath != "" {
			name, args := c.helperInvocation("service-config")
			var content []byte
			content, err = c.command(ctx, name, args...)
			if err == nil {
				if int64(len(content)) > remaining || len(content) > 64*1024 {
					err = errors.New("service configuration exceeds staging budget")
				} else {
					remaining -= int64(len(content))
					err = os.WriteFile(filepath.Join(path, "stacklab.env"), content, 0600)
				}
			}
		}
		if err != nil {
			cleanup()
			return "", nil, 0, err
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		cleanup()
		return "", nil, 0, statErr
	}
	metadata, _ := json.Marshal(map[string]any{"version": 1, "root_dir": c.cfg.RootDir, "data_dir": c.cfg.DataDir, "database_path": c.cfg.DatabasePath})
	remaining -= int64(len(metadata))
	if remaining < 0 {
		cleanup()
		return "", nil, 0, errors.New("state metadata exceeds staging budget")
	}
	if err = os.WriteFile(filepath.Join(path, "layout.json"), metadata, 0600); err != nil {
		cleanup()
		return "", nil, 0, err
	}
	return path, cleanup, budget - remaining, nil
}

func (c *NativeCapture) helperInvocation(args ...string) (string, []string) {
	if c.cfg.BackupUseSudo {
		return "sudo", append([]string{"-n", c.cfg.BackupHelperPath}, args...)
	}
	return c.cfg.BackupHelperPath, args
}
