// Package imageupdates resolves whether locally pulled images are behind
// their registry tags (dashboard read-model contract, Slice B). v1 checks
// public registries anonymously; images that need credentials or use build
// mode report state "unknown".
package imageupdates

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"stacklab/internal/store"
)

const (
	StateUpToDate  = "up_to_date"
	StateAvailable = "available"
	StateUnknown   = "unknown"
)

const manifestAcceptHeader = "application/vnd.docker.distribution.manifest.list.v2+json, " +
	"application/vnd.oci.image.index.v1+json, " +
	"application/vnd.docker.distribution.manifest.v2+json, " +
	"application/vnd.oci.image.manifest.v1+json"

type Service struct {
	logger *slog.Logger
	store  *store.Store
	client *http.Client
	// runDocker is injectable for tests; production shells out to the docker CLI
	// like the rest of the runtime integration.
	runDocker func(ctx context.Context, args ...string) ([]byte, error)

	mu    sync.RWMutex
	cache map[string]store.ImageUpdateStatus
}

func NewService(logger *slog.Logger, dataStore *store.Store) *Service {
	return &Service{
		logger: logger,
		store:  dataStore,
		client: &http.Client{Timeout: 15 * time.Second},
		runDocker: func(ctx context.Context, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, "docker", args...).Output()
		},
		cache: map[string]store.ImageUpdateStatus{},
	}
}

// Load primes the in-memory cache from SQLite so the stacks rollup works
// right after boot without waiting for a check run.
func (s *Service) Load(ctx context.Context) error {
	items, err := s.store.ListImageUpdateStatus(ctx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	for _, item := range items {
		s.cache[item.ImageRef] = item
	}
	s.mu.Unlock()
	return nil
}

// StatusByImage returns the cached per-image state.
func (s *Service) StatusByImage() map[string]store.ImageUpdateStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string]store.ImageUpdateStatus, len(s.cache))
	for ref, status := range s.cache {
		result[ref] = status
	}
	return result
}

// CacheStatuses applies externally persisted status changes, such as stack
// invalidations, to the in-memory hot-path cache.
func (s *Service) CacheStatuses(statuses []store.ImageUpdateStatus) {
	if len(statuses) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cache == nil {
		s.cache = map[string]store.ImageUpdateStatus{}
	}
	for _, status := range statuses {
		s.cache[status.ImageRef] = status
	}
}

// CheckImages resolves the update state for every given image ref, persists
// the results, and reports progress through onProgress (may be nil).
func (s *Service) CheckImages(ctx context.Context, imageRefs []string, onProgress func(done, total int, detail string)) []store.ImageUpdateStatus {
	unique := uniqueSorted(imageRefs)
	results := make([]store.ImageUpdateStatus, 0, len(unique))

	for index, ref := range unique {
		status := s.checkImage(ctx, ref)
		results = append(results, status)

		if err := s.store.UpsertImageUpdateStatus(ctx, status); err != nil && s.logger != nil {
			s.logger.Warn("persist image update status failed", slog.String("image", ref), slog.String("err", err.Error()))
		}
		s.CacheStatuses([]store.ImageUpdateStatus{status})

		if onProgress != nil {
			onProgress(index+1, len(unique), ref+" → "+status.State)
		}
	}
	return results
}

func (s *Service) checkImage(ctx context.Context, imageRef string) store.ImageUpdateStatus {
	status := store.ImageUpdateStatus{
		ImageRef:  imageRef,
		State:     StateUnknown,
		CheckedAt: time.Now().UTC(),
	}

	localDigests := s.localRepoDigests(ctx, imageRef)
	if len(localDigests) == 0 {
		return status
	}
	status.LocalDigest = localDigests[0]

	remoteDigest, err := s.remoteDigest(ctx, imageRef)
	if err != nil {
		if s.logger != nil {
			s.logger.Debug("remote digest lookup failed", slog.String("image", imageRef), slog.String("err", err.Error()))
		}
		return status
	}
	status.RemoteDigest = remoteDigest

	if containsDigest(localDigests, remoteDigest) {
		status.LocalDigest = remoteDigest
		status.State = StateUpToDate
	} else {
		status.State = StateAvailable
	}
	return status
}

// localRepoDigests returns the digests the local image is known by, matched
// against the repository of imageRef. Docker may retain multiple RepoDigests for
// the same tag/repository, so callers must compare against the full set.
func (s *Service) localRepoDigests(ctx context.Context, imageRef string) []string {
	output, err := s.runDocker(ctx, "image", "inspect", "--format", "{{json .RepoDigests}}", imageRef)
	if err != nil {
		return nil
	}
	var repoDigests []string
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(output))), &repoDigests); err != nil {
		return nil
	}

	repo := normalizeRepository(imageRef)
	digests := make([]string, 0, len(repoDigests))
	for _, entry := range repoDigests {
		name, digest, ok := strings.Cut(entry, "@")
		if !ok {
			continue
		}
		if normalizeRepository(name) == repo {
			digests = append(digests, digest)
		}
	}
	if len(digests) == 0 && len(repoDigests) == 1 {
		if _, digest, ok := strings.Cut(repoDigests[0], "@"); ok {
			digests = append(digests, digest)
		}
	}
	return digests
}

func containsDigest(digests []string, candidate string) bool {
	for _, digest := range digests {
		if digest == candidate {
			return true
		}
	}
	return false
}

func (s *Service) remoteDigest(ctx context.Context, imageRef string) (string, error) {
	registry, repository, tag := splitImageRef(imageRef)

	manifestURL := fmt.Sprintf("https://%s/v2/%s/manifests/%s", registry, repository, tag)
	digest, challenge, err := s.headManifest(ctx, manifestURL, "")
	if err != nil {
		return "", err
	}
	if digest != "" {
		return digest, nil
	}
	if challenge == nil {
		return "", fmt.Errorf("registry did not return a digest")
	}

	token, err := s.fetchAnonymousToken(ctx, challenge)
	if err != nil {
		return "", err
	}
	digest, _, err = s.headManifest(ctx, manifestURL, token)
	if err != nil {
		return "", err
	}
	if digest == "" {
		return "", fmt.Errorf("registry did not return a digest after auth")
	}
	return digest, nil
}

type bearerChallenge struct {
	Realm   string
	Service string
	Scope   string
}

func (s *Service) headManifest(ctx context.Context, url, token string) (string, *bearerChallenge, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return "", nil, err
	}
	request.Header.Set("Accept", manifestAcceptHeader)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}

	response, err := s.client.Do(request)
	if err != nil {
		return "", nil, err
	}
	defer response.Body.Close()

	switch {
	case response.StatusCode == http.StatusOK:
		digest := response.Header.Get("Docker-Content-Digest")
		if digest == "" {
			return "", nil, fmt.Errorf("registry response missing Docker-Content-Digest")
		}
		return digest, nil, nil
	case response.StatusCode == http.StatusUnauthorized && token == "":
		challenge := parseBearerChallenge(response.Header.Get("WWW-Authenticate"))
		if challenge == nil {
			return "", nil, fmt.Errorf("registry requires unsupported authentication")
		}
		return "", challenge, nil
	default:
		return "", nil, fmt.Errorf("registry returned status %d", response.StatusCode)
	}
}

func (s *Service) fetchAnonymousToken(ctx context.Context, challenge *bearerChallenge) (string, error) {
	url := challenge.Realm + "?service=" + challenge.Service
	if challenge.Scope != "" {
		url += "&scope=" + challenge.Scope
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	response, err := s.client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token endpoint returned status %d", response.StatusCode)
	}

	var payload struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return "", err
	}
	if payload.Token != "" {
		return payload.Token, nil
	}
	return payload.AccessToken, nil
}

func parseBearerChallenge(header string) *bearerChallenge {
	if !strings.HasPrefix(header, "Bearer ") {
		return nil
	}
	challenge := &bearerChallenge{}
	for _, part := range strings.Split(strings.TrimPrefix(header, "Bearer "), ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		value = strings.Trim(value, `"`)
		switch key {
		case "realm":
			challenge.Realm = value
		case "service":
			challenge.Service = value
		case "scope":
			challenge.Scope = value
		}
	}
	if challenge.Realm == "" {
		return nil
	}
	return challenge
}

// splitImageRef resolves registry host, repository path, and tag with Docker
// Hub conventions (implicit docker.io, implicit library/ namespace, implicit
// latest tag). Digest-pinned refs keep the digest as the "tag" — HEAD by
// digest is valid and always up to date.
func splitImageRef(imageRef string) (registry, repository, tag string) {
	remainder := imageRef
	registry = "registry-1.docker.io"

	if name, digest, ok := strings.Cut(remainder, "@"); ok {
		remainder = name
		tag = digest
	}

	firstSegment, rest, hasSlash := strings.Cut(remainder, "/")
	if hasSlash && (strings.ContainsAny(firstSegment, ".:") || firstSegment == "localhost") {
		registry = firstSegment
		remainder = rest
	}
	// Docker Hub aliases all resolve to the same registry endpoint.
	if registry == "docker.io" || registry == "index.docker.io" {
		registry = "registry-1.docker.io"
	}

	if tag == "" {
		if name, parsedTag, ok := strings.Cut(remainder, ":"); ok && !strings.Contains(parsedTag, "/") {
			remainder = name
			tag = parsedTag
		} else {
			tag = "latest"
		}
	} else if name, _, ok := strings.Cut(remainder, ":"); ok {
		remainder = name
	}

	if registry == "registry-1.docker.io" && !strings.Contains(remainder, "/") {
		remainder = "library/" + remainder
	}
	return registry, remainder, tag
}

func normalizeRepository(imageRef string) string {
	registry, repository, _ := splitImageRef(imageRef)
	return registry + "/" + repository
}

func uniqueSorted(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
