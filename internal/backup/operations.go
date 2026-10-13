package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"stacklab/internal/jobs"
	"stacklab/internal/store"
)

func (s *Service) StartRepositoryJob(ctx context.Context, id, action, requestedBy string) (store.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.initErr != nil {
		return store.Job{}, s.initErr
	}
	if s.activeRepositoryReads > 0 {
		return store.Job{}, ErrBusy
	}
	if _, ok := s.state.Destinations[id]; !ok {
		return store.Job{}, ErrNotFound
	}
	switch action {
	case "backup_destination_test", "backup_repository_init", "backup_check", "backup_restore", "backup_prune":
	default:
		return store.Job{}, fmt.Errorf("%w: unsupported repository operation", ErrInvalid)
	}
	job, err := s.jobs.StartDraining(ctx, action, requestedBy, jobs.GlobalResource())
	if err == nil {
		s.repositoryJob = job.ID
	}
	return job, err
}

func (s *Service) ExecuteRepositoryJob(ctx context.Context, job store.Job, id, action string, check CheckRequest, restore RestoreRequest) (result RestoreResult, operationErr error) {
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
				d.CheckStatus, d.CheckError = "verified", ""
				if operationErr != nil {
					d.CheckStatus = "failed"
					d.CheckError = operationErr.Error()
				} else {
					d.Status = "available"
					d.Error = ""
				}
			}
			s.state.Destinations[id] = d
			if err := s.persist(context.Background()); err != nil && operationErr == nil {
				operationErr = err
			}
		}
		s.repositoryJob = ""
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
		if operationErr == nil {
			d.LastCheckAt = s.timePointer()
			d.LastCheckPolicy = check.ReadData
			if d.LastCheckPolicy == "" {
				d.LastCheckPolicy = "metadata"
			}
		}
	case "backup_prune":
		_, operationErr = s.restic(ctx, d, "", "prune")
		if operationErr == nil {
			d.LastPruneAt = s.timePointer()
		}
	case "backup_restore":
		restore.jobID = job.ID
		if restore.RestoredDirectory != "" || restore.RestoreID != "" || len(restore.Mappings) > 0 {
			if restore.Test {
				operationErr = fmt.Errorf("%w: restore tests require a complete isolated recovery", ErrInvalid)
			} else {
				result, operationErr = s.applyReviewedRestore(ctx, d, restore)
			}
		} else {
			result, operationErr = s.restore(ctx, d, restore)
		}
		if operationErr == nil && restore.Test {
			d.LastRestoreTestAt = s.timePointer()
		}
		if operationErr == nil {
			s.event(ctx, job, "Recovered files are available in "+result.Target+".")
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

func (s *Service) restore(ctx context.Context, d Destination, request RestoreRequest) (RestoreResult, error) {
	result := RestoreResult{ID: newID("restore_"), JobID: request.jobID, CreatedAt: s.now(), Available: true, DestinationID: d.ID, SnapshotID: request.SnapshotID, Test: request.Test}
	if !snapshotID.MatchString(request.SnapshotID) {
		return result, fmt.Errorf("%w: snapshot_id must be a hexadecimal snapshot ID", ErrInvalid)
	}
	if request.Test && (len(request.Includes) > 0 || len(request.SourceIDs) > 0 || len(request.Files) > 0) {
		return result, fmt.Errorf("%w: restore tests require the full recovery set", ErrInvalid)
	}
	if len(request.SourceIDs) > 1000 || len(request.Files) > 100 || len(request.Includes) > 1000 {
		return result, fmt.Errorf("%w: restore selection is too large", ErrInvalid)
	}
	if len(request.Includes) > 0 && (len(request.SourceIDs) > 0 || len(request.Files) > 0) || len(request.SourceIDs) > 0 && len(request.Files) > 0 {
		return result, fmt.Errorf("%w: choose sources, files, or legacy paths, not multiple selection modes", ErrInvalid)
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
	if err = s.registerRestoreResult(context.Background(), result); err != nil {
		return result, err
	}
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

// RunBackground executes at most one missed occurrence per assignment, then
// records the next due time. A scheduler resource conflict remains due for the
// next tick. Runs are synchronous here so shutdown waits for capture recovery.
func (s *Service) RunBackground(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		s.runScheduled(ctx)
		s.runMaintenance(ctx)
		s.cleanupExpired()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (s *Service) runScheduled(ctx context.Context) {
	s.mu.Lock()
	plans := make([]Plan, 0, len(s.state.Plans))
	for _, p := range s.state.Plans {
		plans = append(plans, clone(p))
	}
	s.mu.Unlock()
	sortPlans(plans)
	for _, p := range plans {
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
			if ok && (a.LastScheduledAt == nil || a.LastScheduledAt.Before(due)) {
				dueIDs = append(dueIDs, a.DestinationID)
				dueTimes[a.DestinationID] = due
			}
		}
		if len(dueIDs) == 0 {
			continue
		}
		job, run, err := s.StartRun(ctx, p.ID, RunRequest{DestinationIDs: dueIDs}, "scheduler")
		if err != nil {
			continue
		}
		s.mu.Lock()
		current := s.state.Plans[p.ID]
		for i := range current.Destinations {
			if due, ok := dueTimes[current.Destinations[i].DestinationID]; ok {
				current.Destinations[i].LastScheduledAt = &due
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
			continue
		}
		s.ExecuteRun(ctx, job, run.ID)
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
		if run.State != "running" && !s.now().Before(run.ExpiresAt) && safeID.MatchString(run.ID) {
			_ = os.RemoveAll(filepath.Join(s.directory, "staging", run.ID))
		}
	}
}
