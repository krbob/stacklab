package stacks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"time"

	"stacklab/internal/atomicfile"
)

var ErrMaintenanceRollbackNotApplicable = errors.New("no eligible image regression to roll back")
var ErrMaintenanceRollbackUncertain = errors.New("insufficient readiness evidence for automatic rollback")

var (
	rollbackJobID   = regexp.MustCompile(`^job_[A-Za-z0-9_-]{1,64}$`)
	rollbackImageID = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
)

type rollbackService struct {
	Image    string `json:"image_id"`
	Tag      string `json:"local_tag"`
	Replicas int    `json:"replicas"`
}

type rollbackManifest struct {
	Job         string                     `json:"job_id"`
	Stack       string                     `json:"stack_id"`
	Project     string                     `json:"compose_project"`
	Definition  string                     `json:"resolved_config_sha256"`
	Services    map[string]rollbackService `json:"services"`
	ArchiveHash string                     `json:"archive_sha256,omitempty"`
}

// MaintenanceRollback holds the pre-update identities and readiness probes. Its
// on-disk manifest and Docker archive remain available if the worker is interrupted.
type MaintenanceRollback struct {
	dir      string
	manifest rollbackManifest
	baseline MaintenanceHealthBaseline
	Warning  string
	root     string
}

func (r MaintenanceRollback) Eligible() bool { return len(r.manifest.Services) > 0 }

func (s *ServiceReader) PrepareMaintenanceRollback(ctx context.Context, jobID string, baseline MaintenanceHealthBaseline) (MaintenanceRollback, string, error) {
	var r MaintenanceRollback
	if !rollbackJobID.MatchString(jobID) || !IsValidStackID(baseline.stackID) || baseline.configDigest == "" {
		return r, "", errors.New("invalid rollback job or health baseline")
	}
	r.baseline = baseline
	r.manifest = rollbackManifest{Job: jobID, Stack: baseline.stackID, Project: baseline.project, Definition: baseline.configDigest, Services: map[string]rollbackService{}}
	resolved, err := s.maintenanceHealthConfig(ctx, baseline.stackID, baseline.serviceNames)
	if err != nil || contentSHA256(string(resolved)) != baseline.configDigest {
		return MaintenanceRollback{}, "", errors.New("Compose configuration changed since the pre-update health check")
	}
	canonical, prior, err := s.rollbackConfigHashes(ctx, baseline, resolved)
	if ctx.Err() != nil {
		return MaintenanceRollback{}, "", ctx.Err()
	}
	var skipped, drifted, unverifiable []string
	for _, name := range slices.Sorted(maps.Keys(baseline.targets)) {
		health := baseline.before[name]
		target := baseline.targets[name]
		if !health.ready || health.unverified || health.instances == 0 || target.completionExpected || health.instances != health.containerCount || (target.explicitScale && target.replicas != health.instances) || !rollbackImageID.MatchString(health.imageID) {
			skipped = append(skipped, name)
			continue
		}
		if err != nil || !validServiceHash.MatchString(health.configHash) || canonical[name] == "" || target.externalDefinitions {
			unverifiable = append(unverifiable, name)
			continue
		}
		if health.resourceDrift || (health.configHash != canonical[name] && health.configHash != prior[name]) {
			drifted = append(drifted, name)
			continue
		}
		key := sha256.Sum256([]byte(jobID + "\x00" + baseline.stackID + "\x00" + health.imageID))
		tag := fmt.Sprintf("stacklab.local/maintenance-rollback:%x-%s", key[:16], canonical[name])
		r.manifest.Services[name] = rollbackService{Image: health.imageID, Tag: tag, Replicas: health.instances}
	}
	summary := ""
	if len(skipped) > 0 {
		summary = "No verified uniform running image for " + strings.Join(skipped, ", ") + ". "
	}
	if len(drifted) > 0 {
		summary += "Working Compose configuration differs from the deployed configuration for " + strings.Join(drifted, ", ") + ". These services are excluded from image rollback. "
	}
	if len(unverifiable) > 0 {
		summary += "Could not verify the deployed Compose configuration for " + strings.Join(unverifiable, ", ") + ". These services are excluded from image rollback. "
	}
	r.Warning = strings.TrimSpace(summary)
	if !r.Eligible() {
		return r, summary + "No images eligible for automatic rollback.", nil
	}
	if s.cfg.DataDir == "" {
		return MaintenanceRollback{}, "", errors.New("private state directory required for rollback")
	}
	dataDir, err := filepath.Abs(s.cfg.DataDir)
	if err != nil {
		return MaintenanceRollback{}, "", err
	}
	r.root, err = filepath.Abs(s.cfg.RootDir)
	if err != nil {
		return MaintenanceRollback{}, "", err
	}
	root := filepath.Join(dataDir, "maintenance-rollback")
	for _, path := range []string{root, filepath.Join(root, jobID)} {
		if err := os.Mkdir(path, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return MaintenanceRollback{}, "", err
		}
		if info, err := os.Lstat(path); err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
			return MaintenanceRollback{}, "", errors.New("rollback state directory must be a private real directory")
		}
	}
	r.dir = filepath.Join(root, jobID, baseline.stackID)
	created := false
	if err := os.Mkdir(r.dir, 0700); err == nil {
		created = true
	} else if !errors.Is(err, os.ErrExist) {
		return MaintenanceRollback{}, "", err
	}
	if !created {
		// Retries may reuse only a completed archive for the very same baseline.
		if info, err := os.Lstat(r.dir); err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
			return MaintenanceRollback{}, "", errors.New("invalid rollback archive directory")
		}
		var previous rollbackManifest
		data, err := os.ReadFile(filepath.Join(r.dir, "manifest.json"))
		if err == nil && json.Unmarshal(data, &previous) == nil && previous.ArchiveHash != "" {
			r.manifest.ArchiveHash = previous.ArchiveHash
			if reflect.DeepEqual(previous, r.manifest) {
				if file, err := r.openVerifiedArchive(ctx); err == nil {
					file.Close()
					return r, summary + "Reused image recovery archive in " + r.dir, nil
				}
			}
		}
		return MaintenanceRollback{}, "", fmt.Errorf("existing recovery material requires review in %s", r.dir)
	}
	fail := func(cause error) (MaintenanceRollback, string, error) {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return MaintenanceRollback{}, "", errors.Join(cause, s.ReleaseMaintenanceRollback(cleanupCtx, r))
	}
	if err := r.saveManifest(); err != nil {
		return fail(err)
	}
	if err := atomicfile.SyncDir(filepath.Dir(r.dir)); err != nil {
		return fail(err)
	}
	if err := atomicfile.SyncDir(root); err != nil {
		return fail(err)
	}
	if err := atomicfile.SyncDir(dataDir); err != nil {
		return fail(err)
	}
	for _, pin := range r.pins() {
		existing, err := rollbackLocalTag(ctx, pin.Tag)
		if err != nil {
			return fail(err)
		}
		if existing != "" && existing != pin.Image {
			return fail(fmt.Errorf("rollback tag conflicts with another image: %s", pin.Tag))
		}
		if existing == "" {
			if err := exec.CommandContext(ctx, "docker", "image", "tag", pin.Image, pin.Tag).Run(); err != nil {
				return fail(fmt.Errorf("could not pin image %s", pin.Image))
			}
		}
	}
	names := slices.Sorted(maps.Keys(r.manifest.Services))
	override, err := r.writeOverride(names)
	if err != nil {
		return fail(err)
	}
	// Validate the effective overlay and require pull suppression before taking
	// down any workload. Legacy Compose binaries may not support these options.
	command, args := s.rollbackComposeCommand(ctx, r, override)
	config := exec.CommandContext(ctx, command, append(slices.Clone(args), "config", "--quiet")...)
	config.Dir = stackPaths(s.cfg.RootDir, baseline.stackID).RootPath
	if err := config.Run(); err != nil {
		return fail(errors.New("Compose rejected the image rollback override"))
	}
	help, err := exec.CommandContext(ctx, command, append(args, "up", "--help")...).Output()
	if err != nil || !strings.Contains(string(help), "--pull") || !strings.Contains(string(help), "--no-build") {
		return fail(errors.New("Compose up must support --pull never and --no-build for image rollback"))
	}
	hash, err := r.saveArchive(ctx)
	if err != nil {
		return fail(err)
	}
	r.manifest.ArchiveHash = hash
	if err := r.saveManifest(); err != nil {
		return fail(err)
	}
	versions := make([]string, 0, len(names))
	for _, name := range names {
		versions = append(versions, name+"="+r.manifest.Services[name].Image)
	}
	return r, summary + "Saved original images " + strings.Join(versions, ", ") + " in " + r.dir + ".", nil
}

func (r MaintenanceRollback) saveManifest() error {
	data, err := json.MarshalIndent(r.manifest, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.WriteBytesMode(filepath.Join(r.dir, "manifest.json"), data, ".manifest-*", 0600)
}

func (r MaintenanceRollback) pins() []rollbackService {
	byTag := map[string]rollbackService{}
	for _, svc := range r.manifest.Services {
		byTag[svc.Tag] = svc
	}
	var pins []rollbackService
	for _, tag := range slices.Sorted(maps.Keys(byTag)) {
		pins = append(pins, byTag[tag])
	}
	return pins
}

func rollbackLocalTag(ctx context.Context, tag string) (string, error) {
	out, err := exec.CommandContext(ctx, "docker", "image", "ls", "--no-trunc", "--quiet", tag).Output()
	if err != nil {
		return "", errors.New("could not inspect local rollback image tags")
	}
	value := strings.TrimSpace(string(out))
	if value != "" && !rollbackImageID.MatchString(value) {
		return "", errors.New("ambiguous local rollback image tag")
	}
	return value, nil
}

func (s *ServiceReader) checkRollbackDirectory(r MaintenanceRollback) error {
	dataDir, err := filepath.Abs(s.cfg.DataDir)
	if err != nil || s.cfg.DataDir == "" || !rollbackJobID.MatchString(r.manifest.Job) || !IsValidStackID(r.manifest.Stack) || r.dir != filepath.Join(dataDir, "maintenance-rollback", r.manifest.Job, r.manifest.Stack) {
		return errors.New("invalid rollback state scope")
	}
	for _, path := range []string{filepath.Dir(filepath.Dir(r.dir)), filepath.Dir(r.dir), r.dir} {
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
			return errors.New("rollback artifacts require private real directories")
		}
	}
	return nil
}

func (s *ServiceReader) ReleaseMaintenanceRollback(ctx context.Context, r MaintenanceRollback) error {
	if !r.Eligible() {
		return nil
	}
	if err := s.checkRollbackDirectory(r); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, pin := range r.pins() {
		id, err := rollbackLocalTag(ctx, pin.Tag)
		if err != nil {
			return fmt.Errorf("recovery material retained in %s: %w", r.dir, err)
		}
		// A missing tag is already released; preserve a tag reassigned elsewhere.
		if id != pin.Image {
			continue
		}
		if err := exec.CommandContext(ctx, "docker", "image", "rm", "--force", "--no-prune", pin.Tag).Run(); err != nil {
			return fmt.Errorf("could not release local tag %s; recovery material retained in %s", pin.Tag, r.dir)
		}
	}
	if err := os.RemoveAll(r.dir); err != nil {
		return fmt.Errorf("remove recovery material: %w", err)
	}
	_ = os.Remove(filepath.Dir(r.dir)) // other stacks of this job may still have archives
	return nil
}

func (r MaintenanceRollback) writeOverride(names []string) (string, error) {
	type service struct {
		Image string `json:"image"`
		Pull  string `json:"pull_policy"`
	}
	override := struct {
		Services map[string]service `json:"services"`
	}{Services: map[string]service{}}
	for _, name := range names {
		override.Services[name] = service{Image: r.manifest.Services[name].Tag, Pull: "never"}
	}
	data, err := json.Marshal(override)
	if err != nil {
		return "", err
	}
	path := filepath.Join(r.dir, "compose.rollback.yaml") // JSON is valid Compose YAML.
	return path, atomicfile.WriteBytesMode(path, data, ".override-*", 0600)
}

func (s *ServiceReader) rollbackComposeCommand(ctx context.Context, r MaintenanceRollback, override string) (string, []string) {
	paths := stackPaths(r.root, r.manifest.Stack)
	command, args := composeCommand(ctx, paths.RootPath, paths.ComposeFilePath, paths.EnvFilePath)
	return command, append(args, "--project-name", r.manifest.Project, "-f", override)
}

// MaintenanceImageRecovery identifies the services restored and verified together.
// The value is the pre-recovery candidate ID when all observed replicas agreed.
type MaintenanceImageRecovery struct {
	Summary  string
	restored map[string]string
}

// RollbackMaintenanceImages restores changed eligible services as one deployment
// unit. Healthy dependencies that also changed can be coupled to the failed service.
func (s *ServiceReader) RollbackMaintenanceImages(ctx context.Context, r MaintenanceRollback, failed MaintenanceHealthReport, upFailed bool) (MaintenanceImageRecovery, error) {
	if !r.Eligible() {
		return MaintenanceImageRecovery{}, ErrMaintenanceRollbackNotApplicable
	}
	if err := s.checkRollbackDirectory(r); err != nil {
		return MaintenanceImageRecovery{}, fmt.Errorf("cannot access private rollback records in %s: %w", r.dir, err)
	}
	if err := s.validateStackRoot(r.manifest.Stack, false); err != nil {
		return MaintenanceImageRecovery{}, err
	}
	resolved, err := s.maintenanceHealthConfig(ctx, r.manifest.Stack, r.baseline.serviceNames)
	if err != nil || contentSHA256(string(resolved)) != r.manifest.Definition {
		return MaintenanceImageRecovery{}, fmt.Errorf("Compose configuration changed or is unreadable; original images retained in %s", r.dir)
	}
	current, err := observeMaintenanceHealth(ctx, r.manifest.Project, r.baseline.targets, r.baseline.before)
	if err != nil {
		return MaintenanceImageRecovery{}, fmt.Errorf("cannot establish current image state; recovery material retained in %s: %w", r.dir, err)
	}
	var changed []string
	trigger := false
	uncertain := false
	for _, name := range slices.Sorted(maps.Keys(r.manifest.Services)) {
		orig := r.manifest.Services[name]
		now := current[name]
		if now.imageID == orig.Image && now.containerCount == orig.Replicas {
			continue
		}
		changed = append(changed, name)
		uncertain = uncertain || !now.ready || now.unverified
		if upFailed || (slices.Contains(failed.FailedServices, name) && now.regression) {
			trigger = true
		}
	}
	if !trigger {
		if uncertain {
			err := fmt.Errorf("%w; changed services %s, original images retained in %s", ErrMaintenanceRollbackUncertain, strings.Join(changed, ", "), r.dir)
			return MaintenanceImageRecovery{Summary: err.Error()}, err
		}
		return MaintenanceImageRecovery{}, ErrMaintenanceRollbackNotApplicable
	}
	err = s.restoreRollbackGroup(ctx, r, changed)
	if err != nil {
		return MaintenanceImageRecovery{}, fmt.Errorf("rollback of %s failed, original images retained in %s: %w", strings.Join(changed, ", "), r.dir, err)
	}
	restored := make(map[string]string, len(changed))
	for _, name := range changed {
		restored[name] = current[name].imageID
	}
	return MaintenanceImageRecovery{Summary: "Restored original image IDs and verified readiness for " + strings.Join(changed, ", ") + ".", restored: restored}, nil
}

func (s *ServiceReader) restoreRollbackGroup(ctx context.Context, r MaintenanceRollback, names []string) error {
	if err := r.ensureOriginalImages(ctx); err != nil {
		return err
	}
	override, err := r.writeOverride(names)
	if err != nil {
		return err
	}
	command, args := s.rollbackComposeCommand(ctx, r, override)
	args = append(args, "up", "-d", "--no-build", "--pull", "never", "--no-deps", "--force-recreate")
	for _, name := range names {
		args = append(args, "--scale", fmt.Sprintf("%s=%d", name, r.manifest.Services[name].Replicas))
	}
	args = append(args, "--")
	args = append(args, names...)
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = stackPaths(s.cfg.RootDir, r.manifest.Stack).RootPath
	if err := cmd.Run(); err != nil {
		return errors.New("Compose could not recreate the original replicas")
	}
	baseline := r.baseline
	baseline.targets = make(map[string]maintenanceHealthTarget, len(names))
	for _, name := range names {
		baseline.targets[name] = r.baseline.targets[name]
	}
	observe := func(ctx context.Context, project string, targets map[string]maintenanceHealthTarget, before map[string]maintenanceServiceHealth) (map[string]maintenanceServiceHealth, error) {
		current, err := observeMaintenanceHealth(ctx, project, targets, before)
		if err != nil {
			return nil, err
		}
		for name, health := range current {
			orig := r.manifest.Services[name]
			if health.imageID != orig.Image || health.containerCount != orig.Replicas || health.unverified {
				health.ready = false
				health.summary += " (original image, replica count or readiness not restored)"
				current[name] = health
			}
		}
		return current, nil
	}
	_, err = waitForMaintenanceHealth(ctx, baseline, maintenanceHealthWindow, maintenanceHealthPoll, observe)
	return err
}

func (r MaintenanceRollback) ensureOriginalImages(ctx context.Context) error {
	var missing bool
	for _, pin := range r.pins() {
		id, err := rollbackLocalTag(ctx, pin.Tag)
		if err != nil {
			return err
		}
		if id != "" && id != pin.Image {
			return errors.New("a local rollback tag was reassigned")
		}
		missing = missing || id == ""
	}
	if missing {
		archive, err := r.openVerifiedArchive(ctx)
		if err != nil {
			return err
		}
		defer archive.Close()
		cmd := exec.CommandContext(ctx, "docker", "image", "load")
		cmd.Stdin = archive
		if err := cmd.Run(); err != nil {
			return errors.New("could not reload local recovery archive")
		}
	}
	for _, pin := range r.pins() {
		id, err := rollbackLocalTag(ctx, pin.Tag)
		if err != nil || id != pin.Image {
			return fmt.Errorf("original image unavailable: %s", pin.Image)
		}
	}
	return nil
}

func (r MaintenanceRollback) openVerifiedArchive(ctx context.Context) (*os.File, error) {
	path := filepath.Join(r.dir, "images.tar")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxRollbackArchiveBytes {
		return nil, errors.New("invalid local image archive")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	failed := func(err error) (*os.File, error) { file.Close(); return nil, err }
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return failed(errors.New("local image archive changed during inspection"))
	}
	hash := sha256.New()
	buf := make([]byte, 1024*1024)
	for {
		if err := ctx.Err(); err != nil {
			return failed(err)
		}
		n, err := file.Read(buf)
		hash.Write(buf[:n])
		if err == io.EOF {
			break
		}
		if err != nil {
			return failed(err)
		}
	}
	if hex.EncodeToString(hash.Sum(nil)) != r.manifest.ArchiveHash {
		return failed(errors.New("local image archive checksum mismatch"))
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return failed(err)
	}
	return file, nil
}
