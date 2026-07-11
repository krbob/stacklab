// Package imageupdates resolves whether locally pulled images are behind
// their registry tags (dashboard read-model contract, Slice B). v1 checks
// registries anonymously; images that need credentials or use build mode
// report state "unknown". Private registry endpoints explicitly named in an
// image reference are supported, while authentication challenges remain
// constrained to prevent registries from turning token lookup into SSRF.
package imageupdates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"stacklab/internal/store"
)

const (
	StateUpToDate  = "up_to_date"
	StateAvailable = "available"
	StateUnknown   = "unknown"

	maxRegistryResponseHeaderBytes = 64 << 10
	maxRegistryTokenResponseBytes  = 64 << 10
	maxRegistryTokenBytes          = 16 << 10
)

const manifestAcceptHeader = "application/vnd.docker.distribution.manifest.list.v2+json, " +
	"application/vnd.oci.image.index.v1+json, " +
	"application/vnd.docker.distribution.manifest.v2+json, " +
	"application/vnd.oci.image.manifest.v1+json"

var nonPublicRegistryPrefixes = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001:db8::/32"),
}

type Service struct {
	logger      *slog.Logger
	store       *store.Store
	client      *http.Client
	lookupNetIP func(ctx context.Context, host string) ([]netip.Addr, error)
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
		lookupNetIP: func(ctx context.Context, host string) ([]netip.Addr, error) {
			return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		},
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
		if ctx.Err() != nil {
			break
		}
		status := s.checkImage(ctx, ref)
		if ctx.Err() != nil {
			break
		}
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

	manifestURL, err := buildManifestURL(registry, repository, tag)
	if err != nil {
		return "", err
	}
	client, policy, err := s.registryHTTPClient(ctx, manifestURL)
	if err != nil {
		return "", err
	}
	defer client.CloseIdleConnections()
	digest, challenge, err := s.headManifest(ctx, client, policy, manifestURL, "")
	if err != nil {
		return "", err
	}
	if digest != "" {
		return digest, nil
	}
	if challenge == nil {
		return "", fmt.Errorf("registry did not return a digest")
	}

	token, err := s.fetchAnonymousToken(ctx, client, policy, challenge)
	if err != nil {
		return "", err
	}
	digest, _, err = s.headManifest(ctx, client, policy, manifestURL, token)
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

func (s *Service) headManifest(ctx context.Context, client *http.Client, policy *registryRequestPolicy, requestURL, token string) (string, *bearerChallenge, error) {
	if err := policy.validateURL(ctx, requestURL); err != nil {
		return "", nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodHead, requestURL, nil)
	if err != nil {
		return "", nil, err
	}
	request.Header.Set("Accept", manifestAcceptHeader)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}

	response, err := client.Do(request)
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

func (s *Service) fetchAnonymousToken(ctx context.Context, client *http.Client, policy *registryRequestPolicy, challenge *bearerChallenge) (string, error) {
	tokenURL, err := buildTokenURL(challenge)
	if err != nil {
		return "", err
	}
	if err := policy.validateParsedURL(ctx, tokenURL); err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, tokenURL.String(), nil)
	if err != nil {
		return "", err
	}
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token endpoint returned status %d", response.StatusCode)
	}

	if response.ContentLength > maxRegistryTokenResponseBytes {
		return "", fmt.Errorf("token endpoint response exceeds %d bytes", maxRegistryTokenResponseBytes)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxRegistryTokenResponseBytes+1))
	if err != nil {
		return "", fmt.Errorf("read token endpoint response: %w", err)
	}
	if len(body) > maxRegistryTokenResponseBytes {
		return "", fmt.Errorf("token endpoint response exceeds %d bytes", maxRegistryTokenResponseBytes)
	}
	var payload struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", err
	}
	token := payload.Token
	if token == "" {
		token = payload.AccessToken
	}
	if token == "" {
		return "", fmt.Errorf("token endpoint response did not contain a token")
	}
	if len(token) > maxRegistryTokenBytes {
		return "", fmt.Errorf("registry token exceeds %d bytes", maxRegistryTokenBytes)
	}
	return token, nil
}

func buildManifestURL(registry, repository, tag string) (string, error) {
	base, err := url.Parse("https://" + registry)
	if err != nil {
		return "", fmt.Errorf("parse registry URL: %w", err)
	}
	if base.User != nil || base.Hostname() == "" || base.Path != "" || base.RawQuery != "" || base.Fragment != "" {
		return "", fmt.Errorf("invalid registry authority %q", registry)
	}
	base.Path = "/v2/" + repository + "/manifests/" + tag
	return base.String(), nil
}

func buildTokenURL(challenge *bearerChallenge) (*url.URL, error) {
	if challenge == nil {
		return nil, fmt.Errorf("registry token challenge is missing")
	}
	tokenURL, err := url.Parse(challenge.Realm)
	if err != nil {
		return nil, fmt.Errorf("parse registry token realm: %w", err)
	}
	values := tokenURL.Query()
	values.Set("service", challenge.Service)
	if challenge.Scope != "" {
		values.Set("scope", challenge.Scope)
	}
	tokenURL.RawQuery = values.Encode()
	return tokenURL, nil
}

type registryEndpoint struct {
	host string
	port string
	key  string
}

type registryRequestPolicy struct {
	lookupNetIP            func(context.Context, string) ([]netip.Addr, error)
	pinnedPrivateEndpoints map[string]map[netip.Addr]struct{}
}

func (s *Service) registryHTTPClient(ctx context.Context, manifestURL string) (*http.Client, *registryRequestPolicy, error) {
	parsedManifestURL, err := url.Parse(manifestURL)
	if err != nil {
		return nil, nil, fmt.Errorf("parse manifest URL: %w", err)
	}
	lookupNetIP := s.lookupNetIP
	if lookupNetIP == nil {
		lookupNetIP = func(ctx context.Context, host string) ([]netip.Addr, error) {
			return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		}
	}
	policy, err := newRegistryRequestPolicy(ctx, parsedManifestURL, lookupNetIP)
	if err != nil {
		return nil, nil, err
	}

	template := s.client
	if template == nil {
		template = &http.Client{Timeout: 15 * time.Second}
	}
	baseTransport := template.Transport
	if baseTransport == nil {
		baseTransport = http.DefaultTransport
	}
	transport, ok := baseTransport.(*http.Transport)
	if !ok {
		return nil, nil, fmt.Errorf("registry HTTP client transport must be *http.Transport")
	}
	secureTransport := transport.Clone()
	secureTransport.Proxy = nil
	secureTransport.DialContext = policy.dialContext
	//lint:ignore SA1019 Clearing an inherited DialTLS hook prevents it from bypassing the pinned-address dialer.
	secureTransport.DialTLS = nil
	secureTransport.DialTLSContext = nil
	secureTransport.MaxResponseHeaderBytes = maxRegistryResponseHeaderBytes

	client := &http.Client{
		Transport: secureTransport,
		Timeout:   template.Timeout,
	}
	originalCheckRedirect := template.CheckRedirect
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("registry redirect limit exceeded")
		}
		if err := policy.validateParsedURL(request.Context(), request.URL); err != nil {
			return fmt.Errorf("reject registry redirect: %w", err)
		}
		if originalCheckRedirect != nil {
			return originalCheckRedirect(request, via)
		}
		return nil
	}
	return client, policy, nil
}

func newRegistryRequestPolicy(ctx context.Context, registryURL *url.URL, lookupNetIP func(context.Context, string) ([]netip.Addr, error)) (*registryRequestPolicy, error) {
	endpoint, err := parseRegistryEndpoint(registryURL)
	if err != nil {
		return nil, err
	}
	policy := &registryRequestPolicy{
		lookupNetIP:            lookupNetIP,
		pinnedPrivateEndpoints: make(map[string]map[netip.Addr]struct{}),
	}
	addresses, err := policy.lookupEndpoint(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	nonPublicCount := 0
	for _, address := range addresses {
		if isNonPublicAddress(address) {
			nonPublicCount++
		}
	}
	if nonPublicCount > 0 && nonPublicCount != len(addresses) {
		return nil, fmt.Errorf("registry endpoint %q resolves to a mix of public and non-public addresses", endpoint.key)
	}
	if nonPublicCount == len(addresses) {
		// Naming a private registry in the image reference is the explicit opt-in.
		// Pin its exact endpoint and addresses for this lookup; a challenge cannot
		// widen that permission to another LAN host or port.
		pinned := make(map[netip.Addr]struct{}, len(addresses))
		for _, address := range addresses {
			pinned[address] = struct{}{}
		}
		policy.pinnedPrivateEndpoints[endpoint.key] = pinned
	}
	return policy, nil
}

func (p *registryRequestPolicy) validateURL(ctx context.Context, rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("parse registry URL: %w", err)
	}
	return p.validateParsedURL(ctx, parsed)
}

func (p *registryRequestPolicy) validateParsedURL(ctx context.Context, parsed *url.URL) error {
	endpoint, err := parseRegistryEndpoint(parsed)
	if err != nil {
		return err
	}
	addresses, err := p.lookupEndpoint(ctx, endpoint)
	if err != nil {
		return err
	}
	return p.validateEndpointAddresses(endpoint, addresses)
}

func parseRegistryEndpoint(parsed *url.URL) (registryEndpoint, error) {
	if parsed == nil || !strings.EqualFold(parsed.Scheme, "https") {
		return registryEndpoint{}, fmt.Errorf("registry URL must use HTTPS")
	}
	if parsed.Opaque != "" || parsed.User != nil || parsed.Hostname() == "" || parsed.Fragment != "" {
		return registryEndpoint{}, fmt.Errorf("registry URL contains an invalid authority")
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if host == "" {
		return registryEndpoint{}, fmt.Errorf("registry URL host is empty")
	}
	port := parsed.Port()
	if port == "" {
		port = "443"
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return registryEndpoint{}, fmt.Errorf("registry URL has an invalid port")
	}
	return registryEndpoint{
		host: host,
		port: port,
		key:  net.JoinHostPort(host, port),
	}, nil
}

func (p *registryRequestPolicy) lookupEndpoint(ctx context.Context, endpoint registryEndpoint) ([]netip.Addr, error) {
	if literal, err := netip.ParseAddr(endpoint.host); err == nil {
		return []netip.Addr{literal.Unmap()}, nil
	}
	addresses, err := p.lookupNetIP(ctx, endpoint.host)
	if err != nil {
		return nil, fmt.Errorf("resolve registry endpoint %q: %w", endpoint.key, err)
	}
	result := make([]netip.Addr, 0, len(addresses))
	seen := make(map[netip.Addr]struct{}, len(addresses))
	for _, address := range addresses {
		address = address.Unmap()
		if !address.IsValid() {
			continue
		}
		if _, exists := seen[address]; exists {
			continue
		}
		seen[address] = struct{}{}
		result = append(result, address)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("registry endpoint %q did not resolve to an IP address", endpoint.key)
	}
	return result, nil
}

func (p *registryRequestPolicy) validateEndpointAddresses(endpoint registryEndpoint, addresses []netip.Addr) error {
	if pinned, ok := p.pinnedPrivateEndpoints[endpoint.key]; ok {
		for _, address := range addresses {
			if _, allowed := pinned[address]; !allowed {
				return fmt.Errorf("private registry endpoint %q changed address during the request", endpoint.key)
			}
		}
		return nil
	}
	for _, address := range addresses {
		if isNonPublicAddress(address) {
			return fmt.Errorf("registry endpoint %q resolves to disallowed address %s", endpoint.key, address)
		}
	}
	return nil
}

func (p *registryRequestPolicy) dialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("parse registry dial address: %w", err)
	}
	endpoint := registryEndpoint{
		host: strings.ToLower(strings.TrimSuffix(host, ".")),
		port: port,
		key:  net.JoinHostPort(strings.ToLower(strings.TrimSuffix(host, ".")), port),
	}
	addresses, err := p.lookupEndpoint(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	if err := p.validateEndpointAddresses(endpoint, addresses); err != nil {
		return nil, err
	}

	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	dialErrors := make([]error, 0, len(addresses))
	for _, resolvedAddress := range addresses {
		connection, err := dialer.DialContext(ctx, network, net.JoinHostPort(resolvedAddress.String(), port))
		if err == nil {
			return connection, nil
		}
		dialErrors = append(dialErrors, err)
	}
	return nil, fmt.Errorf("dial registry endpoint %q: %w", endpoint.key, errors.Join(dialErrors...))
}

func isNonPublicAddress(address netip.Addr) bool {
	address = address.Unmap()
	if !address.IsGlobalUnicast() || address.IsPrivate() {
		return true
	}
	for _, prefix := range nonPublicRegistryPrefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
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
