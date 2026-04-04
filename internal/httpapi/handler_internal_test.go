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
	"path/filepath"
	"testing"
	"time"

	"stacklab/internal/audit"
	"stacklab/internal/auth"
	"stacklab/internal/config"
	"stacklab/internal/configworkspace"
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
	}
	handler.registerRoutes()

	return handler, handler.withLogging(handler.mux), cfg
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
