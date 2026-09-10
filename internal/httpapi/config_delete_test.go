package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"stacklab/internal/configworkspace"
	"stacklab/internal/gitworkspace"
)

func TestConfigFileDeleteRequiresReviewAndRefreshesGitWithAudit(t *testing.T) {
	t.Parallel()
	_, served, cfg := newInternalTestHandler(t)
	cookies := loginInternalTestUser(t, served, "test-password")
	parent := filepath.Join(cfg.RootDir, "config", "demo")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(parent, "app.conf")
	if err := os.WriteFile(target, []byte("PORT=8080\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runInternalGit(t, cfg.RootDir, "init", "-b", "main")
	runInternalGit(t, cfg.RootDir, "config", "user.name", "Stacklab Test")
	runInternalGit(t, cfg.RootDir, "config", "user.email", "stacklab@example.invalid")
	runInternalGit(t, cfg.RootDir, "add", "config/demo/app.conf")
	runInternalGit(t, cfg.RootDir, "commit", "-m", "Track config")
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"path": "demo/app.conf", "expected_modified_at": info.ModTime()}
	for _, tc := range []struct {
		name    string
		body    any
		cookies []*http.Cookie
		status  int
		code    string
	}{
		{"unauthenticated", body, nil, http.StatusUnauthorized, "unauthorized"},
		{"missing timestamp", map[string]any{"path": "demo/app.conf"}, cookies, http.StatusBadRequest, "validation_failed"},
		{"stale", map[string]any{"path": "demo/app.conf", "expected_modified_at": info.ModTime().Add(-time.Second)}, cookies, http.StatusConflict, "edit_conflict"},
		{"directory", map[string]any{"path": "demo", "expected_modified_at": info.ModTime()}, cookies, http.StatusBadRequest, "path_not_file"},
		{"escape", map[string]any{"path": "../outside", "expected_modified_at": info.ModTime()}, cookies, http.StatusBadRequest, "path_outside_workspace"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := performInternalJSONRequest(t, served, http.MethodDelete, "/api/config/workspace/file", tc.body, tc.cookies)
			if response.Code != tc.status || !strings.Contains(response.Body.String(), `"code":"`+tc.code+`"`) {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if _, err := os.Stat(target); err != nil {
				t.Fatalf("rejected request changed the file: %v", err)
			}
		})
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodDelete, "/api/config/workspace/file", strings.NewReader(string(encoded)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://foreign.example")
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	served.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("cross-origin deletion status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	response := performInternalJSONRequest(t, served, http.MethodDelete, "/api/config/workspace/file", body, cookies)
	if response.Code != http.StatusOK {
		t.Fatalf("deletion status=%d body=%s", response.Code, response.Body.String())
	}
	var deleted configworkspace.DeleteFileResponse
	decodeInternalResponse(t, response, &deleted)
	if !deleted.Deleted || deleted.Path != "demo/app.conf" || deleted.AuditAction != "delete_config_file" {
		t.Fatalf("unexpected deletion: %#v", deleted)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file still exists: %v", err)
	}
	statusResponse := performInternalJSONRequest(t, served, http.MethodGet, "/api/git/workspace/status", nil, cookies)
	var status gitworkspace.StatusResponse
	decodeInternalResponse(t, statusResponse, &status)
	found := false
	for _, item := range status.Items {
		if item.Path == "config/demo/app.conf" && item.Status == "deleted" && item.CommitAllowed {
			found = true
		}
	}
	if !found {
		t.Fatalf("deletion missing from Git Changes: %#v", status)
	}
	auditResponse := performInternalJSONRequest(t, served, http.MethodGet, "/api/audit", nil, cookies)
	var auditPayload struct {
		Items []struct {
			Action  string  `json:"action"`
			StackID *string `json:"stack_id"`
		} `json:"items"`
	}
	decodeInternalResponse(t, auditResponse, &auditPayload)
	if len(auditPayload.Items) != 1 || auditPayload.Items[0].Action != "delete_config_file" || auditPayload.Items[0].StackID == nil || *auditPayload.Items[0].StackID != "demo" {
		t.Fatalf("unexpected audit: %#v", auditPayload.Items)
	}
	response = performInternalJSONRequest(t, served, http.MethodDelete, "/api/config/workspace/file", body, cookies)
	if response.Code != http.StatusNotFound {
		t.Fatalf("repeated deletion status=%d body=%s", response.Code, response.Body.String())
	}
}
