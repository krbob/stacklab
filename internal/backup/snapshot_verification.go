package backup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const recoveryObjectsVerified = "recovery_objects_verified"

var errIncompleteSnapshot = errors.New("snapshot recovery objects are incomplete")
var errLegacySnapshot = errors.New("legacy recovery object requires a new verified backup")

type verificationContextKey struct{}

// A snapshot object only proves that restic committed a tree. In particular,
// exit status 3 can commit a tree with unreadable files missing. Compare every
// staged recovery object before adopting a snapshot left by an interrupted run.
// This checks completeness, not all repository data blocks (restic check).
func (s *Service) verifyRecoveryObjects(ctx context.Context, d Destination, id, stage string) error {
	manifest, err := os.ReadFile(filepath.Join(stage, "manifest.json"))
	if err != nil {
		return fmt.Errorf("read prepared recovery manifest: %w", err)
	}
	expected := map[string]int64{}
	err = filepath.WalkDir(stage, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, statErr := entry.Info()
		if statErr != nil {
			return statErr
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("prepared recovery set contains a non-regular object")
		}
		relative, relErr := filepath.Rel(stage, path)
		if relErr != nil {
			return relErr
		}
		expected["/"+filepath.ToSlash(relative)] = info.Size()
		return nil
	})
	if err != nil {
		return err
	}
	if err := s.verifySnapshotObjects(ctx, d, id, expected); err != nil {
		return err
	}
	ctx = context.WithValue(ctx, verificationContextKey{}, true)
	remote, err := s.restic(ctx, d, "", "dump", id, "/manifest.json")
	if err != nil {
		return fmt.Errorf("read snapshot manifest: %w", err)
	}
	if !bytes.Equal(bytes.TrimSpace(remote), bytes.TrimSpace(manifest)) {
		return fmt.Errorf("%w: manifest does not match the prepared recovery set", errIncompleteSnapshot)
	}
	return nil
}

func (s *Service) verifySnapshotObjects(ctx context.Context, d Destination, id string, expected map[string]int64) error {
	ctx = context.WithValue(ctx, verificationContextKey{}, true)
	output, err := s.restic(ctx, d, "", "ls", "--recursive", id)
	if err != nil {
		return err
	}
	for _, line := range bytes.Split(output, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var node struct {
			Path string `json:"path"`
			Type string `json:"type"`
			Size int64  `json:"size"`
		}
		if json.Unmarshal(line, &node) != nil {
			return fmt.Errorf("invalid repository file listing")
		}
		size, needed := expected[node.Path]
		if !needed {
			continue
		}
		if node.Type != "file" || node.Size < 0 || size >= 0 && node.Size != size {
			return fmt.Errorf("%w: recovery object has an unexpected type or size", errIncompleteSnapshot)
		}
		delete(expected, node.Path)
	}
	if len(expected) != 0 {
		return fmt.Errorf("%w: required recovery object is missing", errIncompleteSnapshot)
	}
	return nil
}

// Existing installations may only have a metadata checkpoint. Revalidate its
// authenticated manifest and all referenced objects before calling it usable.
func (s *Service) verifySavedSnapshot(ctx context.Context, d Destination, id string) error {
	ctx = context.WithValue(ctx, verificationContextKey{}, true)
	// Establish absence using a complete listing, never a transport error.
	if err := s.verifySnapshotObjects(ctx, d, id, map[string]int64{"/manifest.json": -1}); err != nil {
		return err
	}
	manifest, err := s.snapshotManifest(ctx, d, id)
	if err != nil {
		return err
	}
	expected := map[string]int64{"/manifest.json": -1}
	for _, source := range manifest.Sources {
		if !source.Archive {
			return errLegacySnapshot
		}
		expected["/"+strings.TrimPrefix(source.SavedPath, "/")] = source.SizeBytes
		if source.IndexPath != "" {
			expected["/"+source.IndexPath] = -1
		}
	}
	return s.verifySnapshotObjects(ctx, d, id, expected)
}
