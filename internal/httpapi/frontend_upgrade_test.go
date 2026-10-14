package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFrontendUpgradeRevalidatesShellAndRejectsMissingAssets(t *testing.T) {
	h, _, _ := newInternalTestHandler(t)
	controller := &systemController{Handler: h}
	for _, path := range []string{"/", "/backups", "/index.html"} {
		response := httptest.NewRecorder()
		controller.handleFrontend(response, httptest.NewRequest(http.MethodGet, path, nil))
		wantStatus := http.StatusOK
		if path == "/index.html" {
			wantStatus = http.StatusMovedPermanently
		}
		if response.Code != wantStatus || response.Header().Get("Cache-Control") != "no-cache" {
			t.Fatalf("shell %s: status=%d cache=%q", path, response.Code, response.Header().Get("Cache-Control"))
		}
	}
	response := httptest.NewRecorder()
	controller.handleFrontend(response, httptest.NewRequest(http.MethodGet, "/assets/previous-release.js", nil))
	if response.Code != 404 || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("obsolete asset returned shell: %d %s", response.Code, response.Body.String())
	}
}
