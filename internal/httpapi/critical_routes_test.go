package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"stacklab/internal/config"
	"stacklab/internal/store"
)

func TestHandlerStackWorkspaceTreeFileAndSave(t *testing.T) {
	t.Parallel()

	_, served, cfg := newInternalTestHandler(t)
	stackRoot := filepath.Join(cfg.RootDir, "stacks", "demo")
	if err := os.MkdirAll(filepath.Join(stackRoot, "nested"), 0o755); err != nil {
		t.Fatalf("MkdirAll(stack workspace) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(stackRoot, "compose.yaml"), []byte("services: {}\n"), 0o640); err != nil {
		t.Fatalf("WriteFile(compose.yaml) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(stackRoot, "notes.txt"), []byte("before\n"), 0o640); err != nil {
		t.Fatalf("WriteFile(notes.txt) error = %v", err)
	}
	cookies := loginInternalTestUser(t, served, "test-password")

	treeResponse := performInternalJSONRequest(t, served, http.MethodGet, "/api/stacks/demo/workspace/tree", nil, cookies)
	if treeResponse.Code != http.StatusOK {
		t.Fatalf("GET stack workspace tree status = %d; body=%s", treeResponse.Code, treeResponse.Body.String())
	}
	var treePayload struct {
		StackID string `json:"stack_id"`
		Items   []struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"items"`
	}
	decodeInternalResponse(t, treeResponse, &treePayload)
	if treePayload.StackID != "demo" || len(treePayload.Items) != 3 || treePayload.Items[0].Name != "nested" || treePayload.Items[0].Type != "directory" {
		t.Fatalf("stack workspace tree = %#v", treePayload)
	}

	fileResponse := performInternalJSONRequest(t, served, http.MethodGet, "/api/stacks/demo/workspace/file?path=notes.txt", nil, cookies)
	if fileResponse.Code != http.StatusOK {
		t.Fatalf("GET stack workspace file status = %d; body=%s", fileResponse.Code, fileResponse.Body.String())
	}
	var filePayload struct {
		StackID  string  `json:"stack_id"`
		Path     string  `json:"path"`
		Content  *string `json:"content"`
		Readable bool    `json:"readable"`
		Writable bool    `json:"writable"`
	}
	decodeInternalResponse(t, fileResponse, &filePayload)
	if filePayload.StackID != "demo" || filePayload.Path != "notes.txt" || filePayload.Content == nil || *filePayload.Content != "before\n" || !filePayload.Readable || !filePayload.Writable {
		t.Fatalf("stack workspace file = %#v", filePayload)
	}

	saveResponse := performInternalJSONRequest(t, served, http.MethodPut, "/api/stacks/demo/workspace/file", map[string]any{
		"path":                      "nested/runbook.md",
		"content":                   "# Runbook\n",
		"create_parent_directories": true,
	}, cookies)
	if saveResponse.Code != http.StatusOK {
		t.Fatalf("PUT stack workspace file status = %d; body=%s", saveResponse.Code, saveResponse.Body.String())
	}
	var savePayload struct {
		Saved       bool   `json:"saved"`
		StackID     string `json:"stack_id"`
		Path        string `json:"path"`
		AuditAction string `json:"audit_action"`
	}
	decodeInternalResponse(t, saveResponse, &savePayload)
	if !savePayload.Saved || savePayload.StackID != "demo" || savePayload.Path != "nested/runbook.md" || savePayload.AuditAction != "save_stack_file" {
		t.Fatalf("stack workspace save = %#v", savePayload)
	}
	content, err := os.ReadFile(filepath.Join(stackRoot, "nested", "runbook.md"))
	if err != nil || string(content) != "# Runbook\n" {
		t.Fatalf("saved stack workspace file = %q, %v", content, err)
	}

	errorCases := []struct {
		method string
		path   string
		body   any
		status int
		code   string
	}{
		{method: http.MethodGet, path: "/api/stacks/missing/workspace/tree", status: http.StatusNotFound, code: "not_found"},
		{method: http.MethodGet, path: "/api/stacks/demo/workspace/tree?path=notes.txt", status: http.StatusBadRequest, code: "path_not_directory"},
		{method: http.MethodGet, path: "/api/stacks/demo/workspace/file?path=compose.yaml", status: http.StatusConflict, code: "invalid_state"},
		{method: http.MethodGet, path: "/api/stacks/demo/workspace/file?path=nested", status: http.StatusBadRequest, code: "path_not_file"},
		{method: http.MethodGet, path: "/api/stacks/demo/workspace/file?path=..%2Foutside", status: http.StatusBadRequest, code: "path_outside_workspace"},
		{method: http.MethodPut, path: "/api/stacks/demo/workspace/file", body: map[string]any{"path": "compose.yaml", "content": "services: {}\n"}, status: http.StatusConflict, code: "invalid_state"},
	}
	for _, test := range errorCases {
		response := performInternalJSONRequest(t, served, test.method, test.path, test.body, cookies)
		assertCriticalHTTPError(t, response, test.status, test.code)
	}

	request := httptest.NewRequest(http.MethodPut, "http://stacklab.test/api/stacks/demo/workspace/file", strings.NewReader(`{"path":"notes.txt","content":"blocked"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://attacker.example")
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	crossOriginResponse := httptest.NewRecorder()
	served.ServeHTTP(crossOriginResponse, request)
	assertCriticalHTTPError(t, crossOriginResponse, http.StatusForbidden, "forbidden")
}

func TestHandlerServesTemplatesImageStatusFrontendAndFallbacks(t *testing.T) {
	t.Parallel()

	handler, served, cfg := newInternalTestHandler(t)
	cookies := loginInternalTestUser(t, served, "test-password")
	handler.imageUpdates.CacheStatuses([]store.ImageUpdateStatus{
		{ImageRef: "zeta:latest", State: "available", CheckedAt: time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)},
		{ImageRef: "alpha:latest", State: "up_to_date", CheckedAt: time.Date(2026, 7, 11, 12, 1, 0, 0, time.UTC)},
	})

	templatesResponse := performInternalJSONRequest(t, served, http.MethodGet, "/api/templates", nil, cookies)
	if templatesResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/templates status = %d; body=%s", templatesResponse.Code, templatesResponse.Body.String())
	}
	var templatesPayload struct {
		Items []json.RawMessage `json:"items"`
	}
	decodeInternalResponse(t, templatesResponse, &templatesPayload)
	if len(templatesPayload.Items) == 0 {
		t.Fatal("GET /api/templates returned no built-in templates")
	}

	updatesResponse := performInternalJSONRequest(t, served, http.MethodGet, "/api/maintenance/image-updates", nil, cookies)
	if updatesResponse.Code != http.StatusOK {
		t.Fatalf("GET image updates status = %d; body=%s", updatesResponse.Code, updatesResponse.Body.String())
	}
	var updatesPayload struct {
		Items []store.ImageUpdateStatus `json:"items"`
	}
	decodeInternalResponse(t, updatesResponse, &updatesPayload)
	if len(updatesPayload.Items) != 2 || updatesPayload.Items[0].ImageRef != "alpha:latest" || updatesPayload.Items[1].ImageRef != "zeta:latest" {
		t.Fatalf("image update items = %#v", updatesPayload.Items)
	}

	if err := os.WriteFile(filepath.Join(cfg.FrontendDistDir, "app.js"), []byte("console.log('stacklab')"), 0o644); err != nil {
		t.Fatalf("WriteFile(app.js) error = %v", err)
	}
	for _, test := range []struct {
		path string
		want string
	}{
		{path: "/", want: "<title>Stacklab</title>"},
		{path: "/app.js", want: "console.log('stacklab')"},
		{path: "/stacks/demo", want: "<title>Stacklab</title>"},
	} {
		response := httptest.NewRecorder()
		served.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://stacklab.test"+test.path, nil))
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), test.want) {
			t.Fatalf("GET %s = %d %q, want body containing %q", test.path, response.Code, response.Body.String(), test.want)
		}
	}

	notImplemented := performInternalJSONRequest(t, served, http.MethodGet, "/api/not-implemented", nil, cookies)
	assertCriticalHTTPError(t, notImplemented, http.StatusNotImplemented, "not_implemented")

	fallback := &Handler{
		cfg:    config.Config{FrontendDistDir: filepath.Join(t.TempDir(), "missing")},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	fallbackResponse := httptest.NewRecorder()
	(&systemController{Handler: fallback}).handleFrontend(fallbackResponse, httptest.NewRequest(http.MethodGet, "http://stacklab.test/", nil))
	if fallbackResponse.Code != http.StatusOK || !strings.Contains(fallbackResponse.Body.String(), "Frontend assets have not been built") {
		t.Fatalf("frontend fallback = %d %q", fallbackResponse.Code, fallbackResponse.Body.String())
	}
}

func TestHandlerValidatesStackActionRequestsAndResolvedSources(t *testing.T) {
	t.Parallel()

	_, served, _ := newInternalTestHandler(t)
	cookies := loginInternalTestUser(t, served, "test-password")
	cases := []struct {
		path   string
		body   any
		status int
		code   string
	}{
		{path: "/api/stacks/demo/actions/unsupported", body: map[string]any{}, status: http.StatusBadRequest, code: "validation_failed"},
		{path: "/api/stacks/missing/actions/up", body: map[string]any{}, status: http.StatusNotFound, code: "not_found"},
	}
	for _, test := range cases {
		response := performInternalJSONRequest(t, served, http.MethodPost, test.path, test.body, cookies)
		assertCriticalHTTPError(t, response, test.status, test.code)
	}

	unsupportedSource := performInternalJSONRequest(t, served, http.MethodGet, "/api/stacks/demo/resolved-config?source=unknown", nil, cookies)
	assertCriticalHTTPError(t, unsupportedSource, http.StatusBadRequest, "validation_failed")
	missingResolved := performInternalJSONRequest(t, served, http.MethodGet, "/api/stacks/missing/resolved-config?source=current", nil, cookies)
	assertCriticalHTTPError(t, missingResolved, http.StatusNotFound, "not_found")

	invalidJSON := httptest.NewRequest(http.MethodPost, "http://stacklab.test/api/stacks/demo/actions/up", bytes.NewBufferString("{"))
	invalidJSON.Header.Set("Content-Type", "application/json")
	invalidJSON.Header.Set("Origin", "http://stacklab.test")
	for _, cookie := range cookies {
		invalidJSON.AddCookie(cookie)
	}
	invalidJSONResponse := httptest.NewRecorder()
	served.ServeHTTP(invalidJSONResponse, invalidJSON)
	assertCriticalHTTPError(t, invalidJSONResponse, http.StatusBadRequest, "validation_failed")
}

func TestHandlerLogoutRevokesSessionAndClearsCookie(t *testing.T) {
	t.Parallel()

	_, served, cfg := newInternalTestHandler(t)
	cookies := loginInternalTestUser(t, served, "test-password")
	logoutResponse := performInternalJSONRequest(t, served, http.MethodPost, "/api/auth/logout", nil, cookies)
	if logoutResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/logout status = %d; body=%s", logoutResponse.Code, logoutResponse.Body.String())
	}
	cleared := false
	for _, cookie := range logoutResponse.Result().Cookies() {
		if cookie.Name == cfg.SessionCookieName && cookie.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatalf("logout cookies = %#v, want cleared %q", logoutResponse.Result().Cookies(), cfg.SessionCookieName)
	}
	staleResponse := performInternalJSONRequest(t, served, http.MethodGet, "/api/session", nil, cookies)
	assertCriticalHTTPError(t, staleResponse, http.StatusUnauthorized, "unauthorized")
}

func TestStackActionAndWebSocketErrorContracts(t *testing.T) {
	t.Parallel()

	for _, action := range []string{"validate", "up", "down", "stop", "restart", "pull", "build", "recreate"} {
		if !isSupportedStackAction(action) {
			t.Errorf("isSupportedStackAction(%q) = false", action)
		}
	}
	if isSupportedStackAction("delete") {
		t.Fatal("isSupportedStackAction(delete) = true")
	}
	if !stackActionAllowed([]string{"up", "down"}, "down") || stackActionAllowed([]string{"up"}, "down") {
		t.Fatal("stackActionAllowed() returned an unexpected result")
	}
	workflow := stackActionWorkflow("alpha", "up")
	if len(workflow) != 1 || workflow[0].State != "running" || workflow[0].TargetStackID != "alpha" {
		t.Fatalf("stackActionWorkflow() = %#v", workflow)
	}
	if step := workflowStepRef(workflow, 0); step == nil || step.Index != 1 || step.Total != 1 || step.Action != "up" {
		t.Fatalf("workflowStepRef() = %#v", step)
	}
	if workflowStepRef(workflow, -1) != nil || workflowStepRef(workflow, len(workflow)) != nil {
		t.Fatal("workflowStepRef() accepted an invalid index")
	}
	if got := markWorkflowFailed(workflow, 0); got[0].State != "failed" {
		t.Fatalf("markWorkflowFailed() = %#v", got)
	}
	if !stackActionUpdatesDeployBaseline("up") || !stackActionUpdatesDeployBaseline("recreate") || stackActionUpdatesDeployBaseline("pull") {
		t.Fatal("stackActionUpdatesDeployBaseline() returned an unexpected result")
	}
	if !stackActionInvalidatesImageUpdates("pull") || !stackActionInvalidatesImageUpdates("build") || stackActionInvalidatesImageUpdates("up") {
		t.Fatal("stackActionInvalidatesImageUpdates() returned an unexpected result")
	}

	timeoutCtx, cancelTimeout := context.WithCancelCause(context.Background())
	cancelTimeout(context.DeadlineExceeded)
	// context.WithCancelCause reports Canceled via ctx.Err(), so exercise the
	// deadline classification through the operation error itself.
	state, code, message, stepMessage := stackActionFailure(timeoutCtx, 30*time.Second, context.DeadlineExceeded)
	if state != "timed_out" || code != "stack_action_timed_out" || !strings.Contains(message, "30s") || stepMessage != "Stack action timed out." {
		t.Fatalf("stackActionFailure(timeout) = %q %q %q %q", state, code, message, stepMessage)
	}
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	state, code, message, stepMessage = stackActionFailure(cancelledCtx, time.Minute, errors.New("ignored"))
	if state != "cancelled" || code != "stack_action_cancelled" || message != "Stack action was cancelled." || stepMessage != "Stack action cancelled." {
		t.Fatalf("stackActionFailure(cancel) = %q %q %q %q", state, code, message, stepMessage)
	}
	state, code, message, stepMessage = stackActionFailure(context.Background(), time.Minute, errors.New("compose failed"))
	if state != "failed" || code != "stack_action_failed" || message != "compose failed" || stepMessage != "Stack action failed." {
		t.Fatalf("stackActionFailure(failure) = %q %q %q %q", state, code, message, stepMessage)
	}

	frame := wsClientFrame{RequestID: "req-1", StreamID: "stream-1"}
	frames := []struct {
		frame wsServerFrame
		code  string
	}{
		{frame: validationErrorFrame(frame, "invalid"), code: "validation_failed"},
		{frame: notFoundErrorFrame(frame, "missing"), code: "not_found"},
		{frame: internalErrorFrame(frame, "failed"), code: "internal_error"},
		{frame: streamErrorFrame("stream-1", "stream_failed", "failed"), code: "stream_failed"},
	}
	for _, test := range frames {
		if test.frame.Type != "error" || test.frame.StreamID != "stream-1" {
			t.Errorf("websocket error frame = %#v", test.frame)
			continue
		}
		errorPayload, ok := test.frame.Error.(map[string]any)
		if !ok || errorPayload["code"] != test.code {
			t.Errorf("websocket error payload = %#v, want code %q", test.frame.Error, test.code)
		}
	}
	if emptyToNil("") != nil || emptyToNil("value") != "value" {
		t.Fatal("emptyToNil() returned an unexpected result")
	}
}

func assertCriticalHTTPError(t *testing.T, response *httptest.ResponseRecorder, wantStatus int, wantCode string) {
	t.Helper()
	if response.Code != wantStatus {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, wantStatus, response.Body.String())
	}
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	decodeInternalResponse(t, response, &payload)
	if payload.Error.Code != wantCode {
		t.Fatalf("error code = %q, want %q; body=%s", payload.Error.Code, wantCode, response.Body.String())
	}
}
