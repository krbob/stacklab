package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type domainRoute struct {
	method  string
	path    string
	pattern string
}

func domainRouteContract() []domainRoute {
	return []domainRoute{
		{http.MethodGet, "/api/session", "GET /api/session"},
		{http.MethodPost, "/api/auth/login", "POST /api/auth/login"},
		{http.MethodPost, "/api/auth/logout", "POST /api/auth/logout"},
		{http.MethodGet, "/api/live", "GET /api/live"},
		{http.MethodGet, "/api/ready", "GET /api/ready"},
		{http.MethodGet, "/api/health", "GET /api/health"},
		{http.MethodGet, "/api/ws", "GET /api/ws"},
		{http.MethodGet, "/api/meta", "GET /api/meta"},
		{http.MethodGet, "/api/service/metrics", "GET /api/service/metrics"},
		{http.MethodGet, "/api/host/overview", "GET /api/host/overview"},
		{http.MethodGet, "/api/host/metrics", "GET /api/host/metrics"},
		{http.MethodGet, "/api/host/stacklab-logs", "GET /api/host/stacklab-logs"},
		{http.MethodGet, "/api/docker/admin/overview", "GET /api/docker/admin/overview"},
		{http.MethodGet, "/api/docker/admin/daemon-config", "GET /api/docker/admin/daemon-config"},
		{http.MethodPost, "/api/docker/admin/daemon-config/validate", "POST /api/docker/admin/daemon-config/validate"},
		{http.MethodPost, "/api/docker/admin/daemon-config/apply", "POST /api/docker/admin/daemon-config/apply"},
		{http.MethodGet, "/api/docker/registries", "GET /api/docker/registries"},
		{http.MethodPost, "/api/docker/registries/login", "POST /api/docker/registries/login"},
		{http.MethodPost, "/api/docker/registries/logout", "POST /api/docker/registries/logout"},
		{http.MethodGet, "/api/stacklab/update/overview", "GET /api/stacklab/update/overview"},
		{http.MethodPost, "/api/stacklab/update/apply", "POST /api/stacklab/update/apply"},
		{http.MethodGet, "/api/config/workspace/tree", "GET /api/config/workspace/tree"},
		{http.MethodGet, "/api/config/workspace/file", "GET /api/config/workspace/file"},
		{http.MethodPut, "/api/config/workspace/file", "PUT /api/config/workspace/file"},
		{http.MethodPost, "/api/config/workspace/repair-permissions", "POST /api/config/workspace/repair-permissions"},
		{http.MethodGet, "/api/git/workspace/status", "GET /api/git/workspace/status"},
		{http.MethodGet, "/api/git/workspace/diff", "GET /api/git/workspace/diff"},
		{http.MethodPost, "/api/git/workspace/commit", "POST /api/git/workspace/commit"},
		{http.MethodPost, "/api/git/workspace/push", "POST /api/git/workspace/push"},
		{http.MethodPost, "/api/maintenance/update-stacks", "POST /api/maintenance/update-stacks"},
		{http.MethodGet, "/api/maintenance/images", "GET /api/maintenance/images"},
		{http.MethodGet, "/api/templates", "GET /api/templates"},
		{http.MethodGet, "/api/maintenance/image-updates", "GET /api/maintenance/image-updates"},
		{http.MethodPost, "/api/maintenance/image-updates/check", "POST /api/maintenance/image-updates/check"},
		{http.MethodGet, "/api/maintenance/networks", "GET /api/maintenance/networks"},
		{http.MethodPost, "/api/maintenance/networks", "POST /api/maintenance/networks"},
		{http.MethodDelete, "/api/maintenance/networks/demo", "DELETE /api/maintenance/networks/{name}"},
		{http.MethodGet, "/api/maintenance/volumes", "GET /api/maintenance/volumes"},
		{http.MethodPost, "/api/maintenance/volumes", "POST /api/maintenance/volumes"},
		{http.MethodDelete, "/api/maintenance/volumes/demo", "DELETE /api/maintenance/volumes/{name}"},
		{http.MethodGet, "/api/maintenance/prune-preview", "GET /api/maintenance/prune-preview"},
		{http.MethodPost, "/api/maintenance/prune", "POST /api/maintenance/prune"},
		{http.MethodGet, "/api/stacks", "GET /api/stacks"},
		{http.MethodPost, "/api/stacks", "POST /api/stacks"},
		{http.MethodGet, "/api/stacks/demo", "GET /api/stacks/{stackId}"},
		{http.MethodDelete, "/api/stacks/demo", "DELETE /api/stacks/{stackId}"},
		{http.MethodGet, "/api/stacks/demo/definition", "GET /api/stacks/{stackId}/definition"},
		{http.MethodPut, "/api/stacks/demo/definition", "PUT /api/stacks/{stackId}/definition"},
		{http.MethodGet, "/api/stacks/demo/workspace/tree", "GET /api/stacks/{stackId}/workspace/tree"},
		{http.MethodGet, "/api/stacks/demo/workspace/file", "GET /api/stacks/{stackId}/workspace/file"},
		{http.MethodPut, "/api/stacks/demo/workspace/file", "PUT /api/stacks/{stackId}/workspace/file"},
		{http.MethodPost, "/api/stacks/demo/workspace/repair-permissions", "POST /api/stacks/{stackId}/workspace/repair-permissions"},
		{http.MethodGet, "/api/stacks/demo/resolved-config", "GET /api/stacks/{stackId}/resolved-config"},
		{http.MethodPost, "/api/stacks/demo/resolved-config", "POST /api/stacks/{stackId}/resolved-config"},
		{http.MethodPost, "/api/stacks/demo/actions/up", "POST /api/stacks/{stackId}/actions/{action}"},
		{http.MethodGet, "/api/jobs/active", "GET /api/jobs/active"},
		{http.MethodGet, "/api/jobs/job_demo/events", "GET /api/jobs/{jobId}/events"},
		{http.MethodPost, "/api/jobs/job_demo/cancel", "POST /api/jobs/{jobId}/cancel"},
		{http.MethodGet, "/api/jobs/job_demo", "GET /api/jobs/{jobId}"},
		{http.MethodGet, "/api/stacks/demo/audit", "GET /api/stacks/{stackId}/audit"},
		{http.MethodGet, "/api/audit", "GET /api/audit"},
		{http.MethodGet, "/api/settings/notifications", "GET /api/settings/notifications"},
		{http.MethodPut, "/api/settings/notifications", "PUT /api/settings/notifications"},
		{http.MethodPost, "/api/settings/notifications/test", "POST /api/settings/notifications/test"},
		{http.MethodGet, "/api/settings/host", "GET /api/settings/host"},
		{http.MethodPut, "/api/settings/host", "PUT /api/settings/host"},
		{http.MethodGet, "/api/settings/maintenance-schedules", "GET /api/settings/maintenance-schedules"},
		{http.MethodPut, "/api/settings/maintenance-schedules", "PUT /api/settings/maintenance-schedules"},
		{http.MethodPost, "/api/settings/password", "POST /api/settings/password"},
	}
}

func TestHandlerRegistersDomainRouteContract(t *testing.T) {
	t.Parallel()

	handler := &Handler{mux: http.NewServeMux()}
	handler.registerRoutes()
	routes := domainRouteContract()

	if len(routes) != 69 {
		t.Fatalf("route contract contains %d operations, want 69", len(routes))
	}
	for _, route := range routes {
		route := route
		t.Run(route.pattern, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequest(route.method, "http://stacklab.test"+route.path, nil)
			_, pattern := handler.mux.Handler(request)
			if pattern != route.pattern {
				t.Fatalf("%s %s matched %q, want %q", route.method, route.path, pattern, route.pattern)
			}
		})
	}

	fallbacks := []struct {
		method  string
		path    string
		pattern string
	}{
		{http.MethodPatch, "/api/stacks", "/api/"},
		{http.MethodGet, "/api/not-implemented", "/api/"},
		{http.MethodGet, "/deep/frontend/route", "/"},
	}
	for _, fallback := range fallbacks {
		request := httptest.NewRequest(fallback.method, "http://stacklab.test"+fallback.path, nil)
		_, pattern := handler.mux.Handler(request)
		if pattern != fallback.pattern {
			t.Fatalf("%s %s matched %q, want fallback %q", fallback.method, fallback.path, pattern, fallback.pattern)
		}
	}
}

func TestOpenAPICoversDomainRouteContract(t *testing.T) {
	t.Parallel()

	rawSpec, err := os.ReadFile(filepath.Join("..", "..", "docs", "api", "openapi.yaml"))
	if err != nil {
		t.Fatalf("read OpenAPI spec: %v", err)
	}

	var spec struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(rawSpec, &spec); err != nil {
		t.Fatalf("parse OpenAPI spec: %v", err)
	}

	expected := make(map[string]struct{}, len(domainRouteContract())-1)
	for _, route := range domainRouteContract() {
		if route.pattern == "GET /api/ws" {
			continue
		}
		expected[route.pattern] = struct{}{}
	}

	documented := make(map[string]struct{}, len(expected))
	for path, pathItem := range spec.Paths {
		for method := range pathItem {
			if !isOpenAPIOperationMethod(method) {
				continue
			}
			pattern := strings.ToUpper(method) + " /api" + path
			documented[pattern] = struct{}{}
			if _, ok := expected[pattern]; !ok {
				t.Errorf("OpenAPI documents unregistered operation %s", pattern)
			}
		}
	}

	for pattern := range expected {
		if _, ok := documented[pattern]; !ok {
			t.Errorf("registered operation %s is missing from OpenAPI", pattern)
		}
	}
	if len(documented) != len(expected) {
		t.Fatalf("OpenAPI operation count = %d, want %d registered REST operations", len(documented), len(expected))
	}
}

func isOpenAPIOperationMethod(method string) bool {
	switch strings.ToUpper(method) {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodConnect, http.MethodOptions, http.MethodTrace:
		return true
	default:
		return false
	}
}

func TestDomainRoutesPreserveAuthenticationPolicy(t *testing.T) {
	_, served, _ := newInternalTestHandler(t)

	publicRoutes := map[string]int{
		"GET /api/live":        http.StatusOK,
		"GET /api/ready":       http.StatusOK,
		"GET /api/health":      http.StatusOK,
		"POST /api/auth/login": http.StatusBadRequest,
	}
	selfAuthenticatedRoutes := map[string]struct{}{
		"GET /api/session": {},
		"GET /api/ws":      {},
	}
	protectedCount := 0
	for _, route := range domainRouteContract() {
		request := httptest.NewRequest(route.method, "http://stacklab.test"+route.path, nil)
		response := httptest.NewRecorder()
		served.ServeHTTP(response, request)

		if wantStatus, public := publicRoutes[route.pattern]; public {
			if response.Code != wantStatus {
				t.Errorf("public route %s returned %d, want %d", route.pattern, response.Code, wantStatus)
			}
			continue
		}
		if _, selfAuthenticated := selfAuthenticatedRoutes[route.pattern]; selfAuthenticated {
			if response.Code != http.StatusUnauthorized {
				t.Errorf("self-authenticated route %s returned %d, want %d", route.pattern, response.Code, http.StatusUnauthorized)
			}
			continue
		}

		protectedCount++
		if response.Code != http.StatusUnauthorized {
			t.Errorf("protected route %s returned %d, want %d", route.pattern, response.Code, http.StatusUnauthorized)
		}
	}
	if protectedCount != 63 {
		t.Fatalf("protected route count = %d, want 63", protectedCount)
	}
}

func TestDomainRouteFallbackPolicy(t *testing.T) {
	_, served, _ := newInternalTestHandler(t)

	unknownAPI := httptest.NewRequest(http.MethodGet, "http://stacklab.test/api/not-implemented", nil)
	unknownAPIResponse := httptest.NewRecorder()
	served.ServeHTTP(unknownAPIResponse, unknownAPI)
	if unknownAPIResponse.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous API fallback status = %d, want %d", unknownAPIResponse.Code, http.StatusUnauthorized)
	}

	cookies := loginInternalTestUser(t, served, "test-password")
	authenticatedFallback := performInternalJSONRequest(t, served, http.MethodGet, "/api/not-implemented", nil, cookies)
	if authenticatedFallback.Code != http.StatusNotImplemented {
		t.Fatalf("authenticated API fallback status = %d, want %d", authenticatedFallback.Code, http.StatusNotImplemented)
	}

	frontend := httptest.NewRequest(http.MethodGet, "http://stacklab.test/deep/frontend/route", nil)
	frontendResponse := httptest.NewRecorder()
	served.ServeHTTP(frontendResponse, frontend)
	if frontendResponse.Code != http.StatusOK {
		t.Fatalf("frontend fallback status = %d, want %d", frontendResponse.Code, http.StatusOK)
	}
}
