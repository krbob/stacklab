package backup

import (
	"context"
	"fmt"
	"path/filepath"
	"time"
)

// Reject local input errors before reserving a repository or accepting a job.
// Execution repeats state-dependent validation after admission.
func (s *Service) ValidateRepositoryRequest(ctx context.Context, id, action string, check CheckRequest, request RestoreRequest) error {
	if action == "backup_check" {
		if check.ReadData != "" && check.ReadData != "metadata" && check.ReadData != "all" && check.ReadData != "subset" {
			return fmt.Errorf("%w: read_data must be metadata, all or subset", ErrInvalid)
		}
	}
	if action != "backup_restore" {
		return nil
	}
	if request.RestoreID != "" || request.RestoredDirectory != "" {
		s.mu.Lock()
		recoveryError := s.recoveryError
		s.mu.Unlock()
		if recoveryError != "" {
			return fmt.Errorf("%w: backup recovery requires attention: %s", ErrBusy, recoveryError)
		}
		_, err := s.normalizeRecoveryRequest(ctx, id, request)
		return err
	}
	if len(request.Mappings) > 0 {
		return fmt.Errorf("%w: source mappings require a registered restore_id or restored_directory", ErrInvalid)
	}
	if err := validateRestoreSelection(request); err != nil {
		return err
	}
	s.mu.Lock()
	libraryFull := len(s.state.Restores) >= 200
	s.mu.Unlock()
	if libraryFull {
		return fmt.Errorf("%w: recovery library is full; remove an old result first", ErrInvalid)
	}
	if len(request.Files) > 0 {
		return s.validateSelectedFileEntries(ctx, id, request)
	}
	return nil
}

// Consult each bounded authenticated index once before accepting a costly job.
// Unindexed legacy archives retain execution-time validation after retrieval.
func (s *Service) validateSelectedFileEntries(ctx context.Context, id string, request RestoreRequest) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	d, release, err := s.beginRepositoryRead(ctx, id)
	if err != nil {
		return err
	}
	defer release()
	manifest, err := s.snapshotManifest(ctx, d, request.SnapshotID)
	if err != nil {
		return err
	}
	indexes := map[string]map[string]FileEntry{}
	for _, selected := range request.Files {
		entries, loaded := indexes[selected.SourceID]
		if !loaded {
			source, sourceErr := catalogSource(manifest, selected.SourceID)
			if sourceErr != nil {
				return fmt.Errorf("%w: unknown selected source", ErrInvalid)
			}
			index, available, indexErr := s.snapshotIndex(ctx, d, request.SnapshotID, source)
			if indexErr != nil {
				return indexErr
			}
			if available {
				entries = map[string]FileEntry{}
				for _, entry := range index.Entries {
					entries[entry.Path] = entry
				}
			}
			indexes[selected.SourceID] = entries
		}
		if entries == nil {
			continue
		}
		name, _ := catalogPath(selected.Path, false)
		entry, found := entries[name]
		if !found {
			return fmt.Errorf("%w: selected file is not in the captured index", ErrInvalid)
		}
		if entry.Type != "file" {
			return fmt.Errorf("%w: selected entry is %s, not a regular file; choose its regular-file target or restore the whole source", ErrInvalid, entry.Type)
		}
	}
	return nil
}

func validateRestoreSelection(request RestoreRequest) error {
	if !snapshotID.MatchString(request.SnapshotID) {
		return fmt.Errorf("%w: snapshot_id must be a hexadecimal snapshot ID", ErrInvalid)
	}
	if request.Test && (len(request.Includes) > 0 || len(request.SourceIDs) > 0 || len(request.Files) > 0) {
		return fmt.Errorf("%w: restore tests require the full recovery set", ErrInvalid)
	}
	if len(request.SourceIDs) > 1000 || len(request.Files) > 100 || len(request.Includes) > 1000 {
		return fmt.Errorf("%w: restore selection is too large", ErrInvalid)
	}
	if len(request.Includes) > 0 && (len(request.SourceIDs) > 0 || len(request.Files) > 0) || len(request.SourceIDs) > 0 && len(request.Files) > 0 {
		return fmt.Errorf("%w: choose sources, files, or legacy paths, not multiple selection modes", ErrInvalid)
	}
	if request.Target != "" && !filepath.IsAbs(request.Target) {
		return fmt.Errorf("%w: restore target must be absolute", ErrInvalid)
	}
	for _, file := range request.Files {
		if !safeID.MatchString(file.SourceID) {
			return fmt.Errorf("%w: invalid source ID", ErrInvalid)
		}
		if _, err := catalogPath(file.Path, false); err != nil {
			return err
		}
	}
	for _, source := range request.SourceIDs {
		if !safeID.MatchString(source) {
			return fmt.Errorf("%w: invalid source ID", ErrInvalid)
		}
	}
	return nil
}
