package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"stacklab/internal/audit"
	"stacklab/internal/gitworkspace"
)

func TestGitRestoreRequiresReviewedStateAndRecordsAudit(t *testing.T) {
	t.Parallel()
	handler, served, cfg := newInternalTestHandler(t)
	cookies := loginInternalTestUser(t, served, "test-password")
	path := "config/demo/app.conf"
	target := filepath.Join(cfg.RootDir, path)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runInternalGit(t, cfg.RootDir, "init", "-b", "main")
	runInternalGit(t, cfg.RootDir, "config", "user.name", "Stacklab Test")
	runInternalGit(t, cfg.RootDir, "config", "user.email", "stacklab@example.invalid")
	runInternalGit(t, cfg.RootDir, "add", path)
	runInternalGit(t, cfg.RootDir, "commit", "-m", "Initial workspace")
	if err := os.WriteFile(target, []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	diffResponse := performInternalJSONRequest(t, served, http.MethodGet, "/api/git/workspace/diff?path="+path, nil, cookies)
	var diff gitworkspace.DiffResponse
	decodeInternalResponse(t, diffResponse, &diff)
	if !diff.RestoreAllowed || diff.RestoreState == "" {
		t.Fatalf("missing restore review: %#v", diff)
	}
	body := gitworkspace.RestoreFileRequest{Path: path, ExpectedState: diff.RestoreState}
	for _, tc := range []struct {
		name    string
		body    any
		cookies []*http.Cookie
		status  int
		code    string
	}{
		{"unauthenticated", body, nil, http.StatusUnauthorized, "unauthorized"},
		{"missing review", map[string]any{"path": path}, cookies, http.StatusBadRequest, "validation_failed"},
		{"stale", gitworkspace.RestoreFileRequest{Path: path, ExpectedState: strings.Repeat("0", 64)}, cookies, http.StatusConflict, "edit_conflict"},
		{"escape", gitworkspace.RestoreFileRequest{Path: "../outside", ExpectedState: diff.RestoreState}, cookies, http.StatusBadRequest, "path_outside_workspace"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := performInternalJSONRequest(t, served, http.MethodPost, "/api/git/workspace/restore", tc.body, tc.cookies)
			if response.Code != tc.status || !strings.Contains(response.Body.String(), `"code":"`+tc.code+`"`) {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if data, _ := os.ReadFile(target); string(data) != "changed\n" {
				t.Fatal("rejected request changed the file")
			}
		})
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/git/workspace/restore", strings.NewReader(string(encoded)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://foreign.example")
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	served.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("cross-origin restore status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	response := performInternalJSONRequest(t, served, http.MethodPost, "/api/git/workspace/restore", body, cookies)
	if response.Code != http.StatusOK {
		t.Fatalf("restore status=%d body=%s", response.Code, response.Body.String())
	}
	var restored gitworkspace.RestoreFileResponse
	decodeInternalResponse(t, response, &restored)
	if !restored.Restored || restored.Path != path || restored.HeadCommit != diff.RestoreCommit || restored.AuditAction != "git_restore_file" {
		t.Fatalf("unexpected restore: %#v", restored)
	}
	if data, _ := os.ReadFile(target); string(data) != "original\n" {
		t.Fatal("original file not restored")
	}
	statusResponse := performInternalJSONRequest(t, served, http.MethodGet, "/api/git/workspace/status", nil, cookies)
	var status gitworkspace.StatusResponse
	decodeInternalResponse(t, statusResponse, &status)
	if !status.Clean {
		t.Fatalf("restored file remains in Changes: %#v", status)
	}
	auditResponse := performInternalJSONRequest(t, served, http.MethodGet, "/api/audit", nil, cookies)
	var auditPayload struct {
		Items []struct {
			Action  string  `json:"action"`
			StackID *string `json:"stack_id"`
		} `json:"items"`
	}
	decodeInternalResponse(t, auditResponse, &auditPayload)
	if len(auditPayload.Items) != 1 || auditPayload.Items[0].Action != "git_restore_file" || auditPayload.Items[0].StackID == nil || *auditPayload.Items[0].StackID != "demo" {
		t.Fatalf("unexpected audit: %#v", auditPayload.Items)
	}
	entries, err := handler.audit.List(t.Context(), audit.ListQuery{Limit: 10})
	if err != nil || len(entries.Items) != 1 {
		t.Fatalf("audit entries: %#v, %v", entries, err)
	}
	entry := entries.Items[0]
	if entry.TargetID == nil || *entry.TargetID != path || entry.DetailJSON == nil || !strings.Contains(*entry.DetailJSON, diff.RestoreCommit) {
		t.Fatalf("missing audit target or source commit: %#v", entry)
	}
	response = performInternalJSONRequest(t, served, http.MethodPost, "/api/git/workspace/restore", body, cookies)
	if response.Code != http.StatusNotFound {
		t.Fatalf("repeated restore status=%d body=%s", response.Code, response.Body.String())
	}
}
