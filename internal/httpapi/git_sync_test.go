package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"stacklab/internal/gitworkspace"
)

func TestGitIncomingReviewDownloadAndAudit(t *testing.T) {
	t.Parallel()
	handler, served, cfg := newInternalTestHandler(t)
	cookies := loginInternalTestUser(t, served, "test-password")
	if err := os.MkdirAll(cfg.RootDir, 0755); err != nil {
		t.Fatal(err)
	}
	runInternalGit(t, cfg.RootDir, "init", "-b", "main")
	runInternalGit(t, cfg.RootDir, "config", "user.name", "Test")
	runInternalGit(t, cfg.RootDir, "config", "user.email", "test@example.invalid")
	runInternalGit(t, cfg.RootDir, "add", ".")
	runInternalGit(t, cfg.RootDir, "commit", "--allow-empty", "-m", "Initial")
	remote := filepath.Join(t.TempDir(), "remote.git")
	runInternalGit(t, cfg.RootDir, "init", "--bare", "--initial-branch=main", remote)
	runInternalGit(t, cfg.RootDir, "remote", "add", "origin", remote)
	runInternalGit(t, cfg.RootDir, "push", "-u", "origin", "main")
	laptop := filepath.Join(t.TempDir(), "laptop")
	runInternalGit(t, cfg.RootDir, "clone", remote, laptop)
	runInternalGit(t, laptop, "config", "user.name", "Test")
	runInternalGit(t, laptop, "config", "user.email", "test@example.invalid")
	if err := os.MkdirAll(filepath.Join(laptop, "config/demo"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(laptop, "config/demo/incoming.conf"), []byte("external change\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runInternalGit(t, laptop, "add", ".")
	runInternalGit(t, laptop, "commit", "-m", "External change")
	runInternalGit(t, laptop, "push")
	for _, endpoint := range []string{"incoming", "incoming/diff", "pull"} {
		response := performInternalJSONRequest(t, served, http.MethodPost, "/api/git/workspace/"+endpoint, map[string]any{}, nil)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("%s authentication: %d", endpoint, response.Code)
		}
		r := httptest.NewRequest(http.MethodPost, "/api/git/workspace/"+endpoint, strings.NewReader("{}"))
		r.Header.Set("Origin", "https://foreign.example")
		r.Header.Set("Content-Type", "application/json")
		for _, c := range cookies {
			r.AddCookie(c)
		}
		w := httptest.NewRecorder()
		served.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s origin: %d", endpoint, w.Code)
		}
	}
	response := performInternalJSONRequest(t, served, http.MethodPost, "/api/git/workspace/incoming", nil, cookies)
	if response.Code != http.StatusOK {
		t.Fatalf("incoming: %d %s", response.Code, response.Body.String())
	}
	var review gitworkspace.IncomingResponse
	decodeInternalResponse(t, response, &review)
	if !review.CanPull {
		t.Fatalf("not pullable: %+v", review)
	}
	request := gitworkspace.PullRequest{ExpectedState: review.ExpectedState}
	active, err := handler.jobs.Start(context.Background(), "demo", "up", "test")
	if err != nil {
		t.Fatal(err)
	}
	response = performInternalJSONRequest(t, served, http.MethodPost, "/api/git/workspace/pull", request, cookies)
	if response.Code != http.StatusConflict {
		t.Fatalf("active deploy not blocked: %d", response.Code)
	}
	if _, err = handler.jobs.FinishSucceeded(context.Background(), active); err != nil {
		t.Fatal(err)
	}
	// Block downloads during file edits, and reject new edits during downloads.
	handler.workspaceWriteMu.RLock()
	response = performInternalJSONRequest(t, served, http.MethodPost, "/api/git/workspace/pull", request, cookies)
	handler.workspaceWriteMu.RUnlock()
	if response.Code != http.StatusConflict {
		t.Fatal("download raced an editor")
	}
	handler.workspaceWriteMu.Lock()
	response = performInternalJSONRequest(t, served, http.MethodPut, "/api/config/workspace/file", map[string]any{}, cookies)
	handler.workspaceWriteMu.Unlock()
	if response.Code != http.StatusConflict {
		t.Fatal("editor raced a download")
	}
	response = performInternalJSONRequest(t, served, http.MethodPost, "/api/git/workspace/pull", gitworkspace.PullRequest{ExpectedState: strings.Repeat("0", 64)}, cookies)
	if response.Code != http.StatusConflict {
		t.Fatal("stale review accepted")
	}
	response = performInternalJSONRequest(t, served, http.MethodPost, "/api/git/workspace/pull", request, cookies)
	if response.Code != http.StatusOK {
		t.Fatalf("pull: %d %s", response.Code, response.Body.String())
	}
	if b, _ := os.ReadFile(filepath.Join(cfg.RootDir, "config/demo/incoming.conf")); string(b) != "external change\n" {
		t.Fatal("download missing")
	}
	jobs, err := handler.jobs.ListActive(context.Background())
	if err != nil || len(jobs.Items) != 0 {
		t.Fatalf("reservation leaked: %+v %v", jobs, err)
	}
	audit := performInternalJSONRequest(t, served, http.MethodGet, "/api/audit", nil, cookies)
	var entries struct {
		Items []struct {
			Action string `json:"action"`
			Result string `json:"result"`
		}
	}
	if err := json.Unmarshal(audit.Body.Bytes(), &entries); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries.Items {
		if e.Action == "git_pull" && e.Result == "succeeded" {
			found = true
		}
	}
	if !found {
		t.Fatal("successful download not audited")
	}
}
