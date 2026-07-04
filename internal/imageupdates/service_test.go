package imageupdates

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
