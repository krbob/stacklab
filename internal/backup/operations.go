package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"stacklab/internal/jobs"
	"stacklab/internal/store"
)

type PruneRequest struct {
	AcknowledgeIntegrityFailure bool `json:"acknowledge_integrity_failure,omitempty"`
}

func validatePruneAcknowledgement(d Destination, requestedBy string, options []PruneRequest) error {
	normalizeCheckEvidence(&d)
	if d.CheckStatus != "failed" {
		return nil
	}
	if requestedBy == "scheduler" {
		return fmt.Errorf("%w: scheduled pruning is blocked by unresolved repository integrity failure", ErrBusy)
	}
	if len(options) != 1 || !options[0].AcknowledgeIntegrityFailure {
		return fmt.Errorf("%w: pruning a repository with unresolved integrity failure requires acknowledge_integrity_failure=true; preserve existing copies and inspect the repository first", ErrBusy)
	}
	return nil
}

func (s *Service) StartRepositoryJob(ctx context.Context, id, action, requestedBy string, prune ...PruneRequest) (store.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.initErr != nil {
		return store.Job{}, s.initErr
	}
	d, ok := s.state.Destinations[id]
	if !ok {
		return store.Job{}, ErrNotFound
	}
	switch action {
	case "backup_destination_test", "backup_repository_init", "backup_check", "backup_restore", "backup_prune", "backup_unlock":
	default:
		return store.Job{}, fmt.Errorf("%w: unsupported repository operation", ErrInvalid)
	}
	if action == "backup_prune" {
		if err := validatePruneAcknowledgement(d, requestedBy, prune); err != nil {
			return store.Job{}, err
		}
	}
	key := repositoryKey(d)
	if s.repositoryReads[key] > 0 || s.repositoryJobs[key] != "" {
		return store.Job{}, ErrBusy
	}
	job, err := s.jobs.StartWithResources(ctx, "", action, requestedBy, jobs.BackupRepositoryResource(key))
	if err == nil {
		if s.repositoryJobs == nil {
			s.repositoryJobs = map[string]string{}
		}
		s.repositoryJobs[key] = job.ID
	}
	return job, err
}

func (s *Service) ExecuteRepositoryJob(ctx context.Context, job store.Job, id, action string, check CheckRequest, restore RestoreRequest, prune ...PruneRequest) (result RestoreResult, operationErr error) {
	defer s.releaseRepositories(job.ID)
	ctx, cancel := context.WithTimeout(ctx, 24*time.Hour)
	defer cancel()
	unregister := s.jobs.RegisterCancel(job.ID, cancel)
	defer unregister()
	s.mu.Lock()
	d, ok := s.state.Destinations[id]
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if ok {
			currentEvidence := s.state.Destinations[id]
			d.LastInventoryAt = currentEvidence.LastInventoryAt
			d.LastInventoryAttemptAt = currentEvidence.LastInventoryAttemptAt
			d.InventoryError = currentEvidence.InventoryError
			d.LastOperation = action
			d.LastOperationAt = s.timePointer()
			d.LastOperationStatus, d.LastOperationError = "succeeded", ""
			if operationErr != nil {
				d.LastOperationStatus = "failed"
				d.LastOperationError = operationErr.Error()
			}
			// Validation/review/extraction errors do not establish repository
			// unavailability. Integrity and the last operation are independent.
			if action == "backup_destination_test" || action == "backup_repository_init" {
				if operationErr != nil {
					d.Status = "error"
					d.Error = operationErr.Error()
				} else {
					d.Status = "available"
					d.Error = ""
				}
			}
			if action == "backup_check" {
				if operationErr == nil {
					d.Status = "available"
					d.Error = ""
				}
			}
			s.state.Destinations[id] = d
			if err := s.persist(context.Background()); err != nil && operationErr == nil {
				operationErr = err
			}
		}
		s.mu.Unlock()
		s.finish(ctx, job, operationErr)
	}()
	if !ok {
		return result, ErrNotFound
	}
	s.event(ctx, job, "Running repository operation for "+d.Name+".")
	switch action {
	case "backup_repository_init":
		_, operationErr = s.restic(ctx, d, "", "init", "--repository-version", "2")
	case "backup_destination_test":
		_, operationErr = s.restic(ctx, d, "", "cat", "config")
	case "backup_check":
		operationErr = s.check(ctx, d, check.ReadData)
		recordCheckAttempt(&d, check.ReadData, s.now(), operationErr)
		if operationErr == nil {
			_ = s.refreshInventory(ctx, d)
		}
	case "backup_prune":
		if operationErr = validatePruneAcknowledgement(d, job.RequestedBy, prune); operationErr != nil {
			break
		}
		if d.CheckStatus == "failed" {
			s.event(ctx, job, "Operator acknowledged unresolved repository integrity failure before pruning.")
		}
		_, operationErr = s.restic(ctx, d, "", "prune")
		if operationErr == nil {
			d.LastPruneAt = s.timePointer()
		}
	case "backup_unlock":
		s.event(ctx, job, "Removing only locks restic identifies as stale; active locks are preserved.")
		_, operationErr = s.restic(ctx, d, "", "unlock")
		if operationErr == nil {
			s.event(ctx, job, "Stale-lock removal completed. Repository integrity evidence is unchanged.")
		}
	case "backup_restore":
		restore.jobID = job.ID
		if restore.RestoredDirectory != "" || restore.RestoreID != "" || len(restore.Mappings) > 0 {
			if restore.Test {
				operationErr = fmt.Errorf("%w: restore tests require a complete isolated recovery", ErrInvalid)
			} else {
				s.mu.Lock()
				recoveryError := s.recoveryError
				s.mu.Unlock()
				if recoveryError != "" {
					operationErr = fmt.Errorf("%w: backup recovery requires attention: %s", ErrBusy, recoveryError)
				} else if lockErr := s.jobs.ReplaceResources(job.ID, jobs.GlobalResource(), jobs.BackupRepositoryResource(repositoryKey(d))); lockErr != nil {
					operationErr = lockErr
				} else {
					result, operationErr = s.applyReviewedRestore(ctx, d, restore)
					if operationErr != nil {
						s.recordRecoveryFailure(operationErr)
					}
				}
			}
		} else {
			result, operationErr = s.restore(ctx, d, restore)
		}
		if operationErr == nil && restore.Test {
			d.LastRestoreTestAt = s.timePointer()
			d.LastRestoreTestSnapshotID = result.SnapshotID
			s.mu.Lock()
			rememberSnapshotEvidence(&s.state.RestoreTests, snapshotEvidenceKey(d.ID, result.SnapshotID), *d.LastRestoreTestAt)
			s.mu.Unlock()
		}
		if operationErr == nil {
			if restore.RestoredDirectory != "" || restore.RestoreID != "" || len(restore.Mappings) > 0 {
				s.event(ctx, job, "Reviewed recovery was applied to the selected targets.")
			} else {
				s.event(ctx, job, "Recovered files are available in "+result.Target+".")
			}
		}
	default:
		operationErr = fmt.Errorf("%w: unsupported repository operation", ErrInvalid)
	}
	if operationErr == nil && (action == "backup_check" || action == "backup_destination_test" || action == "backup_prune") {
		if output, statsErr := s.restic(ctx, d, "", "stats", "--mode", "raw-data"); statsErr == nil {
			var stats struct {
				TotalSize int64 `json:"total_size"`
			}
			if json.Unmarshal(output, &stats) == nil && stats.TotalSize >= 0 {
				d.RepositorySizeBytes = &stats.TotalSize
			}
		}
	}
	return result, operationErr
}

func (s *Service) restore(ctx context.Context, d Destination, request RestoreRequest) (result RestoreResult, resultErr error) {
	s.mu.Lock()
	libraryFull := len(s.state.Restores) >= 200
	s.mu.Unlock()
	if libraryFull {
		return RestoreResult{}, fmt.Errorf("%w: recovery library is full; remove an old result first", ErrInvalid)
	}
	result = RestoreResult{ID: newID("restore_"), JobID: request.jobID, CreatedAt: s.now(), Available: true, DestinationID: d.ID, SnapshotID: request.SnapshotID, Test: request.Test}
	if err := validateRestoreSelection(request); err != nil {
		return result, err
	}
	if len(request.SnapshotID) < 64 {
		// A short ID is accepted as input, but durable proof is bound to the
		// unique immutable full ID returned by this repository before restore.
		output, err := s.restic(ctx, d, "", "snapshots", request.SnapshotID)
		if err != nil {
			return result, err
		}
		var snapshots []Snapshot
		if err := json.Unmarshal(output, &snapshots); err != nil || len(snapshots) != 1 || len(snapshots[0].ID) != 64 || !snapshotID.MatchString(snapshots[0].ID) || !strings.HasPrefix(snapshots[0].ID, request.SnapshotID) {
			return result, fmt.Errorf("%w: restore requires a uniquely resolved snapshot ID", ErrInvalid)
		}
		request.SnapshotID, result.SnapshotID = snapshots[0].ID, snapshots[0].ID
	}
	exactSelection := []string{}
	if len(request.SourceIDs) > 0 || len(request.Files) > 0 {
		manifest, selectionErr := s.snapshotManifest(ctx, d, request.SnapshotID)
		if selectionErr != nil {
			return result, selectionErr
		}
		chosen := map[string]bool{}
		for _, id := range request.SourceIDs {
			chosen[id] = true
		}
		for index := range request.Files {
			selected := &request.Files[index]
			normalized, pathErr := catalogPath(selected.Path, false)
			if pathErr != nil {
				return result, pathErr
			}
			selected.Path = normalized
			chosen[selected.SourceID] = true
		}
		for id := range chosen {
			source, sourceErr := catalogSource(manifest, id)
			if sourceErr != nil || !source.Archive || strings.ContainsAny(source.SavedPath, "*?[\\") {
				return result, fmt.Errorf("%w: unknown or unsupported selected source", ErrInvalid)
			}
			request.Includes = append(request.Includes, "/"+source.SavedPath)
			if source.IndexPath != "" {
				request.Includes = append(request.Includes, "/"+source.IndexPath)
			}
		}
		exactSelection = append(exactSelection, request.Includes...)
		exactSelection = append(exactSelection, "/manifest.json")
		result.SelectedFiles = append([]FileSelection(nil), request.Files...)
	}
	target := request.Target
	if target == "" {
		target = filepath.Join(s.directory, "restores", newID("restore_"))
	}
	if !filepath.IsAbs(target) || filepath.Clean(target) == "/" {
		return result, fmt.Errorf("%w: restore target must be an absolute, new directory", ErrInvalid)
	}
	target = filepath.Clean(target)
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		return result, fmt.Errorf("%w: restore target must not already exist", ErrInvalid)
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(target))
	if err != nil {
		return result, fmt.Errorf("%w: restore parent must exist", ErrInvalid)
	}
	target = filepath.Join(parent, filepath.Base(target))
	// No restore into live workspace/configuration or repository directories.
	root, _ := filepath.Abs(s.cfg.RootDir)
	if overlaps(target, root) && !isWithin(filepath.Join(s.directory, "restores"), target) {
		return result, fmt.Errorf("%w: use an isolated target outside the live workspace", ErrInvalid)
	}
	if overlaps(target, s.directory) && !isWithin(filepath.Join(s.directory, "restores"), target) {
		return result, fmt.Errorf("%w: restore target overlaps private backup state", ErrInvalid)
	}
	s.mu.Lock()
	for _, destination := range s.state.Destinations {
		if destination.Kind == "local" && overlaps(target, destination.Repository) {
			s.mu.Unlock()
			return result, fmt.Errorf("%w: restore target overlaps a repository", ErrInvalid)
		}
	}
	s.mu.Unlock()
	for _, include := range request.Includes {
		if include == "" || strings.ContainsAny(include, "\x00\r\n") || strings.Contains(include, "..") {
			return result, fmt.Errorf("%w: invalid restore selection", ErrInvalid)
		}
	}
	// Reserve enough room for the downloaded archives and their extracted
	// content. Repository snapshots stay untouched if local capacity is too low.
	restoreBytes, sizeErr := s.restoreSize(ctx, d, request.SnapshotID, exactSelection)
	if sizeErr != nil {
		return result, sizeErr
	}
	if err = checkCaptureSpace(parent, restoreBytes*2); err != nil {
		return result, fmt.Errorf("insufficient space for isolated restore: %w", err)
	}
	if err = os.Mkdir(target, 0o700); err != nil {
		return result, err
	}
	result.Target = target
	result.Managed = s.managedRestore(result)
	registered := false
	if result.Managed {
		// A failed isolated restore owns only this newly created internal directory.
		// Bind cleanup to its parent descriptor and original inode.
		root, rootErr := os.OpenRoot(parent)
		if rootErr != nil {
			_ = os.Remove(target)
			return result, rootErr
		}
		original, _ := root.Lstat(filepath.Base(target))
		defer func() {
			defer root.Close()
			if !registered {
				current, statErr := root.Lstat(filepath.Base(target))
				if statErr == nil && original != nil && os.SameFile(original, current) {
					if cleanupErr := s.deleteManagedRestore(result); cleanupErr != nil {
						resultErr = errors.Join(resultErr, fmt.Errorf("could not remove failed isolated recovery: %w", cleanupErr))
					}
				}
			}
		}()
	}
	args := []string{"restore", request.SnapshotID, "--target", target, "--verify"}
	for _, include := range request.Includes {
		args = append(args, "--include", include)
	}
	if len(request.Includes) > 0 {
		args = append(args, "--include", "/manifest.json")
	}
	if _, err = s.restic(ctx, d, "", args...); err != nil {
		return result, err
	}
	manifest, err := LoadManifest(target)
	if err != nil {
		return result, fmt.Errorf("restored data has no supported Stacklab manifest: %w", err)
	}
	result.Manifest = &manifest
	if err = os.Mkdir(filepath.Join(target, "files"), 0o700); err != nil {
		return result, fmt.Errorf("isolated restore inspection directory must be new: %w", err)
	}

	// Keep the original recovery archives for reviewed recovery, and expose
	// extracted, inspectable files for both ordinary restores and restore tests.
	seenSources := map[string]bool{}
	extractedSources := 0
	for _, source := range manifest.Sources {
		if !source.Archive {
			continue
		}
		if !safeID.MatchString(source.Source.ID) || seenSources[source.Source.ID] {
			return result, fmt.Errorf("invalid or duplicate manifest source identity")
		}
		seenSources[source.Source.ID] = true
		archive, archiveErr := ManifestArchivePath(target, source)
		if archiveErr != nil {
			if len(request.Includes) > 0 && os.IsNotExist(archiveErr) {
				continue
			}
			return result, archiveErr
		}
		info, statErr := os.Stat(archive)
		if statErr != nil {
			return result, statErr
		}
		if err = checkCaptureSpace(target, info.Size()); err != nil {
			return result, fmt.Errorf("insufficient space to extract recovery source: %w", err)
		}
		extractedSources++
		result.SourceIDs = append(result.SourceIDs, source.Source.ID)
		if len(source.RecoveryRestrictions) > 0 && len(request.Files) == 0 {
			if request.Test {
				return result, fmt.Errorf("restore test cannot automatically materialize source %s: recovery restrictions require operator review", source.Source.ID)
			}
			// Preserve the faithful archive for explicit offline recovery without
			// silently removing metadata or creating privileged filesystem objects.
			continue
		}
		filesTarget := filepath.Join(target, "files", source.Source.ID)
		if len(request.Files) > 0 {
			paths := []string{}
			for _, selected := range request.Files {
				if selected.SourceID == source.Source.ID {
					paths = append(paths, selected.Path)
				}
			}
			if len(paths) == 0 {
				continue
			}
			err = ExtractSelectedFiles(ctx, archive, filesTarget, paths)
		} else {
			err = ExtractArchive(ctx, archive, filesTarget, false)
		}
		if err != nil {
			return result, fmt.Errorf("isolated source extraction failed: %w", err)
		}
	}

	if len(manifest.Sources) > 0 && extractedSources == 0 {
		return result, fmt.Errorf("%w: restore selection contains no recovery archives", ErrInvalid)
	}
	s.mu.Lock()
	result.RecoveryStatus = "unknown"
	if evidence, found := s.snapshotClassificationLocked(d.ID, result.SnapshotID); found {
		result.RecoveryStatus = evidence.Status
		at := evidence.ClassifiedAt
		result.ClassifiedAt = &at
	}
	s.mu.Unlock()
	if err = s.registerRestoreResult(context.Background(), result); err != nil {
		return result, err
	}
	registered = true
	return result, nil
}

// LoadManifest reads metadata from an isolated restic restore. Source paths are
// revalidated by the capture layer before any reviewed in-place recovery.
func LoadManifest(target string) (Manifest, error) {
	var manifest Manifest
	canonical, canonicalErr := filepath.EvalSymlinks(target)
	if canonicalErr != nil || canonical != filepath.Clean(target) {
		return manifest, fmt.Errorf("restore directory must not contain symlinks")
	}
	manifestPath := filepath.Join(target, "manifest.json")
	info, err := os.Lstat(manifestPath)
	if err != nil {
		return manifest, err
	}
	if !info.Mode().IsRegular() || info.Size() > 16<<20 {
		return manifest, fmt.Errorf("manifest must be a regular file below 16 MiB")
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return manifest, err
	}
	if len(data) > 16<<20 {
		return manifest, fmt.Errorf("manifest exceeds 16 MiB")
	}
	if err = json.Unmarshal(data, &manifest); err != nil {
		return manifest, fmt.Errorf("invalid recovery manifest")
	}
	if manifest.Version != 1 {
		return manifest, fmt.Errorf("unsupported recovery manifest version")
	}
	return manifest, nil
}
func isWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Background admission stays responsive while transfers or checks run. Capture
// exclusion and per-repository leases remain authoritative; bounded workers are
// joined before shutdown completes.
func (s *Service) RunBackground(ctx context.Context) {
	runs, maintenance, inventories := newBackgroundWorkers(4), newBackgroundWorkers(2), newBackgroundWorkers(1)
	defer runs.wait()
	defer maintenance.wait()
	defer inventories.wait()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	var lastHousekeeping time.Time
	for {
		if ctx.Err() != nil {
			return
		}
		s.scheduleRuns(ctx, runs)
		s.scheduleMaintenance(ctx, maintenance)
		if lastHousekeeping.IsZero() || time.Since(lastHousekeeping) >= time.Minute {
			s.cleanupExpired()
			if inventories.reserve(ctx) {
				go func() { defer inventories.release(); s.refreshInventories(ctx) }()
			}
			lastHousekeeping = time.Now()
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (s *Service) runScheduled(ctx context.Context) { s.scheduleRuns(ctx, nil) }
func (s *Service) scheduleRuns(ctx context.Context, workers *backgroundWorkers) {
	if s.SchedulingPaused() {
		return
	}
	s.mu.Lock()
	plans := make([]Plan, 0, len(s.state.Plans))
	for _, p := range s.state.Plans {
		plans = append(plans, clone(p))
	}
	s.mu.Unlock()
	sortPlans(plans)
	for _, p := range plans {
		if s.SchedulingPaused() {
			return
		}
		if !p.Enabled {
			continue
		}
		dueIDs := []string{}
		dueTimes := map[string]time.Time{}
		for _, a := range p.Destinations {
			if !a.Enabled {
				continue
			}
			due, ok := latestDue(a.Schedule, s.now())
			if ok && (occurrencePending(a.LastScheduledAt, due, s.now())) {
				dueIDs = append(dueIDs, a.DestinationID)
				dueTimes[a.DestinationID] = due
			}
		}
		if len(dueIDs) == 0 {
			continue
		}
		if workers != nil && !workers.reserve(ctx) {
			return
		}
		job, run, err := s.StartRun(ctx, p.ID, RunRequest{DestinationIDs: dueIDs}, "scheduler")
		if err != nil {
			if workers != nil {
				workers.release()
			}
			continue
		}
		s.mu.Lock()
		current := s.state.Plans[p.ID]
		for i := range current.Destinations {
			if due, ok := dueTimes[current.Destinations[i].DestinationID]; ok {
				current.Destinations[i].LastScheduledAt = &due
				if activated := current.Destinations[i].ScheduleActivatedAt; activated != nil && activated.After(s.now().Add(clockSkewTolerance)) {
					current.Destinations[i].ScheduleActivatedAt = s.timePointer()
				}
			}
		}
		s.state.Plans[p.ID] = current
		persistErr := s.persist(context.Background())
		s.mu.Unlock()
		if persistErr != nil {
			run.State = "failed"
			run.Error = "Could not persist scheduler occurrence."
			run.CaptureState = "failed"
			_ = s.saveRun(run, p)
			s.finish(ctx, job, persistErr)
			s.releaseRepositories(job.ID)
			if workers != nil {
				workers.release()
			}
			continue
		}
		if workers == nil {
			s.ExecuteRun(ctx, job, run.ID)
		} else {
			go func() { defer workers.release(); s.ExecuteRun(ctx, job, run.ID) }()
		}
	}
}
func sortPlans(plans []Plan) { // Stable ordering prevents map iteration starvation.
	for i := 1; i < len(plans); i++ {
		for j := i; j > 0 && plans[j].ID < plans[j-1].ID; j-- {
			plans[j], plans[j-1] = plans[j-1], plans[j]
		}
	}
}
func (s *Service) cleanupExpired() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, run := range s.state.Runs {
		if run.State != "running" && run.State != "queued" && !s.now().Before(run.ExpiresAt) && safeID.MatchString(run.ID) {
			_ = os.RemoveAll(filepath.Join(s.directory, "staging", run.ID))
		}
	}
}
