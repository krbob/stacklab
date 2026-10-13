package backup

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"stacklab/internal/atomicfile"
	"stacklab/internal/config"
	"stacklab/internal/jobs"
	"stacklab/internal/store"
)

const settingKey = "backup.v1"

type persistedRun struct {
	Run
	PlanSnapshot Plan `json:"plan_snapshot"`
}
type state struct {
	RestoreOrder []string                 `json:"restore_order,omitempty"`
	Restores     map[string]RestoreResult `json:"restores"`
	InstanceID   string                   `json:"instance_id"`
	Version      int                      `json:"version"`
	Destinations map[string]Destination   `json:"destinations"`
	Plans        map[string]Plan          `json:"plans"`
	Runs         map[string]persistedRun  `json:"runs"`
}

type Service struct {
	mu            sync.Mutex
	cfg           config.Config
	jobs          *jobs.Service
	capture       Capturer
	runner        CommandRunner
	directory     string
	state         state
	reviews       map[string]restoreChallenge
	repositoryJob string
	initErr       error
	now           func() time.Time
}

func NewService(cfg config.Config, jobService *jobs.Service, capture Capturer) *Service {
	dir, err := filepath.Abs(filepath.Join(cfg.DataDir, "backup"))
	s := &Service{cfg: cfg, jobs: jobService, capture: capture, runner: nativeRunner{}, directory: dir, now: func() time.Time { return time.Now().UTC() }, initErr: err, state: state{Version: 1, Destinations: map[string]Destination{}, Plans: map[string]Plan{}, Runs: map[string]persistedRun{}}}
	if err != nil {
		return s
	}
	for _, name := range []string{"", "credentials", "staging", "cache", "restores"} {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(path, 0o700); err != nil {
			s.initErr = err
			return s
		}
		if err := os.Chmod(path, 0o700); err != nil {
			s.initErr = err
			return s
		}
	}
	canonicalDirectory, canonicalErr := filepath.EvalSymlinks(s.directory)
	if canonicalErr != nil {
		s.initErr = canonicalErr
		return s
	}
	s.directory = canonicalDirectory
	raw, found, err := jobService.Store().AppSetting(context.Background(), settingKey)
	if err != nil {
		s.initErr = err
		return s
	}
	if found {
		if err = json.Unmarshal([]byte(raw), &s.state); err != nil {
			s.initErr = fmt.Errorf("invalid backup state")
			return s
		}
		if s.state.Version != 1 {
			s.initErr = fmt.Errorf("unsupported backup state version")
			return s
		}
	}
	if s.state.InstanceID == "" {
		s.state.InstanceID = newID("instance_")
		if err := s.persist(context.Background()); err != nil {
			s.initErr = err
			return s
		}
	}
	if s.state.Restores == nil {
		s.state.Restores = map[string]RestoreResult{}
	}
	if s.state.Destinations == nil {
		s.state.Destinations = map[string]Destination{}
	}
	if s.state.Plans == nil {
		s.state.Plans = map[string]Plan{}
	}
	if s.state.Runs == nil {
		s.state.Runs = map[string]persistedRun{}
	}
	return s
}

func (s *Service) persist(ctx context.Context) error {
	if s.initErr != nil {
		return s.initErr
	}
	s.pruneMetadataLocked()
	data, err := json.Marshal(s.state)
	if err != nil {
		return err
	}
	return s.jobs.Store().SetAppSetting(ctx, settingKey, string(data), s.now())
}
func newID(prefix string) string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return prefix + hex.EncodeToString(b)
}
func clone[T any](value T) T {
	data, _ := json.Marshal(value)
	var result T
	_ = json.Unmarshal(data, &result)
	return result
}

func (s *Service) Overview(ctx context.Context) (Overview, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.initErr != nil {
		return Overview{}, s.initErr
	}
	result := Overview{Destinations: []Destination{}, Plans: []Plan{}, Runs: []Run{}, StagingDirectory: filepath.Join(s.directory, "staging")}
	_, err := exec.LookPath("restic")
	result.ResticAvailable = err == nil
	_, err = exec.LookPath("rclone")
	result.RcloneAvailable = err == nil
	for _, d := range s.state.Destinations {
		result.Destinations = append(result.Destinations, d)
	}
	for _, p := range s.state.Plans {
		result.Plans = append(result.Plans, p)
	}
	for _, r := range s.state.Runs {
		result.Runs = append(result.Runs, r.Run)
	}
	sort.Slice(result.Destinations, func(i, j int) bool { return result.Destinations[i].Name < result.Destinations[j].Name })
	sort.Slice(result.Plans, func(i, j int) bool { return result.Plans[i].Name < result.Plans[j].Name })
	sort.Slice(result.Runs, func(i, j int) bool { return result.Runs[i].CreatedAt.After(result.Runs[j].CreatedAt) })
	return clone(result), nil
}

func (s *Service) configurationBusyLocked() bool {
	if s.repositoryJob != "" {
		return true
	}
	for _, r := range s.state.Runs {
		if r.State == "running" || r.State == "queued" {
			return true
		}
	}
	return false
}
func (s *Service) SaveDestination(ctx context.Context, id string, input DestinationInput) (Destination, error) {
	if err := validateDestination(input); err != nil {
		return Destination{}, err
	}
	if input.Kind == "local" {
		canonical, err := filepath.EvalSymlinks(input.Repository)
		if os.IsNotExist(err) {
			parent, parentErr := filepath.EvalSymlinks(filepath.Dir(input.Repository))
			if parentErr != nil {
				return Destination{}, fmt.Errorf("%w: repository parent must exist", ErrInvalid)
			}
			canonical = filepath.Join(parent, filepath.Base(input.Repository))
			err = nil
		}
		if err != nil {
			return Destination{}, fmt.Errorf("%w: local repository is inaccessible", ErrInvalid)
		}
		input.Repository = canonical
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.initErr != nil {
		return Destination{}, s.initErr
	}
	if s.configurationBusyLocked() {
		return Destination{}, ErrBusy
	}
	for existingID, destination := range s.state.Destinations {
		if existingID != id && destination.Kind == input.Kind && destination.Repository == input.Repository {
			return Destination{}, fmt.Errorf("%w: this repository already has a destination", ErrInvalid)
		}
	}
	previous, exists := s.state.Destinations[id]
	if id != "" && !exists {
		return Destination{}, ErrNotFound
	}
	if id == "" {
		id = newID("dst_")
	}
	if exists && (previous.Kind != input.Kind || previous.Repository != input.Repository) {
		return Destination{}, fmt.Errorf("%w: repository kind and location are immutable; create a new destination", ErrInvalid)
	}
	if input.Secrets == nil && !exists {
		return Destination{}, fmt.Errorf("%w: credentials and repository password are required", ErrInvalid)
	}
	d := Destination{ID: id, Name: strings.TrimSpace(input.Name), Kind: input.Kind, Repository: input.Repository, ExpectedFilesystem: input.ExpectedFilesystem, Region: input.Region, CredentialsConfigured: true, Status: "untested"}
	if input.Kind == "local" {
		repo := filepath.Clean(input.Repository)
		if overlaps(repo, s.directory) {
			return Destination{}, fmt.Errorf("%w: repositories cannot overlap private backup state", ErrInvalid)
		}
	}
	if exists && previous.Kind == d.Kind && previous.Repository == d.Repository && input.Secrets == nil {
		d.LastCheckAt = previous.LastCheckAt
		d.LastCheckPolicy = previous.LastCheckPolicy
		d.LastSuccessAt = previous.LastSuccessAt
		d.LastRestoreTestAt = previous.LastRestoreTestAt
		d.RepositorySizeBytes = previous.RepositorySizeBytes
		d.Status = previous.Status
	}
	if input.Secrets != nil {
		secret := *input.Secrets
		if exists && secret.Password == "" {
			old, err := s.readSecrets(id)
			if err != nil {
				return Destination{}, err
			}
			secret.Password = old.Password
		}
		if d.Kind == "s3" && (secret.AccessKeyID == "" || secret.SecretAccessKey == "") {
			return Destination{}, fmt.Errorf("%w: S3 access key and secret are required", ErrInvalid)
		}
		if d.Kind == "sftp" && (secret.SSHPrivateKey == "" || secret.SSHKnownHosts == "") {
			return Destination{}, fmt.Errorf("%w: SSH private key and pinned known_hosts are required", ErrInvalid)
		}
		if d.Kind == "rclone" && secret.RcloneConfig == "" {
			return Destination{}, fmt.Errorf("%w: authorized rclone configuration is required", ErrInvalid)
		}
		if err := s.writeSecrets(id, secret); err != nil {
			return Destination{}, err
		}
	}
	s.state.Destinations[id] = d
	if err := s.persist(ctx); err != nil {
		if exists {
			s.state.Destinations[id] = previous
		} else {
			delete(s.state.Destinations, id)
		}
		return Destination{}, err
	}
	return d, nil
}
func overlaps(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	return a == b || strings.HasPrefix(a, b+string(filepath.Separator)) || strings.HasPrefix(b, a+string(filepath.Separator))
}
func (s *Service) DeleteDestination(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.configurationBusyLocked() {
		return ErrBusy
	}
	d, ok := s.state.Destinations[id]
	if !ok {
		return ErrNotFound
	}
	for _, p := range s.state.Plans {
		for _, a := range p.Destinations {
			if a.DestinationID == id {
				return fmt.Errorf("%w: destination is assigned to a plan", ErrInvalid)
			}
		}
	}
	delete(s.state.Destinations, id)
	if err := s.persist(ctx); err != nil {
		s.state.Destinations[id] = d
		return err
	}
	return os.RemoveAll(s.secretDirectory(id))
}

func normalizePlan(p *Plan) error {
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" || len(p.Name) > 200 {
		return fmt.Errorf("%w: plan name is required (max 200 characters)", ErrInvalid)
	}
	if p.Consistency != "files" && p.Consistency != "stop" {
		return fmt.Errorf("%w: consistency must be files or stop", ErrInvalid)
	}
	if len(p.Sources) == 0 || len(p.Sources) > 1000 {
		return fmt.Errorf("%w: select 1 to 1000 sources", ErrInvalid)
	}
	sourceIDs := map[string]bool{}
	for index := range p.Sources {
		source := &p.Sources[index]
		if source.ID == "" {
			source.ID = sourceID(source.Kind, source.StackID, source.Service, source.Mount+source.Path)
		}
		if !safeID.MatchString(source.ID) || sourceIDs[source.ID] {
			return fmt.Errorf("%w: source IDs must be unique safe identifiers", ErrInvalid)
		}
		sourceIDs[source.ID] = true
	}
	if len(p.Destinations) == 0 || len(p.Destinations) > 30 {
		return fmt.Errorf("%w: select 1 to 30 destinations", ErrInvalid)
	}
	if p.StagingMaxBytes == 0 {
		p.StagingMaxBytes = 10 << 30
	}
	if p.StagingMaxBytes < 1<<20 || p.StagingMaxBytes > 1<<50 {
		return fmt.Errorf("%w: staging_max_bytes outside supported range", ErrInvalid)
	}
	if p.StagingTTLHours == 0 {
		p.StagingTTLHours = 24
	}
	if p.StagingTTLHours < 1 || p.StagingTTLHours > 168 {
		return fmt.Errorf("%w: staging_ttl_hours must be 1 to 168", ErrInvalid)
	}
	if p.StopTimeoutSeconds == 0 {
		p.StopTimeoutSeconds = 300
	}
	if p.StopTimeoutSeconds < 10 || p.StopTimeoutSeconds > 3600 {
		return fmt.Errorf("%w: stop_timeout_seconds must be 10 to 3600", ErrInvalid)
	}
	seen := map[string]bool{}
	for i := range p.Destinations {
		a := &p.Destinations[i]
		if seen[a.DestinationID] {
			return fmt.Errorf("%w: duplicate destination", ErrInvalid)
		}
		seen[a.DestinationID] = true
		if a.Schedule.Frequency == "" {
			a.Schedule.Frequency = "manual"
		}
		if err := validateSchedule(a.Schedule); err != nil {
			return err
		}
		for _, v := range []int{a.Retention.Daily, a.Retention.Weekly, a.Retention.Monthly} {
			if v < 0 || v > 10000 {
				return fmt.Errorf("%w: invalid retention count", ErrInvalid)
			}
		}
	}
	return nil
}
func (s *Service) SavePlan(ctx context.Context, id string, p Plan) (Plan, error) {
	if err := normalizePlan(&p); err != nil {
		return Plan{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.initErr != nil {
		return Plan{}, s.initErr
	}
	if s.configurationBusyLocked() {
		return Plan{}, ErrBusy
	}
	previous, exists := s.state.Plans[id]
	if id != "" && !exists {
		return Plan{}, ErrNotFound
	}
	if id == "" {
		id = newID("plan_")
	}
	p.ID = id
	for i := range p.Destinations {
		a := &p.Destinations[i]
		if _, ok := s.state.Destinations[a.DestinationID]; !ok {
			return Plan{}, fmt.Errorf("%w: unknown destination", ErrInvalid)
		}
		a.LastScheduledAt = nil
		for _, old := range previous.Destinations {
			if old.DestinationID == a.DestinationID {
				a.LastScheduledAt = old.LastScheduledAt
			}
		}
	}
	if previewer, ok := s.capture.(interface {
		Preview(context.Context, Plan) ([]ManifestSource, error)
	}); ok {
		sources, err := previewer.Preview(ctx, p)
		if err != nil {
			return Plan{}, fmt.Errorf("%w: %v", ErrInvalid, err)
		}
		if err := s.validateSourceRepositories(sources); err != nil {
			return Plan{}, err
		}
	}
	s.state.Plans[id] = clone(p)
	if err := s.persist(ctx); err != nil {
		if exists {
			s.state.Plans[id] = previous
		} else {
			delete(s.state.Plans, id)
		}
		return Plan{}, err
	}
	return p, nil
}
func (s *Service) validateSourceRepositories(sources []ManifestSource) error {
	for _, source := range sources {
		for _, d := range s.state.Destinations {
			if d.Kind == "local" && source.OriginalPath != "" && overlaps(source.OriginalPath, d.Repository) {
				return fmt.Errorf("%w: source overlaps local backup repository", ErrInvalid)
			}
		}
	}
	return nil
}
func (s *Service) DeletePlan(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.configurationBusyLocked() {
		return ErrBusy
	}
	p, ok := s.state.Plans[id]
	if !ok {
		return ErrNotFound
	}
	delete(s.state.Plans, id)
	if err := s.persist(ctx); err != nil {
		s.state.Plans[id] = p
		return err
	}
	return nil
}

func (s *Service) StartRun(ctx context.Context, planID string, request RunRequest, requestedBy string) (store.Job, Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.initErr != nil {
		return store.Job{}, Run{}, s.initErr
	}
	p, ok := s.state.Plans[planID]
	if !ok {
		return store.Job{}, Run{}, ErrNotFound
	}
	selected := map[string]bool{}
	for _, id := range request.DestinationIDs {
		selected[id] = true
	}
	copies := []CopyResult{}
	for _, a := range p.Destinations {
		if a.Enabled && (len(request.DestinationIDs) == 0 || selected[a.DestinationID]) {
			copies = append(copies, CopyResult{DestinationID: a.DestinationID, State: "pending", Verification: "pending", Retention: "pending"})
			delete(selected, a.DestinationID)
		}
	}
	if len(selected) > 0 || len(copies) == 0 {
		return store.Job{}, Run{}, fmt.Errorf("%w: select enabled destinations assigned to this plan", ErrInvalid)
	}
	job, err := s.jobs.StartDraining(ctx, "backup_run", requestedBy, jobs.GlobalResource())
	if err != nil {
		return store.Job{}, Run{}, err
	}
	now := s.now()
	run := Run{ID: newID("run_"), JobID: job.ID, PlanID: p.ID, PlanName: p.Name, State: "running", CaptureState: "pending", CreatedAt: now, ExpiresAt: now.Add(time.Duration(p.StagingTTLHours) * time.Hour), Copies: copies, Plan: clone(p)}
	s.state.Runs[run.ID] = persistedRun{Run: run, PlanSnapshot: clone(p)}
	if err := s.persist(ctx); err != nil {
		delete(s.state.Runs, run.ID)
		_, _ = s.jobs.FinishFailed(context.Background(), job, "backup_state_failed", "Could not persist backup run.")
		return store.Job{}, Run{}, err
	}
	return job, clone(run), nil
}

func (s *Service) StartRetry(ctx context.Context, runID string, request RunRequest, requestedBy string) (store.Job, Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored, ok := s.state.Runs[runID]
	if !ok {
		return store.Job{}, Run{}, ErrNotFound
	}
	run := stored.Run
	if run.State == "running" {
		return store.Job{}, Run{}, ErrBusy
	}
	if run.CaptureState != "succeeded" || run.CapturedAt == nil || run.Manifest == nil || !s.now().Before(run.ExpiresAt) {
		return store.Job{}, Run{}, ErrExpired
	}
	if _, err := os.Stat(filepath.Join(s.directory, "staging", runID, "manifest.json")); err != nil {
		return store.Job{}, Run{}, ErrExpired
	}
	selected := map[string]bool{}
	for _, id := range request.DestinationIDs {
		selected[id] = true
	}
	pending := 0
	for i := range run.Copies {
		copy := &run.Copies[i]
		if copy.State != "succeeded" && (len(request.DestinationIDs) == 0 || selected[copy.DestinationID]) {
			copy.State = "pending"
			copy.Error = ""
			delete(selected, copy.DestinationID)
			pending++
		}
	}
	if pending == 0 || len(selected) > 0 {
		return store.Job{}, Run{}, fmt.Errorf("%w: no failed destination selected", ErrInvalid)
	}
	job, err := s.jobs.StartDraining(ctx, "backup_retry", requestedBy, jobs.GlobalResource())
	if err != nil {
		return store.Job{}, Run{}, err
	}
	run.JobID = job.ID
	run.State = "running"
	run.Error = ""
	s.state.Runs[runID] = persistedRun{Run: run, PlanSnapshot: stored.PlanSnapshot}
	if err := s.persist(ctx); err != nil {
		s.state.Runs[runID] = stored
		_, _ = s.jobs.FinishFailed(context.Background(), job, "backup_state_failed", "Could not persist retry.")
		return store.Job{}, Run{}, err
	}
	return job, clone(run), nil
}

func (s *Service) saveRun(run Run, p Plan) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Runs[run.ID] = persistedRun{Run: run, PlanSnapshot: p}
	return s.persist(context.Background())
}
func (s *Service) event(ctx context.Context, job store.Job, message string) {
	_ = s.jobs.PublishEvent(ctx, job, "job_progress", message, "", nil)
}
func (s *Service) finish(ctx context.Context, job store.Job, err error) {
	finishCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err == nil {
		_, _ = s.jobs.FinishSucceeded(finishCtx, job)
	} else if errors.Is(err, context.Canceled) {
		_, _ = s.jobs.FinishCancelled(finishCtx, job, "backup_cancelled", "Backup operation cancelled.")
	} else if errors.Is(err, context.DeadlineExceeded) {
		_, _ = s.jobs.FinishTimedOut(finishCtx, job, "backup_timeout", "Backup operation timed out.")
	} else {
		_, _ = s.jobs.FinishFailed(finishCtx, job, "backup_failed", err.Error())
	}
}
func (s *Service) ExecuteRun(ctx context.Context, job store.Job, runID string) {
	ctx, cancel := context.WithTimeout(ctx, 24*time.Hour)
	defer cancel()
	unregister := s.jobs.RegisterCancel(job.ID, cancel)
	defer unregister()
	s.mu.Lock()
	stored, ok := s.state.Runs[runID]
	s.mu.Unlock()
	if !ok {
		s.finish(ctx, job, ErrNotFound)
		return
	}
	run := stored.Run
	ctx, expireCancel := context.WithDeadline(ctx, run.ExpiresAt)
	defer expireCancel()
	p := stored.PlanSnapshot
	stage := filepath.Join(s.directory, "staging", run.ID)
	var operationErr error
	defer func() {
		if operationErr != nil {
			run.State = "failed"
			run.Error = operationErr.Error()
		} else {
			run.State = "succeeded"
		}
		if err := s.saveRun(run, p); err != nil {
			operationErr = fmt.Errorf("backup state persistence failed")
		}
		s.finish(ctx, job, operationErr)
	}()
	if run.CaptureState != "succeeded" {
		s.event(ctx, job, "Preparing one recovery set for all selected destinations.")
		run.CaptureState = "running"
		if err := s.saveRun(run, p); err != nil {
			operationErr = err
			return
		}
		if s.capture == nil {
			operationErr = fmt.Errorf("capture service unavailable")
			run.CaptureState = "failed"
			return
		}
		if previewer, ok := s.capture.(interface {
			Preview(context.Context, Plan) ([]ManifestSource, error)
		}); ok {
			sources, err := previewer.Preview(ctx, p)
			if err != nil {
				operationErr = err
				run.CaptureState = "failed"
				return
			}
			s.mu.Lock()
			err = s.validateSourceRepositories(sources)
			s.mu.Unlock()
			if err != nil {
				operationErr = err
				run.CaptureState = "failed"
				return
			}
		}
		manifest, err := s.capture.Prepare(ctx, p, run.ID, stage)
		if err != nil {
			run.CaptureState = "failed"
			operationErr = err
			return
		}
		// Surface single-device copies without treating them as independent
		// protection from host/disk loss.
		s.mu.Lock()
		for _, copy := range run.Copies {
			d := s.state.Destinations[copy.DestinationID]
			if d.Kind != "local" {
				continue
			}
			repoPath := d.Repository
			if _, statErr := os.Stat(repoPath); os.IsNotExist(statErr) {
				repoPath = filepath.Dir(repoPath)
			}
			repoDevice, repoErr := FilesystemIdentity(repoPath)
			if repoErr != nil {
				continue
			}
			for _, source := range manifest.Sources {
				sourceDevice, sourceErr := FilesystemIdentity(source.OriginalPath)
				if sourceErr == nil && sourceDevice == repoDevice {
					manifest.Warnings = append(manifest.Warnings, "Local destination "+d.Name+" shares a filesystem with selected data; retain an independent off-host copy.")
					break
				}
			}
		}
		s.mu.Unlock()
		manifest.Version = 1
		manifest.RunID = run.ID
		manifest.PlanID = p.ID
		if manifest.CapturedAt.IsZero() {
			manifest.CapturedAt = s.now()
		}
		data, err := json.MarshalIndent(manifest, "", "  ")
		if err == nil {
			err = atomicfile.WriteBytesMode(filepath.Join(stage, "manifest.json"), data, ".manifest-*", 0o600)
		}
		if err != nil {
			operationErr = err
			run.CaptureState = "failed"
			return
		}
		run.Manifest = &manifest
		run.CapturedAt = &manifest.CapturedAt
		run.CaptureState = "succeeded"
		if err = s.saveRun(run, p); err != nil {
			operationErr = err
			return
		}
	}
	for i := range run.Copies {
		copy := &run.Copies[i]
		if copy.State != "pending" {
			continue
		}
		if err := ctx.Err(); err != nil {
			copy.State = "failed"
			copy.Error = err.Error()
			operationErr = err
			continue
		}
		s.mu.Lock()
		d, exists := s.state.Destinations[copy.DestinationID]
		s.mu.Unlock()
		if !exists {
			copy.State = "failed"
			copy.Error = "Destination was removed."
			operationErr = fmt.Errorf("one or more configured copies failed")
			continue
		}
		copy.State = "running"
		_ = s.saveRun(run, p)
		s.event(ctx, job, "Uploading recovery set to "+d.Name+".")
		var err error
		if copy.SnapshotID == "" {
			copy.SnapshotID, err = s.upload(ctx, d, p, run, stage)
			if err == nil {
				copy.UploadedAt = nowPointer()
				_ = s.saveRun(run, p)
			}
		}
		if err == nil {
			_, err = s.restic(ctx, d, "", "cat", "snapshot", copy.SnapshotID)
			if err == nil {
				copy.Verification = "snapshot_metadata_verified"
			} else {
				copy.Verification = "failed"
			}
		}
		if err == nil {
			for _, a := range p.Destinations {
				if a.DestinationID == d.ID {
					err = s.retain(ctx, d, p.ID, a.Retention)
					break
				}
			}
			if err == nil {
				copy.Retention = "succeeded"
			} else {
				copy.Retention = "failed"
			}
		}
		if err != nil {
			copy.State = "failed"
			copy.Error = err.Error()
			operationErr = fmt.Errorf("one or more configured copies failed")
			if ctx.Err() != nil {
				operationErr = ctx.Err()
			}
		} else {
			copy.State = "succeeded"
			copy.Error = ""
			s.mu.Lock()
			d.LastSuccessAt = copy.UploadedAt
			d.Status = "available"
			d.Error = ""
			s.state.Destinations[d.ID] = d
			s.mu.Unlock()
		}
		if err = s.saveRun(run, p); err != nil {
			operationErr = err
			return
		}
	}
	for _, copy := range run.Copies {
		if copy.State != "succeeded" && operationErr == nil {
			operationErr = fmt.Errorf("one or more configured copies are missing")
		}
	}
}

// Reconcile preserves successful copies and marks interrupted work for an
// explicit retry. Capture recovery must run before accepting mutations.
func (s *Service) Reconcile(ctx context.Context) error {
	if s.capture != nil {
		if err := s.capture.Recover(ctx); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.initErr != nil {
		return s.initErr
	}
	for id, stored := range s.state.Runs {
		if stored.State != "running" && stored.State != "queued" {
			continue
		}
		stored.State = "failed"
		stored.Error = "Operation interrupted by service restart."
		if stored.CaptureState != "succeeded" {
			stored.CaptureState = "failed"
		}
		for i := range stored.Copies {
			copy := &stored.Copies[i]
			if copy.State == "running" || copy.State == "pending" {
				copy.State = "failed"
				copy.Error = "Operation interrupted by service restart."
			}
		}
		s.state.Runs[id] = stored
	}
	return s.persist(ctx)
}

// pruneMetadataLocked bounds operational summaries without deleting repository
// snapshots or dropping active/retryable recovery sets. The durable repository
// manifest remains the source of truth after a summary expires.
func (s *Service) pruneMetadataLocked() {
	expired := []persistedRun{}
	for _, run := range s.state.Runs {
		if run.State != "running" && run.State != "queued" && !s.now().Before(run.ExpiresAt) {
			expired = append(expired, run)
		}
	}
	sort.Slice(expired, func(i, j int) bool { return expired[i].CreatedAt.After(expired[j].CreatedAt) })
	for index := 200; index < len(expired); index++ {
		run := expired[index]
		if safeID.MatchString(run.ID) {
			if err := os.RemoveAll(filepath.Join(s.directory, "staging", run.ID)); err != nil {
				continue
			}
		}
		delete(s.state.Runs, run.ID)
	}
	for len(s.state.RestoreOrder) > 200 {
		oldest := s.state.RestoreOrder[0]
		s.state.RestoreOrder = s.state.RestoreOrder[1:]
		delete(s.state.Restores, oldest)
	}
}
