package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"stacklab/internal/audit"
	"stacklab/internal/auth"
	"stacklab/internal/config"
	"stacklab/internal/configworkspace"
	"stacklab/internal/gitworkspace"
	"stacklab/internal/hostinfo"
	"stacklab/internal/jobs"
	"stacklab/internal/stacks"
	"stacklab/internal/store"
	"stacklab/internal/terminal"
)

type fakeHostInfo struct {
	overviewResponse hostinfo.OverviewResponse
	overviewError    error
	logsResponse     hostinfo.StacklabLogsResponse
	logsError        error
	lastLogsQuery    hostinfo.LogsQuery
}

func (f *fakeHostInfo) Overview(ctx context.Context) (hostinfo.OverviewResponse, error) {
	return f.overviewResponse, f.overviewError
}

func (f *fakeHostInfo) StacklabLogs(ctx context.Context, query hostinfo.LogsQuery) (hostinfo.StacklabLogsResponse, error) {
	f.lastLogsQuery = query
	return f.logsResponse, f.logsError
}

func TestHandlerPutDefinitionReturnsStackLockedWhenAnotherJobOwnsStack(t *testing.T) {
	t.Parallel()

	handler, served, _ := newInternalTestHandler(t)
	cookies := loginInternalTestUser(t, served, "secret")
	stackID := "fixture-save-locked"

	createResponse := performInternalJSONRequest(t, served, http.MethodPost, "/api/stacks", map[string]any{
		"stack_id":            stackID,
		"compose_yaml":        "services:\n  app:\n    image: nginx:alpine\n",
		"env":                 "",
		"create_config_dir":   false,
		"create_data_dir":     false,
		"deploy_after_create": false,
	}, cookies)
	if createResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/stacks status = %d, want %d", createResponse.Code, http.StatusOK)
	}

	lockingJob, err := handler.jobs.Start(context.Background(), stackID, "up", "local")
	if err != nil {
		t.Fatalf("jobs.Start() error = %v", err)
	}
	t.Cleanup(func() {
		_, _ = handler.jobs.FinishSucceeded(context.Background(), lockingJob)
	})

	putResponse := performInternalJSONRequest(t, served, http.MethodPut, "/api/stacks/"+stackID+"/definition", map[string]any{
		"compose_yaml":        "services:\n  app:\n    image: nginx:alpine\n",
		"env":                 "",
		"validate_after_save": true,
	}, cookies)
	if putResponse.Code != http.StatusConflict {
		t.Fatalf("PUT /api/stacks/%s/definition status = %d, want %d; body=%s", stackID, putResponse.Code, http.StatusConflict, putResponse.Body.String())
	}

	var errorPayload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	decodeInternalResponse(t, putResponse, &errorPayload)
	if errorPayload.Error.Code != "stack_locked" {
		t.Fatalf("unexpected error payload: %#v", errorPayload)
	}
}

func TestHandlerHostOverviewAndLogs(t *testing.T) {
	t.Parallel()

	handler, served, _ := newInternalTestHandler(t)
	host := &fakeHostInfo{
		overviewResponse: hostinfo.OverviewResponse{
			Host: hostinfo.HostMeta{
				Hostname:      "fixture-host",
				OSName:        "Fixture Linux",
				KernelVersion: "6.1.0",
				Architecture:  "linux-amd64",
				UptimeSeconds: 123,
			},
			Stacklab: hostinfo.StacklabMeta{
				Version:   "2026.04.0",
				Commit:    "abc1234",
				StartedAt: time.Unix(1_712_598_000, 0).UTC(),
			},
			Docker: hostinfo.DockerMeta{
				EngineVersion:  "28.5.1",
				ComposeVersion: "2.39.2",
			},
		},
		logsResponse: hostinfo.StacklabLogsResponse{
			Items: []hostinfo.StacklabLogEntry{
				{
					Timestamp: time.Unix(1_712_598_800, 0).UTC(),
					Level:     "info",
					Message:   "started",
					Cursor:    "s=cursor-1",
				},
			},
			NextCursor: "s=cursor-1",
			HasMore:    false,
		},
	}
	handler.hostInfo = host
	cookies := loginInternalTestUser(t, served, "secret")

	overviewResponse := performInternalJSONRequest(t, served, http.MethodGet, "/api/host/overview", nil, cookies)
	if overviewResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/host/overview status = %d, want %d; body=%s", overviewResponse.Code, http.StatusOK, overviewResponse.Body.String())
	}
	var overviewPayload hostinfo.OverviewResponse
	decodeInternalResponse(t, overviewResponse, &overviewPayload)
	if overviewPayload.Host.Hostname != "fixture-host" || overviewPayload.Stacklab.Version != "2026.04.0" {
		t.Fatalf("unexpected overview payload: %#v", overviewPayload)
	}

	logsResponse := performInternalJSONRequest(t, served, http.MethodGet, "/api/host/stacklab-logs?limit=25&level=error&q=bind&cursor=s%3Dprev", nil, cookies)
	if logsResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/host/stacklab-logs status = %d, want %d; body=%s", logsResponse.Code, http.StatusOK, logsResponse.Body.String())
	}
	if host.lastLogsQuery.Limit != 25 || host.lastLogsQuery.Level != "error" || host.lastLogsQuery.Search != "bind" || host.lastLogsQuery.Cursor != "s=prev" {
		t.Fatalf("unexpected logs query: %#v", host.lastLogsQuery)
	}
}

func TestHandlerStacklabLogsUnavailable(t *testing.T) {
	t.Parallel()

	handler, served, _ := newInternalTestHandler(t)
	handler.hostInfo = &fakeHostInfo{logsError: hostinfo.ErrLogsUnavailable}
	cookies := loginInternalTestUser(t, served, "secret")

	response := performInternalJSONRequest(t, served, http.MethodGet, "/api/host/stacklab-logs", nil, cookies)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET /api/host/stacklab-logs status = %d, want %d; body=%s", response.Code, http.StatusServiceUnavailable, response.Body.String())
	}
}

func TestHandlerStacklabLogsRejectsInvalidLimit(t *testing.T) {
	t.Parallel()

	_, served, _ := newInternalTestHandler(t)
	cookies := loginInternalTestUser(t, served, "secret")

	response := performInternalJSONRequest(t, served, http.MethodGet, "/api/host/stacklab-logs?limit=0", nil, cookies)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("GET /api/host/stacklab-logs?limit=0 status = %d, want %d; body=%s", response.Code, http.StatusBadRequest, response.Body.String())
	}
}

func TestHandlerConfigWorkspaceTreeFileAndSave(t *testing.T) {
	t.Parallel()

	handler, served, cfg := newInternalTestHandler(t)
	cookies := loginInternalTestUser(t, served, "secret")
	configRoot := filepath.Join(cfg.RootDir, "config")
	if err := os.MkdirAll(filepath.Join(configRoot, "nextcloud"), 0o755); err != nil {
		t.Fatalf("MkdirAll(config nextcloud) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(configRoot, "nextcloud", "app.conf"), []byte("PORT=8080\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(app.conf) error = %v", err)
	}

	treeResponse := performInternalJSONRequest(t, served, http.MethodGet, "/api/config/workspace/tree", nil, cookies)
	if treeResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/config/workspace/tree status = %d, want %d; body=%s", treeResponse.Code, http.StatusOK, treeResponse.Body.String())
	}
	var treePayload configworkspace.TreeResponse
	decodeInternalResponse(t, treeResponse, &treePayload)
	if len(treePayload.Items) != 1 || treePayload.Items[0].Name != "nextcloud" {
		t.Fatalf("unexpected config tree payload: %#v", treePayload)
	}

	fileResponse := performInternalJSONRequest(t, served, http.MethodGet, "/api/config/workspace/file?path=nextcloud%2Fapp.conf", nil, cookies)
	if fileResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/config/workspace/file status = %d, want %d; body=%s", fileResponse.Code, http.StatusOK, fileResponse.Body.String())
	}
	var filePayload configworkspace.FileResponse
	decodeInternalResponse(t, fileResponse, &filePayload)
	if filePayload.Content == nil || *filePayload.Content != "PORT=8080\n" || filePayload.Type != configworkspace.EntryTypeTextFile {
		t.Fatalf("unexpected config file payload: %#v", filePayload)
	}

	saveResponse := performInternalJSONRequest(t, served, http.MethodPut, "/api/config/workspace/file", map[string]any{
		"path":                      "nextcloud/app.conf",
		"content":                   "PORT=9090\n",
		"create_parent_directories": false,
	}, cookies)
	if saveResponse.Code != http.StatusOK {
		t.Fatalf("PUT /api/config/workspace/file status = %d, want %d; body=%s", saveResponse.Code, http.StatusOK, saveResponse.Body.String())
	}
	var savePayload configworkspace.SaveFileResponse
	decodeInternalResponse(t, saveResponse, &savePayload)
	if !savePayload.Saved || savePayload.AuditAction != "save_config_file" {
		t.Fatalf("unexpected save payload: %#v", savePayload)
	}

	updatedContent, err := os.ReadFile(filepath.Join(configRoot, "nextcloud", "app.conf"))
	if err != nil {
		t.Fatalf("ReadFile(updated app.conf) error = %v", err)
	}
	if string(updatedContent) != "PORT=9090\n" {
		t.Fatalf("saved app.conf = %q, want %q", string(updatedContent), "PORT=9090\n")
	}

	auditResponse := performInternalJSONRequest(t, served, http.MethodGet, "/api/audit", nil, cookies)
	if auditResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/audit status = %d, want %d; body=%s", auditResponse.Code, http.StatusOK, auditResponse.Body.String())
	}
	var auditPayload struct {
		Items []struct {
			Action  string  `json:"action"`
			StackID *string `json:"stack_id"`
		} `json:"items"`
	}
	decodeInternalResponse(t, auditResponse, &auditPayload)
	if len(auditPayload.Items) == 0 || auditPayload.Items[0].Action != "save_config_file" {
		t.Fatalf("unexpected audit entries after config save: %#v", auditPayload.Items)
	}
	if auditPayload.Items[0].StackID == nil || *auditPayload.Items[0].StackID != "nextcloud" {
		t.Fatalf("expected config save audit stack id, got %#v", auditPayload.Items[0].StackID)
	}

	_ = handler
}

func TestHandlerConfigWorkspaceErrors(t *testing.T) {
	t.Parallel()

	_, served, cfg := newInternalTestHandler(t)
	cookies := loginInternalTestUser(t, served, "secret")
	configRoot := filepath.Join(cfg.RootDir, "config")
	if err := os.MkdirAll(filepath.Join(configRoot, "nextcloud"), 0o755); err != nil {
		t.Fatalf("MkdirAll(config nextcloud) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(configRoot, "nextcloud", "blob.bin"), []byte{0x00, 0x01}, 0o644); err != nil {
		t.Fatalf("WriteFile(blob.bin) error = %v", err)
	}

	treeTraversal := performInternalJSONRequest(t, served, http.MethodGet, "/api/config/workspace/tree?path=..%2Fetc", nil, cookies)
	if treeTraversal.Code != http.StatusBadRequest {
		t.Fatalf("GET /api/config/workspace/tree traversal status = %d, want %d; body=%s", treeTraversal.Code, http.StatusBadRequest, treeTraversal.Body.String())
	}
	var treeError struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	decodeInternalResponse(t, treeTraversal, &treeError)
	if treeError.Error.Code != "path_outside_workspace" {
		t.Fatalf("unexpected tree traversal error payload: %#v", treeError)
	}

	fileDirectory := performInternalJSONRequest(t, served, http.MethodGet, "/api/config/workspace/file?path=nextcloud", nil, cookies)
	if fileDirectory.Code != http.StatusBadRequest {
		t.Fatalf("GET /api/config/workspace/file(directory) status = %d, want %d; body=%s", fileDirectory.Code, http.StatusBadRequest, fileDirectory.Body.String())
	}

	saveBinary := performInternalJSONRequest(t, served, http.MethodPut, "/api/config/workspace/file", map[string]any{
		"path":                      "nextcloud/blob.bin",
		"content":                   "text\n",
		"create_parent_directories": false,
	}, cookies)
	if saveBinary.Code != http.StatusConflict {
		t.Fatalf("PUT /api/config/workspace/file(binary) status = %d, want %d; body=%s", saveBinary.Code, http.StatusConflict, saveBinary.Body.String())
	}
	var saveBinaryError struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	decodeInternalResponse(t, saveBinary, &saveBinaryError)
	if saveBinaryError.Error.Code != "binary_not_editable" {
		t.Fatalf("unexpected binary save error payload: %#v", saveBinaryError)
	}
}

func TestHandlerConfigWorkspacePermissionDiagnostics(t *testing.T) {
	t.Parallel()

	if os.Geteuid() == 0 {
		t.Skip("permission diagnostics test requires non-root user")
	}

	_, served, cfg := newInternalTestHandler(t)
	cookies := loginInternalTestUser(t, served, "secret")
	configRoot := filepath.Join(cfg.RootDir, "config")
	protectedPath := filepath.Join(configRoot, "demo", "secret.conf")
	if err := os.MkdirAll(filepath.Join(configRoot, "demo"), 0o755); err != nil {
		t.Fatalf("MkdirAll(config demo) error = %v", err)
	}
	if err := os.WriteFile(protectedPath, []byte("token=secret\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(secret.conf) error = %v", err)
	}
	if err := os.Chmod(protectedPath, 0o000); err != nil {
		t.Fatalf("Chmod(secret.conf) error = %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(protectedPath, 0o644)
	})

	fileResponse := performInternalJSONRequest(t, served, http.MethodGet, "/api/config/workspace/file?path=demo%2Fsecret.conf", nil, cookies)
	if fileResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/config/workspace/file(secret.conf) status = %d, want %d; body=%s", fileResponse.Code, http.StatusOK, fileResponse.Body.String())
	}
	var filePayload configworkspace.FileResponse
	decodeInternalResponse(t, fileResponse, &filePayload)
	if filePayload.Readable || filePayload.BlockedReason == nil || *filePayload.BlockedReason != "not_readable" {
		t.Fatalf("unexpected protected config file payload: %#v", filePayload)
	}

	saveResponse := performInternalJSONRequest(t, served, http.MethodPut, "/api/config/workspace/file", map[string]any{
		"path":                      "demo/secret.conf",
		"content":                   "token=updated\n",
		"create_parent_directories": false,
	}, cookies)
	if saveResponse.Code != http.StatusConflict {
		t.Fatalf("PUT /api/config/workspace/file(secret.conf) status = %d, want %d; body=%s", saveResponse.Code, http.StatusConflict, saveResponse.Body.String())
	}
}

func TestHandlerGitWorkspaceStatusAndDiff(t *testing.T) {
	t.Parallel()

	_, served, cfg := newInternalTestHandler(t)
	cookies := loginInternalTestUser(t, served, "secret")
	if err := os.MkdirAll(filepath.Join(cfg.RootDir, "config", "demo"), 0o755); err != nil {
		t.Fatalf("MkdirAll(config demo) error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(cfg.RootDir, "stacks", "demo"), 0o755); err != nil {
		t.Fatalf("MkdirAll(stacks demo) error = %v", err)
	}

	runInternalGit(t, cfg.RootDir, "init", "-b", "main")
	runInternalGit(t, cfg.RootDir, "config", "user.name", "Stacklab Test")
	runInternalGit(t, cfg.RootDir, "config", "user.email", "stacklab@example.com")
	if err := os.WriteFile(filepath.Join(cfg.RootDir, "config", "demo", "app.conf"), []byte("server_name old.local;\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(config demo app.conf) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(cfg.RootDir, "stacks", "demo", "compose.yaml"), []byte("services:\n  app:\n    image: nginx:alpine\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(stacks demo compose.yaml) error = %v", err)
	}
	runInternalGit(t, cfg.RootDir, "add", ".")
	runInternalGit(t, cfg.RootDir, "commit", "-m", "initial")
	if err := os.WriteFile(filepath.Join(cfg.RootDir, "config", "demo", "app.conf"), []byte("server_name demo.local;\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(updated config demo app.conf) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(cfg.RootDir, "config", "demo", "new.env"), []byte("FEATURE_FLAG=true\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(config demo new.env) error = %v", err)
	}

	statusResponse := performInternalJSONRequest(t, served, http.MethodGet, "/api/git/workspace/status", nil, cookies)
	if statusResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/git/workspace/status status = %d, want %d; body=%s", statusResponse.Code, http.StatusOK, statusResponse.Body.String())
	}
	var statusPayload gitworkspace.StatusResponse
	decodeInternalResponse(t, statusResponse, &statusPayload)
	if !statusPayload.Available || statusPayload.Branch != "main" || statusPayload.Clean {
		t.Fatalf("unexpected git workspace status payload: %#v", statusPayload)
	}
	if len(statusPayload.Items) != 2 {
		t.Fatalf("unexpected git workspace items: %#v", statusPayload.Items)
	}

	diffResponse := performInternalJSONRequest(t, served, http.MethodGet, "/api/git/workspace/diff?path=config%2Fdemo%2Fapp.conf", nil, cookies)
	if diffResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/git/workspace/diff status = %d, want %d; body=%s", diffResponse.Code, http.StatusOK, diffResponse.Body.String())
	}
	var diffPayload gitworkspace.DiffResponse
	decodeInternalResponse(t, diffResponse, &diffPayload)
	if diffPayload.Diff == nil || !strings.Contains(*diffPayload.Diff, "+server_name demo.local;") {
		t.Fatalf("unexpected git diff payload: %#v", diffPayload)
	}
}

func TestHandlerGitWorkspaceUnavailableAndValidation(t *testing.T) {
	t.Parallel()

	_, served, _ := newInternalTestHandler(t)
	cookies := loginInternalTestUser(t, served, "secret")

	statusResponse := performInternalJSONRequest(t, served, http.MethodGet, "/api/git/workspace/status", nil, cookies)
	if statusResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/git/workspace/status(non-repo) status = %d, want %d; body=%s", statusResponse.Code, http.StatusOK, statusResponse.Body.String())
	}
	var statusPayload gitworkspace.StatusResponse
	decodeInternalResponse(t, statusResponse, &statusPayload)
	if statusPayload.Available {
		t.Fatalf("expected git workspace to be unavailable, got %#v", statusPayload)
	}

	diffUnavailable := performInternalJSONRequest(t, served, http.MethodGet, "/api/git/workspace/diff?path=config%2Fdemo%2Fapp.conf", nil, cookies)
	if diffUnavailable.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET /api/git/workspace/diff(non-repo) status = %d, want %d; body=%s", diffUnavailable.Code, http.StatusServiceUnavailable, diffUnavailable.Body.String())
	}

	diffTraversal := performInternalJSONRequest(t, served, http.MethodGet, "/api/git/workspace/diff?path=..%2Fetc%2Fpasswd", nil, cookies)
	if diffTraversal.Code != http.StatusBadRequest {
		t.Fatalf("GET /api/git/workspace/diff(traversal) status = %d, want %d; body=%s", diffTraversal.Code, http.StatusBadRequest, diffTraversal.Body.String())
	}
}

func TestHandlerGitWorkspacePermissionDiagnostics(t *testing.T) {
	t.Parallel()

	if os.Geteuid() == 0 {
		t.Skip("permission diagnostics test requires non-root user")
	}

	_, served, cfg := newInternalTestHandler(t)
	cookies := loginInternalTestUser(t, served, "secret")

	if err := os.MkdirAll(filepath.Join(cfg.RootDir, "config", "demo"), 0o755); err != nil {
		t.Fatalf("MkdirAll(config demo) error = %v", err)
	}
	runInternalGit(t, cfg.RootDir, "init", "-b", "main")
	runInternalGit(t, cfg.RootDir, "config", "user.name", "Stacklab Test")
	runInternalGit(t, cfg.RootDir, "config", "user.email", "stacklab@example.com")
	protectedPath := filepath.Join(cfg.RootDir, "config", "demo", "secret.conf")
	if err := os.WriteFile(protectedPath, []byte("token=old\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(secret.conf) error = %v", err)
	}
	runInternalGit(t, cfg.RootDir, "add", ".")
	runInternalGit(t, cfg.RootDir, "commit", "-m", "initial")
	if err := os.WriteFile(protectedPath, []byte("token=new\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(updated secret.conf) error = %v", err)
	}
	if err := os.Chmod(protectedPath, 0o000); err != nil {
		t.Fatalf("Chmod(secret.conf) error = %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(protectedPath, 0o644)
	})

	statusResponse := performInternalJSONRequest(t, served, http.MethodGet, "/api/git/workspace/status", nil, cookies)
	if statusResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/git/workspace/status(protected) status = %d, want %d; body=%s", statusResponse.Code, http.StatusOK, statusResponse.Body.String())
	}
	var statusPayload gitworkspace.StatusResponse
	decodeInternalResponse(t, statusResponse, &statusPayload)
	if len(statusPayload.Items) != 1 || statusPayload.Items[0].BlockedReason == nil || *statusPayload.Items[0].BlockedReason != "not_readable" {
		t.Fatalf("unexpected protected git status payload: %#v", statusPayload)
	}

	diffResponse := performInternalJSONRequest(t, served, http.MethodGet, "/api/git/workspace/diff?path=config%2Fdemo%2Fsecret.conf", nil, cookies)
	if diffResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/git/workspace/diff(protected) status = %d, want %d; body=%s", diffResponse.Code, http.StatusOK, diffResponse.Body.String())
	}
	var diffPayload gitworkspace.DiffResponse
	decodeInternalResponse(t, diffResponse, &diffPayload)
	if diffPayload.DiffAvailable || diffPayload.BlockedReason == nil || *diffPayload.BlockedReason != "not_readable" {
		t.Fatalf("unexpected protected git diff payload: %#v", diffPayload)
	}

	commitResponse := performInternalJSONRequest(t, served, http.MethodPost, "/api/git/workspace/commit", map[string]any{
		"message": "Update protected config",
		"paths":   []string{"config/demo/secret.conf"},
	}, cookies)
	if commitResponse.Code != http.StatusConflict {
		t.Fatalf("POST /api/git/workspace/commit(protected) status = %d, want %d; body=%s", commitResponse.Code, http.StatusConflict, commitResponse.Body.String())
	}
}

func TestHandlerGitWorkspaceCommitAndPush(t *testing.T) {
	t.Parallel()

	_, served, cfg := newInternalTestHandler(t)
	cookies := loginInternalTestUser(t, served, "secret")

	if err := os.MkdirAll(filepath.Join(cfg.RootDir, "config", "demo"), 0o755); err != nil {
		t.Fatalf("MkdirAll(config demo) error = %v", err)
	}

	runInternalGit(t, cfg.RootDir, "init", "-b", "main")
	runInternalGit(t, cfg.RootDir, "config", "user.name", "Stacklab Test")
	runInternalGit(t, cfg.RootDir, "config", "user.email", "stacklab@example.com")
	if err := os.WriteFile(filepath.Join(cfg.RootDir, "config", "demo", "app.conf"), []byte("server_name old.local;\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(config demo app.conf) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(cfg.RootDir, "config", "demo", "other.env"), []byte("FEATURE_FLAG=false\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(config demo other.env) error = %v", err)
	}
	runInternalGit(t, cfg.RootDir, "add", ".")
	runInternalGit(t, cfg.RootDir, "commit", "-m", "initial")

	remoteDir := filepath.Join(t.TempDir(), "origin.git")
	cmd := exec.CommandContext(context.Background(), "git", "init", "--bare", remoteDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git init --bare failed: %v\n%s", err, string(output))
	}
	runInternalGit(t, cfg.RootDir, "remote", "add", "origin", remoteDir)
	runInternalGit(t, cfg.RootDir, "push", "-u", "origin", "main")

	if err := os.WriteFile(filepath.Join(cfg.RootDir, "config", "demo", "app.conf"), []byte("server_name changed.local;\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(updated config demo app.conf) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(cfg.RootDir, "config", "demo", "other.env"), []byte("FEATURE_FLAG=true\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(updated config demo other.env) error = %v", err)
	}

	commitResponse := performInternalJSONRequest(t, served, http.MethodPost, "/api/git/workspace/commit", map[string]any{
		"message": "Update app config",
		"paths":   []string{"config/demo/app.conf"},
	}, cookies)
	if commitResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/git/workspace/commit status = %d, want %d; body=%s", commitResponse.Code, http.StatusOK, commitResponse.Body.String())
	}
	var commitPayload gitworkspace.CommitResponse
	decodeInternalResponse(t, commitResponse, &commitPayload)
	if !commitPayload.Committed || commitPayload.Commit == "" || commitPayload.RemainingChanges != 1 {
		t.Fatalf("unexpected git commit payload: %#v", commitPayload)
	}

	statusResponse := performInternalJSONRequest(t, served, http.MethodGet, "/api/git/workspace/status", nil, cookies)
	if statusResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/git/workspace/status(after commit) status = %d, want %d; body=%s", statusResponse.Code, http.StatusOK, statusResponse.Body.String())
	}
	var statusPayload gitworkspace.StatusResponse
	decodeInternalResponse(t, statusResponse, &statusPayload)
	if statusPayload.AheadCount != 1 || len(statusPayload.Items) != 1 || statusPayload.Items[0].Path != "config/demo/other.env" {
		t.Fatalf("unexpected git workspace status after commit: %#v", statusPayload)
	}

	pushResponse := performInternalJSONRequest(t, served, http.MethodPost, "/api/git/workspace/push", nil, cookies)
	if pushResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/git/workspace/push status = %d, want %d; body=%s", pushResponse.Code, http.StatusOK, pushResponse.Body.String())
	}
	var pushPayload gitworkspace.PushResponse
	decodeInternalResponse(t, pushResponse, &pushPayload)
	if !pushPayload.Pushed || pushPayload.Remote != "origin" || pushPayload.UpstreamName != "origin/main" || pushPayload.AheadCount != 0 {
		t.Fatalf("unexpected git push payload: %#v", pushPayload)
	}

	auditResponse := performInternalJSONRequest(t, served, http.MethodGet, "/api/audit", nil, cookies)
	if auditResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/audit(after git commit/push) status = %d, want %d; body=%s", auditResponse.Code, http.StatusOK, auditResponse.Body.String())
	}
	var auditPayload store.AuditListResult
	decodeInternalResponse(t, auditResponse, &auditPayload)
	foundCommit := false
	foundPush := false
	for _, item := range auditPayload.Items {
		if item.Action == "git_commit" {
			foundCommit = true
		}
		if item.Action == "git_push" {
			foundPush = true
		}
	}
	if !foundCommit || !foundPush {
		t.Fatalf("expected git audit actions, got %#v", auditPayload.Items)
	}
}

func TestHandlerMaintenanceUpdateStacksWorkflow(t *testing.T) {
	stacks.ResetComposeCLICacheForTests()
	t.Cleanup(stacks.ResetComposeCLICacheForTests)

	shimDir := t.TempDir()
	logPath := filepath.Join(shimDir, "docker.log")
	writeInternalDockerShim(t, filepath.Join(shimDir, "docker"))
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("STACKLAB_MAINTENANCE_LOG", logPath)

	_, served, cfg := newInternalTestHandler(t)
	cookies := loginInternalTestUser(t, served, "secret")

	stackRoot := filepath.Join(cfg.RootDir, "stacks", "demo")
	if err := os.MkdirAll(stackRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(stacks demo) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(stackRoot, "compose.yaml"), []byte(strings.Join([]string{
		"services:",
		"  app:",
		"    image: demo:latest",
		"    build: .",
		"",
	}, "\n")), 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yaml) error = %v", err)
	}

	response := performInternalJSONRequest(t, served, http.MethodPost, "/api/maintenance/update-stacks", map[string]any{
		"target": map[string]any{
			"mode":      "selected",
			"stack_ids": []string{"demo"},
		},
		"options": map[string]any{
			"pull_images":    true,
			"build_images":   true,
			"remove_orphans": true,
			"prune_after": map[string]any{
				"enabled":         true,
				"include_volumes": true,
			},
		},
	}, cookies)
	if response.Code != http.StatusOK {
		t.Fatalf("POST /api/maintenance/update-stacks status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}

	var payload struct {
		Job struct {
			StackID  *string `json:"stack_id"`
			Action   string  `json:"action"`
			State    string  `json:"state"`
			Workflow *struct {
				Steps []struct {
					Action        string `json:"action"`
					State         string `json:"state"`
					TargetStackID string `json:"target_stack_id"`
				} `json:"steps"`
			} `json:"workflow"`
		} `json:"job"`
	}
	decodeInternalResponse(t, response, &payload)
	if payload.Job.Action != "update_stacks" || payload.Job.State != "succeeded" {
		t.Fatalf("unexpected maintenance job payload: %#v", payload.Job)
	}
	if payload.Job.StackID != nil {
		t.Fatalf("expected maintenance job stack_id to be null, got %#v", payload.Job.StackID)
	}
	if payload.Job.Workflow == nil || len(payload.Job.Workflow.Steps) != 4 {
		t.Fatalf("unexpected maintenance workflow: %#v", payload.Job.Workflow)
	}
	if payload.Job.Workflow.Steps[0].Action != "pull" || payload.Job.Workflow.Steps[0].TargetStackID != "demo" {
		t.Fatalf("unexpected first maintenance step: %#v", payload.Job.Workflow.Steps[0])
	}
	if payload.Job.Workflow.Steps[3].Action != "prune" || payload.Job.Workflow.Steps[3].TargetStackID != "" {
		t.Fatalf("unexpected prune maintenance step: %#v", payload.Job.Workflow.Steps[3])
	}

	auditResponse := performInternalJSONRequest(t, served, http.MethodGet, "/api/audit", nil, cookies)
	if auditResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/audit status = %d, want %d; body=%s", auditResponse.Code, http.StatusOK, auditResponse.Body.String())
	}
	var auditPayload struct {
		Items []struct {
			Action  string  `json:"action"`
			StackID *string `json:"stack_id"`
		} `json:"items"`
	}
	decodeInternalResponse(t, auditResponse, &auditPayload)
	if len(auditPayload.Items) == 0 || auditPayload.Items[0].Action != "update_stacks" {
		t.Fatalf("unexpected audit payload after maintenance: %#v", auditPayload.Items)
	}
	if auditPayload.Items[0].StackID != nil {
		t.Fatalf("expected maintenance audit stack_id to be null, got %#v", auditPayload.Items[0].StackID)
	}

	recorded, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile(docker log) error = %v", err)
	}
	recordedText := string(recorded)
	for _, expected := range []string{
		"compose pull",
		"compose build",
		"compose up -d --remove-orphans",
		"docker system prune -af --volumes",
	} {
		if !strings.Contains(recordedText, expected) {
			t.Fatalf("expected docker log to contain %q, got %q", expected, recordedText)
		}
	}
}

func newInternalTestHandler(t *testing.T) (*Handler, http.Handler, config.Config) {
	t.Helper()

	tempDir := t.TempDir()
	cfg := config.Config{
		RootDir:                 filepath.Join(tempDir, "root"),
		DataDir:                 filepath.Join(tempDir, "var"),
		DatabasePath:            filepath.Join(tempDir, "var", "stacklab.db"),
		FrontendDistDir:         filepath.Join(tempDir, "frontend"),
		BootstrapPassword:       "secret",
		SystemdUnitName:         "stacklab",
		SessionCookieName:       "stacklab_session",
		SessionIdleTimeout:      30 * time.Minute,
		SessionAbsoluteLifetime: 24 * time.Hour,
	}
	if err := os.MkdirAll(cfg.FrontendDistDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(frontend dist) error = %v", err)
	}

	testStore, err := store.Open(cfg.DatabasePath)
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := testStore.Close(); err != nil {
			t.Fatalf("Store.Close() error = %v", err)
		}
	})

	authService := auth.NewService(cfg, testStore)
	if err := authService.Bootstrap(context.Background()); err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	auditService := audit.NewService(testStore)
	jobService := jobs.NewService(testStore)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	handler := &Handler{
		cfg:    cfg,
		logger: logger,
		mux:    http.NewServeMux(),
		auth:   authService,
		audit:  auditService,
		jobs:   jobService,
		terminals: terminal.NewService(logger, terminal.Config{
			MaxSessionsPerOwner: 5,
			IdleTimeout:         30 * time.Minute,
			DetachGracePeriod:   time.Minute,
		}, func(event terminal.LifecycleEvent) {}),
		stackReader: stacks.NewServiceReader(cfg, logger),
		hostInfo:    hostinfo.NewService(cfg, time.Unix(1_712_598_000, 0).UTC()),
		configFiles: configworkspace.NewService(cfg),
		gitStatus:   gitworkspace.NewService(cfg),
	}
	handler.registerRoutes()

	return handler, handler.withLogging(handler.mux), cfg
}

func runInternalGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(cmd.Environ(), "GIT_PAGER=cat", "TERM=dumb")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, string(output))
	}
}

func writeInternalDockerShim(t *testing.T, path string) {
	t.Helper()

	script := `#!/bin/sh
set -eu

log_file="${STACKLAB_MAINTENANCE_LOG:-}"

append_log() {
  if [ -n "$log_file" ]; then
    printf '%s\n' "$1" >> "$log_file"
  fi
}

if [ "$1" = "ps" ]; then
  exit 0
fi

if [ "$1" = "inspect" ]; then
  echo '[]'
  exit 0
fi

if [ "$1" = "version" ]; then
  echo "28.5.1"
  exit 0
fi

if [ "$1" = "system" ] && [ "$2" = "prune" ]; then
  shift 2
  append_log "docker system prune $*"
  echo "Deleted Objects:"
  exit 0
fi

if [ "$1" = "compose" ]; then
  shift
  if [ "$1" = "version" ]; then
    echo "2.39.2"
    exit 0
  fi

  sub=""
  args=""
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --project-directory|-f|--env-file)
        shift 2
        ;;
      pull|build|up|down|restart|stop)
        sub="$1"
        shift
        args="$*"
        break
        ;;
      *)
        shift
        ;;
    esac
  done

  append_log "compose $sub $args"
  case "$sub" in
    pull)
      echo "Pulled images"
      ;;
    build)
      echo "Built images"
      ;;
    up)
      echo "Started services"
      ;;
    *)
      echo "OK"
      ;;
  esac
  exit 0
fi

echo "unsupported docker invocation: $*" >&2
exit 1
`

	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(docker shim) error = %v", err)
	}
}

func loginInternalTestUser(t *testing.T, served http.Handler, password string) []*http.Cookie {
	t.Helper()

	response := performInternalJSONRequest(t, served, http.MethodPost, "/api/auth/login", map[string]any{
		"password": password,
	}, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login status = %d, want %d", response.Code, http.StatusOK)
	}

	return response.Result().Cookies()
}

func performInternalJSONRequest(t *testing.T, served http.Handler, method, path string, body any, cookies []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()

	var requestBody io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("json.Marshal() error = %v", err)
		}
		requestBody = bytes.NewReader(payload)
	}

	request := httptest.NewRequest(method, "http://stacklab.test"+path, requestBody)
	request.Header.Set("Origin", "http://stacklab.test")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}

	recorder := httptest.NewRecorder()
	served.ServeHTTP(recorder, request)
	return recorder
}

func decodeInternalResponse(t *testing.T, recorder *httptest.ResponseRecorder, destination any) {
	t.Helper()

	if err := json.NewDecoder(recorder.Body).Decode(destination); err != nil {
		t.Fatalf("json.Decode() error = %v; body = %s", err, recorder.Body.String())
	}
}
