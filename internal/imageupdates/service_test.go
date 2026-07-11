package imageupdates

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"stacklab/internal/store"
)

func TestSplitImageRef(t *testing.T) {
	cases := []struct {
		in         string
		registry   string
		repository string
		tag        string
	}{
		{"nginx", "registry-1.docker.io", "library/nginx", "latest"},
		{"nginx:1.27", "registry-1.docker.io", "library/nginx", "1.27"},
		{"adguard/adguardhome:latest", "registry-1.docker.io", "adguard/adguardhome", "latest"},
		{"ghcr.io/example/app:1.2", "ghcr.io", "example/app", "1.2"},
		{"lscr.io/linuxserver/transmission:latest", "lscr.io", "linuxserver/transmission", "latest"},
		{"registry.local:5000/tools/app", "registry.local:5000", "tools/app", "latest"},
		{"nginx@sha256:abc", "registry-1.docker.io", "library/nginx", "sha256:abc"},
	}
	for _, tc := range cases {
		registry, repository, tag := splitImageRef(tc.in)
		if registry != tc.registry || repository != tc.repository || tag != tc.tag {
			t.Errorf("splitImageRef(%q) = (%q,%q,%q), want (%q,%q,%q)", tc.in, registry, repository, tag, tc.registry, tc.repository, tc.tag)
		}
	}
}

func TestParseBearerChallenge(t *testing.T) {
	header := `Bearer realm="https://auth.docker.io/token",service="registry.docker.io",scope="repository:library/nginx:pull"`
	challenge := parseBearerChallenge(header)
	if challenge == nil {
		t.Fatal("challenge = nil")
	}
	if challenge.Realm != "https://auth.docker.io/token" || challenge.Service != "registry.docker.io" {
		t.Fatalf("challenge = %+v", challenge)
	}
	if parseBearerChallenge("Basic realm=x") != nil {
		t.Fatal("basic auth should be unsupported")
	}
}

func TestNormalizeRepository(t *testing.T) {
	a := normalizeRepository("nginx:1.27")
	b := normalizeRepository("docker.io/library/nginx")
	if a != b {
		t.Fatalf("normalizeRepository mismatch: %q vs %q", a, b)
	}
}

func TestStatusByImageUsesInMemoryCache(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	testStore, err := store.Open(filepath.Join(t.TempDir(), "stacklab.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = testStore.Close() })

	checkedAt := time.Date(2026, 7, 6, 3, 17, 0, 0, time.UTC)
	if err := testStore.UpsertImageUpdateStatus(ctx, store.ImageUpdateStatus{
		ImageRef:  "nginx:latest",
		State:     StateAvailable,
		CheckedAt: checkedAt,
	}); err != nil {
		t.Fatalf("UpsertImageUpdateStatus(initial) error = %v", err)
	}

	service := NewService(nil, testStore)
	if err := service.Load(ctx); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := service.StatusByImage()["nginx:latest"].State; got != StateAvailable {
		t.Fatalf("StatusByImage(initial) = %q, want %q", got, StateAvailable)
	}

	if err := testStore.UpsertImageUpdateStatus(ctx, store.ImageUpdateStatus{
		ImageRef:  "nginx:latest",
		State:     StateUnknown,
		CheckedAt: checkedAt.Add(time.Minute),
	}); err != nil {
		t.Fatalf("UpsertImageUpdateStatus(updated store) error = %v", err)
	}
	if got := service.StatusByImage()["nginx:latest"].State; got != StateAvailable {
		t.Fatalf("StatusByImage(after direct store write) = %q, want cached %q", got, StateAvailable)
	}

	service.CacheStatuses([]store.ImageUpdateStatus{{
		ImageRef:  "nginx:latest",
		State:     StateUnknown,
		CheckedAt: checkedAt.Add(time.Minute),
	}})
	if got := service.StatusByImage()["nginx:latest"].State; got != StateUnknown {
		t.Fatalf("StatusByImage(after cache update) = %q, want %q", got, StateUnknown)
	}
}

func TestCheckImageTreatsAnyMatchingLocalRepoDigestAsUpToDate(t *testing.T) {
	const currentDigest = "sha256:current"

	registry := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Fatalf("method = %s, want HEAD", r.Method)
		}
		if r.URL.Path != "/v2/example/app/manifests/latest" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		w.Header().Set("Docker-Content-Digest", currentDigest)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(registry.Close)

	registryHost := strings.TrimPrefix(registry.URL, "https://")
	imageRef := registryHost + "/example/app:latest"
	repoDigests, err := json.Marshal([]string{
		registryHost + "/example/app@sha256:old",
		registryHost + "/example/app@" + currentDigest,
	})
	if err != nil {
		t.Fatal(err)
	}

	service := &Service{
		client: registry.Client(),
		runDocker: func(ctx context.Context, args ...string) ([]byte, error) {
			return repoDigests, nil
		},
	}

	status := service.checkImage(context.Background(), imageRef)
	if status.State != StateUpToDate {
		t.Fatalf("state = %q, want %q (status=%+v)", status.State, StateUpToDate, status)
	}
	if status.LocalDigest != currentDigest {
		t.Fatalf("local digest = %q, want %q", status.LocalDigest, currentDigest)
	}
	if status.RemoteDigest != currentDigest {
		t.Fatalf("remote digest = %q, want %q", status.RemoteDigest, currentDigest)
	}
}

func TestRemoteDigestUsesValidatedBearerChallengeAndEncodedQuery(t *testing.T) {
	const currentDigest = "sha256:current"

	var manifestRequests atomic.Int32
	var registry *httptest.Server
	registry = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/example/app/manifests/latest":
			manifestRequests.Add(1)
			if r.Header.Get("Authorization") != "Bearer safe-token" {
				w.Header().Set("WWW-Authenticate", `Bearer realm="`+registry.URL+`/token?existing=keep",service="registry.example&admin=true",scope="repository:example/app:pull&other=1"`)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("Docker-Content-Digest", currentDigest)
			w.WriteHeader(http.StatusOK)
		case "/token":
			if got := r.URL.Query().Get("existing"); got != "keep" {
				t.Errorf("existing query = %q, want keep", got)
			}
			if got := r.URL.Query().Get("service"); got != "registry.example&admin=true" {
				t.Errorf("service query = %q", got)
			}
			if got := r.URL.Query().Get("scope"); got != "repository:example/app:pull&other=1" {
				t.Errorf("scope query = %q", got)
			}
			_, _ = io.WriteString(w, `{"access_token":"safe-token"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(registry.Close)

	service := &Service{client: registry.Client()}
	registryHost := strings.TrimPrefix(registry.URL, "https://")
	digest, err := service.remoteDigest(context.Background(), registryHost+"/example/app:latest")
	if err != nil {
		t.Fatalf("remoteDigest() error = %v", err)
	}
	if digest != currentDigest {
		t.Fatalf("remoteDigest() = %q, want %q", digest, currentDigest)
	}
	if got := manifestRequests.Load(); got != 2 {
		t.Fatalf("manifest request count = %d, want 2", got)
	}
}

func TestRemoteDigestRejectsPrivateTokenRealmOutsideExplicitRegistryEndpoint(t *testing.T) {
	var tokenEndpointHit atomic.Bool
	tokenEndpoint := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		tokenEndpointHit.Store(true)
		_, _ = io.WriteString(w, `{"token":"should-not-be-read"}`)
	}))
	t.Cleanup(tokenEndpoint.Close)

	var registry *httptest.Server
	registry = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="`+tokenEndpoint.URL+`/token",service="registry.test"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(registry.Close)

	service := &Service{client: registry.Client()}
	registryHost := strings.TrimPrefix(registry.URL, "https://")
	if _, err := service.remoteDigest(context.Background(), registryHost+"/example/app:latest"); err == nil || !strings.Contains(err.Error(), "disallowed address") {
		t.Fatalf("remoteDigest() error = %v, want disallowed private token endpoint", err)
	}
	if tokenEndpointHit.Load() {
		t.Fatal("disallowed token endpoint received a request")
	}
}

func TestRemoteDigestValidatesTokenRedirects(t *testing.T) {
	var redirectTargetHit atomic.Bool
	redirectTarget := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		redirectTargetHit.Store(true)
		_, _ = io.WriteString(w, `{"token":"should-not-be-read"}`)
	}))
	t.Cleanup(redirectTarget.Close)

	var registry *httptest.Server
	registry = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			http.Redirect(w, r, redirectTarget.URL+"/token", http.StatusFound)
			return
		}
		w.Header().Set("WWW-Authenticate", `Bearer realm="`+registry.URL+`/token",service="registry.test"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(registry.Close)

	service := &Service{client: registry.Client()}
	registryHost := strings.TrimPrefix(registry.URL, "https://")
	if _, err := service.remoteDigest(context.Background(), registryHost+"/example/app:latest"); err == nil || !strings.Contains(err.Error(), "reject registry redirect") {
		t.Fatalf("remoteDigest() error = %v, want rejected redirect", err)
	}
	if redirectTargetHit.Load() {
		t.Fatal("disallowed redirect target received a request")
	}
}

func TestRemoteDigestBoundsTokenResponse(t *testing.T) {
	var registry *httptest.Server
	registry = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			_, _ = io.WriteString(w, `{"token":"`+strings.Repeat("x", maxRegistryTokenResponseBytes)+`"}`)
			return
		}
		w.Header().Set("WWW-Authenticate", `Bearer realm="`+registry.URL+`/token",service="registry.test"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(registry.Close)

	service := &Service{client: registry.Client()}
	registryHost := strings.TrimPrefix(registry.URL, "https://")
	if _, err := service.remoteDigest(context.Background(), registryHost+"/example/app:latest"); err == nil || !strings.Contains(err.Error(), "response exceeds") {
		t.Fatalf("remoteDigest() error = %v, want bounded response error", err)
	}
}

func TestRegistryRequestPolicyRejectsHTTPPrivateAuthAndDNSRebinding(t *testing.T) {
	lookupCalls := 0
	lookup := func(_ context.Context, host string) ([]netip.Addr, error) {
		lookupCalls++
		if host != "registry.example" {
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		}
		if lookupCalls == 1 {
			return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	}
	registryURL, err := url.Parse("https://registry.example/v2/example/app/manifests/latest")
	if err != nil {
		t.Fatal(err)
	}
	policy, err := newRegistryRequestPolicy(context.Background(), registryURL, lookup)
	if err != nil {
		t.Fatalf("newRegistryRequestPolicy() error = %v", err)
	}
	if err := policy.validateURL(context.Background(), "https://registry.example/token"); err == nil || !strings.Contains(err.Error(), "disallowed address") {
		t.Fatalf("validateURL(rebound host) error = %v, want disallowed address", err)
	}
	if err := policy.validateURL(context.Background(), "http://registry.example/token"); err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("validateURL(http) error = %v, want HTTPS error", err)
	}
}

func TestCheckImagesDoesNotOverwriteStatusesAfterCancellation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	testStore, err := store.Open(filepath.Join(t.TempDir(), "stacklab.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = testStore.Close() })

	checkedAt := time.Date(2026, 7, 6, 3, 17, 0, 0, time.UTC)
	if err := testStore.UpsertImageUpdateStatus(ctx, store.ImageUpdateStatus{
		ImageRef:  "nginx:latest",
		State:     StateAvailable,
		CheckedAt: checkedAt,
	}); err != nil {
		t.Fatalf("UpsertImageUpdateStatus(initial) error = %v", err)
	}

	service := NewService(nil, testStore)
	if err := service.Load(ctx); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	results := service.CheckImages(cancelledCtx, []string{"nginx:latest"}, nil)
	if len(results) != 0 {
		t.Fatalf("len(results) = %d, want 0: %#v", len(results), results)
	}
	if got := service.StatusByImage()["nginx:latest"].State; got != StateAvailable {
		t.Fatalf("cached state after cancellation = %q, want %q", got, StateAvailable)
	}
	items, err := testStore.ListImageUpdateStatus(context.Background())
	if err != nil {
		t.Fatalf("ListImageUpdateStatus() error = %v", err)
	}
	if len(items) != 1 || items[0].State != StateAvailable {
		t.Fatalf("stored statuses after cancellation = %#v, want available", items)
	}
}
