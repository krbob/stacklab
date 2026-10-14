package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"stacklab/internal/backup"
)

func TestBackupPolicyAndRecoveryDiagnosticsAccess(t *testing.T) {
	h, served, cfg := newInternalTestHandler(t)
	h.backupCapture = backup.NewNativeCapture(cfg, h.stackReader, h.jobs.Store().Snapshot)
	h.backups = backup.NewService(cfg, h.jobs, h.backupCapture)
	cookies := loginInternalTestUser(t, served, "test-password")
	for _, route := range []string{"/api/backups/policy", "/api/backups/recovery", "/api/backups/restores/restore_unknown/archive?source_id=x"} {
		if response := performInternalJSONRequest(t, served, http.MethodGet, route, nil, nil); response.Code != 401 {
			t.Fatalf("unauthenticated %s: %d", route, response.Code)
		}
	}
	input := map[string]any{"paused": true, "overdue_grace_minutes": 90, "heartbeat_url": "https://monitor.example/push/private-heartbeat-token"}
	response := performInternalJSONRequest(t, served, http.MethodPut, "/api/backups/policy", input, cookies)
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"heartbeat_configured":true`) || strings.Contains(response.Body.String(), "private-heartbeat-token") {
		t.Fatalf("policy %d %s", response.Code, response.Body.String())
	}
	response = performInternalJSONRequest(t, served, http.MethodGet, "/api/backups/policy", nil, cookies)
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"paused":true`) {
		t.Fatalf("policy %d %s", response.Code, response.Body.String())
	}
	request := httptest.NewRequest(http.MethodPut, "http://stacklab.test/api/backups/policy", strings.NewReader(`{"paused":false,"overdue_grace_minutes":60}`))
	request.Header.Set("Origin", "https://outside.example")
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	response = httptest.NewRecorder()
	served.ServeHTTP(response, request)
	if response.Code != 403 {
		t.Fatalf("cross-origin policy accepted: %d", response.Code)
	}
	directory := filepath.Join(cfg.DataDir, "backup", "journals")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "broken.json"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	response = performInternalJSONRequest(t, served, http.MethodGet, "/api/backups/recovery", nil, cookies)
	if response.Code != 200 || !strings.Contains(response.Body.String(), "invalid journal") {
		t.Fatalf("journal diagnostics %d %s", response.Code, response.Body.String())
	}
	response = performInternalJSONRequest(t, served, http.MethodPost, "/api/backups/recovery/test", map[string]any{"action": "delete_everything"}, cookies)
	if response.Code != 400 {
		t.Fatalf("unsupported recovery accepted: %d", response.Code)
	}
}
