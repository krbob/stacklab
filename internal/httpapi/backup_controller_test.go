package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"stacklab/internal/backup"
)

func TestBackupHTTPConfigurationAndSecretBoundary(t *testing.T) {
	h, served, cfg := newInternalTestHandler(t)
	h.backupCapture = backup.NewNativeCapture(cfg, h.stackReader, h.jobs.Store().Snapshot)
	h.backups = backup.NewService(cfg, h.jobs, h.backupCapture)
	response := performInternalJSONRequest(t, served, http.MethodGet, "/api/backups", nil, nil)
	if response.Code != 401 {
		t.Fatalf("unauthenticated: %d", response.Code)
	}
	cookies := loginInternalTestUser(t, served, "test-password")
	response = performInternalJSONRequest(t, served, http.MethodPost, "/api/backups/destinations", map[string]any{
		"name": "S3 copy", "kind": "s3", "repository": "s3:https://storage.example.test/bucket", "secrets": map[string]string{"password": "private-password", "access_key_id": "private-key", "secret_access_key": "private-secret"},
	}, cookies)
	if response.Code != 200 {
		t.Fatalf("save: %d %s", response.Code, response.Body.String())
	}
	for _, secret := range []string{"private-password", "private-key", "private-secret"} {
		if strings.Contains(response.Body.String(), secret) {
			t.Fatal("save exposed credentials")
		}
	}
	var saved struct {
		Destination backup.Destination `json:"destination"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	response = performInternalJSONRequest(t, served, http.MethodGet, "/api/backups", nil, cookies)
	if response.Code != 200 || !strings.Contains(response.Body.String(), saved.Destination.ID) {
		t.Fatalf("overview: %d %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "private-") {
		t.Fatal("overview exposed credentials")
	}
	request := httptest.NewRequest(http.MethodDelete, "http://stacklab.test/api/backups/destinations/"+saved.Destination.ID, nil)
	request.Header.Set("Origin", "https://evil.example")
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	served.ServeHTTP(recorder, request)
	if recorder.Code != 403 {
		t.Fatalf("cross-origin mutation: %d", recorder.Code)
	}
	response = performInternalJSONRequest(t, served, http.MethodDelete, "/api/backups/destinations/"+saved.Destination.ID, nil, cookies)
	if response.Code != 200 {
		t.Fatalf("delete: %d %s", response.Code, response.Body.String())
	}
}

func TestBackupHTTPRejectsUnknownFieldsWithoutEchoingSecrets(t *testing.T) {
	h, served, cfg := newInternalTestHandler(t)
	h.backupCapture = backup.NewNativeCapture(cfg, h.stackReader, h.jobs.Store().Snapshot)
	h.backups = backup.NewService(cfg, h.jobs, h.backupCapture)
	cookies := loginInternalTestUser(t, served, "test-password")
	response := performInternalJSONRequest(t, served, http.MethodPost, "/api/backups/destinations", map[string]any{"unknown": "super-secret"}, cookies)
	if response.Code != 400 || strings.Contains(response.Body.String(), "super-secret") {
		t.Fatalf("invalid input: %d %s", response.Code, response.Body.String())
	}
}
