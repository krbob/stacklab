package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

type RestoreReview struct {
	ReviewToken string         `json:"review_token"`
	ExpiresAt   time.Time      `json:"expires_at"`
	Preview     RestorePreview `json:"preview"`
}
type restoreChallenge struct {
	Fingerprint string
	ExpiresAt   time.Time
}
type recoveryCapturer interface {
	PreviewRestore(context.Context, Manifest, []RestoreMapping) (RestorePreview, error)
	ApplyRestore(context.Context, Manifest, []RestoreMapping, string, string, bool) error
}

func recoveryFingerprint(ctx context.Context, destinationID string, request RestoreRequest, manifest Manifest, preview RestorePreview) string {
	request.ReviewToken = ""
	data, _ := json.Marshal(struct {
		DestinationID string
		Request       RestoreRequest
		Manifest      Manifest
		Preview       RestorePreview
	}{destinationID, request, manifest, preview})
	digest := sha256.New()
	_, _ = digest.Write(data)
	for _, mapping := range preview.Mappings {
		for _, source := range manifest.Sources {
			if source.Source.ID != mapping.SourceID {
				continue
			}
			file, err := os.Open(filepath.Join(request.RestoredDirectory, filepath.FromSlash(source.SavedPath)))
			if err != nil {
				return ""
			}
			_, err = io.Copy(digest, contextReader{ctx: ctx, reader: file})
			_ = file.Close()
			if err != nil {
				return ""
			}
		}
	}
	return hex.EncodeToString(digest.Sum(nil))
}
func (s *Service) recoveryPreview(ctx context.Context, destinationID string, request RestoreRequest) (Manifest, RestorePreview, error) {
	var manifest Manifest
	var preview RestorePreview
	capture, ok := s.capture.(recoveryCapturer)
	if !ok {
		return manifest, preview, fmt.Errorf("reviewed recovery unavailable")
	}
	if !filepath.IsAbs(request.RestoredDirectory) || len(request.Mappings) == 0 {
		return manifest, preview, fmt.Errorf("%w: restored_directory and mappings are required", ErrInvalid)
	}
	resolved, err := filepath.EvalSymlinks(request.RestoredDirectory)
	if err != nil || resolved != filepath.Clean(request.RestoredDirectory) {
		return manifest, preview, fmt.Errorf("%w: restored directory must exist and not contain symlinks", ErrInvalid)
	}
	s.mu.Lock()
	_, exists := s.state.Destinations[destinationID]
	for _, mapping := range request.Mappings {
		for _, d := range s.state.Destinations {
			if d.Kind == "local" && mapping.Target != "" && overlaps(mapping.Target, d.Repository) {
				s.mu.Unlock()
				return manifest, preview, fmt.Errorf("%w: recovery target overlaps a repository", ErrInvalid)
			}
		}
	}
	s.mu.Unlock()
	if !exists {
		return manifest, preview, ErrNotFound
	}
	manifest, err = s.ReadRestoreManifest(ctx, request.RestoredDirectory)
	if err != nil {
		return manifest, preview, err
	}
	preview, err = capture.PreviewRestore(ctx, manifest, request.Mappings)
	if err != nil {
		return manifest, preview, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	// Archive bytes and permissions can change between preview and commit; include
	// their hashes in the token and validate every selected tar before stopping.
	for _, mapping := range preview.Mappings {
		for _, source := range manifest.Sources {
			if source.Source.ID != mapping.SourceID {
				continue
			}
			archive := filepath.Join(request.RestoredDirectory, filepath.FromSlash(source.SavedPath))
			if !isWithin(request.RestoredDirectory, archive) {
				return manifest, preview, fmt.Errorf("invalid recovery archive path")
			}
			info, statErr := os.Lstat(archive)
			canonical, canonicalErr := filepath.EvalSymlinks(archive)
			if statErr != nil || !info.Mode().IsRegular() || canonicalErr != nil || canonical != archive {
				return manifest, preview, fmt.Errorf("recovery archive must be a regular file")
			}
			file, openErr := os.Open(archive)
			if openErr != nil {
				return manifest, preview, openErr
			}
			validateErr := ValidateArchive(file, "")
			_ = file.Close()
			if validateErr != nil {
				return manifest, preview, fmt.Errorf("unsafe recovery archive: %w", validateErr)
			}
		}
	}
	return manifest, preview, nil
}
func (s *Service) PreviewRestore(ctx context.Context, destinationID string, request RestoreRequest) (RestoreReview, error) {
	manifest, preview, err := s.recoveryPreview(ctx, destinationID, request)
	if err != nil {
		return RestoreReview{}, err
	}
	fingerprint := recoveryFingerprint(ctx, destinationID, request, manifest, preview)
	if fingerprint == "" {
		return RestoreReview{}, fmt.Errorf("could not fingerprint recovery archives")
	}
	token := newID("review_")
	expires := s.now().Add(10 * time.Minute)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reviews == nil {
		s.reviews = map[string]restoreChallenge{}
	}
	for key, challenge := range s.reviews {
		if !s.now().Before(challenge.ExpiresAt) {
			delete(s.reviews, key)
		}
	}
	if len(s.reviews) >= 64 {
		return RestoreReview{}, fmt.Errorf("%w: too many outstanding recovery previews", ErrBusy)
	}
	s.reviews[token] = restoreChallenge{Fingerprint: fingerprint, ExpiresAt: expires}
	return RestoreReview{ReviewToken: token, ExpiresAt: expires, Preview: preview}, nil
}
func (s *Service) applyReviewedRestore(ctx context.Context, d Destination, request RestoreRequest) (RestoreResult, error) {
	result := RestoreResult{DestinationID: d.ID, SnapshotID: request.SnapshotID, Target: request.RestoredDirectory}
	manifest, preview, err := s.recoveryPreview(ctx, d.ID, request)
	if err != nil {
		return result, err
	}
	s.mu.Lock()
	challenge, ok := s.reviews[request.ReviewToken]
	delete(s.reviews, request.ReviewToken)
	s.mu.Unlock()
	if !ok || !s.now().Before(challenge.ExpiresAt) || challenge.Fingerprint != recoveryFingerprint(ctx, d.ID, request, manifest, preview) {
		return result, fmt.Errorf("%w: recovery review expired or inputs changed; preview again", ErrInvalid)
	}
	capture := s.capture.(recoveryCapturer)
	if err = capture.ApplyRestore(ctx, manifest, preview.Mappings, request.RestoredDirectory, newID("recovery_"), request.ResumeServices); err != nil {
		return result, err
	}
	result.Manifest = &manifest
	return result, nil
}

// ReadRestoreManifest only exposes manifests from successfully restored, registered recovery sets.
func (s *Service) ReadRestoreManifest(ctx context.Context, target string) (Manifest, error) {
	s.mu.Lock()
	_, ok := s.state.Restores[filepath.Clean(target)]
	s.mu.Unlock()
	if !ok {
		return Manifest{}, ErrNotFound
	}
	return LoadManifest(target)
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(data)
}
