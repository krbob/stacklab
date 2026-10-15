package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestBackupHTTPRejectsInvalidOperationBeforeJobAdmission(t *testing.T) {
	h, served, cfg := newInternalTestHandler(t)
	h.backupCapture = backup.NewNativeCapture(cfg, h.stackReader, h.jobs.Store().Snapshot)
	h.backups = backup.NewService(cfg, h.jobs, h.backupCapture)
	cookies := loginInternalTestUser(t, served, "test-password")
	for _, test := range []struct {
		action string
		body   any
	}{
		{"check", map[string]any{"read_data": "invalid"}},
		{"unlock", map[string]any{"remove_all": true}},
		{"prune", map[string]any{"acknowledge_integrity_failure": "true"}},
		{"restore", map[string]any{"snapshot_id": "invalid"}},
		{"restore", map[string]any{"snapshot_id": strings.Repeat("a", 64), "test": true, "source_ids": []string{"source"}}},
		{"restore", map[string]any{"snapshot_id": strings.Repeat("a", 64), "files": []map[string]string{{"source_id": "source", "path": "../outside"}}}},
	} {
		response := performInternalJSONRequest(t, served, http.MethodPost, "/api/backups/destinations/not-admitted/"+test.action, test.body, cookies)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s admitted invalid operation: %d %s", test.action, response.Code, response.Body.String())
		}
	}
}

func TestBackupHTTPPruneIntegrityAcknowledgementAndUnlockReservation(t *testing.T) {
	h, served, cfg := newInternalTestHandler(t)
	h.backupCapture = backup.NewNativeCapture(cfg, h.stackReader, h.jobs.Store().Snapshot)
	h.backups = backup.NewService(cfg, h.jobs, h.backupCapture)
	parent := t.TempDir()
	identity, err := backup.FilesystemIdentity(parent)
	if err != nil {
		t.Fatal(err)
	}
	destination, err := h.backups.SaveDestination(context.Background(), "", backup.DestinationInput{Name: "Recovery controls", Kind: "local", Repository: filepath.Join(parent, "repository"), ExpectedFilesystem: identity, Secrets: &backup.Secrets{Password: "fixture-password"}})
	if err != nil {
		t.Fatal(err)
	}
	// Load the same durable evidence an installed service reads after restart.
	raw, _, err := h.jobs.Store().AppSetting(context.Background(), "backup.v1")
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &saved); err != nil {
		t.Fatal(err)
	}
	destination.CheckStatus, destination.CheckError = "failed", "corrupt pack (integrity_failed)"
	destination.DataCheck = backup.CheckEvidence{Status: "failed", Scope: "all", Error: destination.CheckError}
	saved["destinations"], err = json.Marshal(map[string]backup.Destination{destination.ID: destination})
	if err != nil {
		t.Fatal(err)
	}
	state, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.jobs.Store().SetAppSetting(context.Background(), "backup.v1", string(state), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	h.backups = backup.NewService(cfg, h.jobs, h.backupCapture)
	cookies := loginInternalTestUser(t, served, "test-password")
	base := "/api/backups/destinations/" + destination.ID
	for _, body := range []any{nil, map[string]any{}, map[string]any{"acknowledge_integrity_failure": false}, map[string]any{"acknowledge_integrity_failure": true}, map[string]any{"acknowledge_integrity_failure": true, "acknowledgement_review_token": "stale"}} {
		response := performInternalJSONRequest(t, served, http.MethodPost, base+"/prune", body, cookies)
		if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "maintenance_acknowledgement_required") {
			t.Fatalf("unacknowledged prune admitted: %d %s", response.Code, response.Body.String())
		}
	}
	reserved, err := h.backups.StartRepositoryJob(context.Background(), destination.ID, "backup_check", "test")
	if err != nil {
		t.Fatal(err)
	}
	response := performInternalJSONRequest(t, served, http.MethodPost, base+"/unlock", map[string]any{}, cookies)
	if response.Code != http.StatusConflict {
		t.Fatalf("unlock ignored repository reservation: %d %s", response.Code, response.Body.String())
	}
	// Execution releases the reservation even when this uninitialized fixture's
	// check fails. No valid repository or installed restic is needed by this test.
	_, _ = h.backups.ExecuteRepositoryJob(context.Background(), reserved, destination.ID, "backup_check", backup.CheckRequest{}, backup.RestoreRequest{})
	overview, err := h.backups.Overview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	token := ""
	for _, current := range overview.Destinations {
		if current.ID == destination.ID {
			token = current.MaintenanceReviewToken
		}
	}
	if token == "" {
		t.Fatal("missing current maintenance review token")
	}
	response = performInternalJSONRequest(t, served, http.MethodPost, base+"/prune", map[string]any{"acknowledge_integrity_failure": true, "acknowledgement_review_token": token}, cookies)
	if response.Code != http.StatusAccepted || !strings.Contains(response.Body.String(), "backup_prune") {
		t.Fatalf("acknowledged prune rejected: %d %s", response.Code, response.Body.String())
	}
}

func TestBackupHTTPHealthyMaintenanceAcceptsEmptyRequestBody(t *testing.T) {
	for _, action := range []string{"prune", "unlock"} {
		t.Run(action, func(t *testing.T) {
			h, served, cfg := newInternalTestHandler(t)
			h.backupCapture = backup.NewNativeCapture(cfg, h.stackReader, h.jobs.Store().Snapshot)
			h.backups = backup.NewService(cfg, h.jobs, h.backupCapture)
			parent := t.TempDir()
			identity, err := backup.FilesystemIdentity(parent)
			if err != nil {
				t.Fatal(err)
			}
			d, err := h.backups.SaveDestination(context.Background(), "", backup.DestinationInput{Name: "Legacy request", Kind: "local", Repository: filepath.Join(parent, "repository"), ExpectedFilesystem: identity, Secrets: &backup.Secrets{Password: "fixture-password"}})
			if err != nil {
				t.Fatal(err)
			}
			cookies := loginInternalTestUser(t, served, "test-password")
			request := httptest.NewRequest(http.MethodPost, "http://stacklab.test/api/backups/destinations/"+d.ID+"/"+action, nil)
			for _, cookie := range cookies {
				request.AddCookie(cookie)
			}
			response := httptest.NewRecorder()
			served.ServeHTTP(response, request)
			if response.Code != http.StatusAccepted || !strings.Contains(response.Body.String(), "backup_"+action) {
				t.Fatalf("empty maintenance request rejected: %d %s", response.Code, response.Body.String())
			}
		})
	}
}
