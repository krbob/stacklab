package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"stacklab/internal/gitworkspace"
)

func TestGitReconcileAPIWithNativeRepository(t *testing.T) {
	t.Parallel()
	handler, served, cfg := newInternalTestHandler(t)
	cookies := loginInternalTestUser(t, served, "test-password")
	for _, suffix := range []string{"prepare", "file", "resolve", "discard", "apply"} {
		path := "/api/git/workspace/reconcile/" + suffix
		if r := performInternalJSONRequest(t, served, http.MethodPost, path, map[string]any{}, nil); r.Code != http.StatusUnauthorized {
			t.Fatalf("%s auth=%d", suffix, r.Code)
		}
		request := httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}"))
		request.Header.Set("Origin", "https://evil.invalid")
		for _, cookie := range cookies {
			request.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		served.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Fatalf("%s origin=%d", suffix, response.Code)
		}
	}
	if err := os.MkdirAll(filepath.Join(cfg.RootDir, "config/demo"), 0755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(cfg.RootDir, "config/demo/app.conf")
	if err := os.WriteFile(file, []byte("base\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runInternalGit(t, cfg.RootDir, "init", "-b", "main")
	runInternalGit(t, cfg.RootDir, "config", "user.name", "Test")
	runInternalGit(t, cfg.RootDir, "config", "user.email", "test@example.invalid")
	runInternalGit(t, cfg.RootDir, "add", ".")
	runInternalGit(t, cfg.RootDir, "commit", "-m", "Initial")
	remote := filepath.Join(t.TempDir(), "remote.git")
	runInternalGit(t, cfg.RootDir, "init", "--bare", "--initial-branch=main", remote)
	runInternalGit(t, cfg.RootDir, "remote", "add", "origin", remote)
	runInternalGit(t, cfg.RootDir, "push", "-u", "origin", "main")
	laptop := filepath.Join(t.TempDir(), "laptop")
	runInternalGit(t, cfg.RootDir, "clone", remote, laptop)
	runInternalGit(t, laptop, "config", "user.name", "Test")
	runInternalGit(t, laptop, "config", "user.email", "test@example.invalid")
	if err := os.WriteFile(file, []byte("local\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runInternalGit(t, cfg.RootDir, "add", ".")
	runInternalGit(t, cfg.RootDir, "commit", "-m", "Local")
	if err := os.WriteFile(filepath.Join(laptop, "config/demo/app.conf"), []byte("remote\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runInternalGit(t, laptop, "add", ".")
	runInternalGit(t, laptop, "commit", "-m", "Remote")
	runInternalGit(t, laptop, "push")
	call := func(method, path string, body any) *httptest.ResponseRecorder {
		t.Helper()
		return performInternalJSONRequest(t, served, method, "/api/git/workspace/"+path, body, cookies)
	}
	var incoming gitworkspace.IncomingResponse
	decodeInternalResponse(t, call(http.MethodPost, "incoming", nil), &incoming)
	var draft gitworkspace.ReconcileResponse
	response := call(http.MethodPost, "reconcile/prepare", gitworkspace.PullRequest{ExpectedState: incoming.ExpectedState})
	if response.Code != http.StatusOK {
		t.Fatalf("prepare=%d %s", response.Code, response.Body.String())
	}
	decodeInternalResponse(t, response, &draft)
	r := gitworkspace.ReconcileRequest{ID: draft.ID, Revision: draft.Revision}
	fileRequest := gitworkspace.ReconcileFileRequest{ReconcileRequest: r, Path: "config/demo/app.conf"}
	response = call(http.MethodPost, "reconcile/file", fileRequest)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "remote\\n") {
		t.Fatalf("file=%s", response.Body.String())
	}
	response = call(http.MethodPost, "reconcile/resolve", gitworkspace.ResolveRequest{ReconcileFileRequest: fileRequest, Choice: "edit", Content: "merged\n"})
	if response.Code != http.StatusOK {
		t.Fatalf("resolve=%d %s", response.Code, response.Body.String())
	}
	decodeInternalResponse(t, response, &draft)
	r.Revision = draft.Revision
	response = call(http.MethodGet, "reconcile", nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"ready":true`) {
		t.Fatal("draft not resumable")
	}
	active, err := handler.jobs.Start(context.Background(), "demo", "up", "test")
	if err != nil {
		t.Fatal(err)
	}
	if response = call(http.MethodPost, "reconcile/apply", r); response.Code != http.StatusConflict {
		t.Fatal("applied during deployment")
	}
	if _, err = handler.jobs.FinishSucceeded(context.Background(), active); err != nil {
		t.Fatal(err)
	}
	handler.workspaceWriteMu.RLock()
	response = call(http.MethodPost, "reconcile/apply", r)
	handler.workspaceWriteMu.RUnlock()
	if response.Code != http.StatusConflict {
		t.Fatal("applied during file edit")
	}
	response = call(http.MethodPost, "reconcile/apply", r)
	if response.Code != http.StatusOK {
		t.Fatalf("apply=%d %s", response.Code, response.Body.String())
	}
	got, _ := os.ReadFile(file)
	if string(got) != "merged\n" {
		t.Fatalf("applied=%q", got)
	}
	audit := performInternalJSONRequest(t, served, http.MethodGet, "/api/audit", nil, cookies)
	if !strings.Contains(audit.Body.String(), `"action":"git_reconcile"`) || !strings.Contains(audit.Body.String(), `"result":"succeeded"`) {
		t.Fatalf("audit=%s", audit.Body.String())
	}
	activeJobs, err := handler.jobs.ListActive(context.Background())
	if err != nil || len(activeJobs.Items) != 0 {
		t.Fatal("job reservation leaked")
	}
	if response = call(http.MethodPost, "reconcile/discard", r); response.Code != http.StatusOK {
		t.Fatalf("discard=%s", response.Body.String())
	}
	if response = call(http.MethodGet, "reconcile", nil); response.Body.String() != "{\"draft\":null}\n" {
		t.Fatalf("remaining draft=%s", response.Body.String())
	}
}
