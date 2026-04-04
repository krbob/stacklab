package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"stacklab/internal/audit"
	"stacklab/internal/auth"
	"stacklab/internal/config"
	"stacklab/internal/httpapi"
	"stacklab/internal/jobs"
	"stacklab/internal/store"
)

func TestHandlerLoginSessionAndPasswordUpdate(t *testing.T) {
	t.Parallel()

	handler, _ := newTestHandler(t)

	metaResponse := performJSONRequest(t, handler, http.MethodGet, "/api/meta", nil, nil)
	if metaResponse.Code != http.StatusUnauthorized {
		t.Fatalf("GET /api/meta status = %d, want %d", metaResponse.Code, http.StatusUnauthorized)
	}

	loginResponse := performJSONRequest(t, handler, http.MethodPost, "/api/auth/login", map[string]any{
		"password": "secret",
	}, nil)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login status = %d, want %d", loginResponse.Code, http.StatusOK)
	}
	cookies := loginResponse.Result().Cookies()
	if len(cookies) == 0 || cookies[0].Name != "stacklab_session" {
		t.Fatalf("expected stacklab_session cookie, got %#v", cookies)
	}

	sessionResponse := performJSONRequest(t, handler, http.MethodGet, "/api/session", nil, cookies)
	if sessionResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/session status = %d, want %d", sessionResponse.Code, http.StatusOK)
	}
	var sessionPayload struct {
		Authenticated bool `json:"authenticated"`
		User          struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	decodeResponse(t, sessionResponse, &sessionPayload)
	if !sessionPayload.Authenticated || sessionPayload.User.ID != "local" {
		t.Fatalf("unexpected session payload: %#v", sessionPayload)
	}

	passwordResponse := performJSONRequest(t, handler, http.MethodPost, "/api/settings/password", map[string]any{
		"current_password": "secret",
		"new_password":     "newsecret",
	}, cookies)
	if passwordResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/settings/password status = %d, want %d", passwordResponse.Code, http.StatusOK)
	}

	oldLoginResponse := performJSONRequest(t, handler, http.MethodPost, "/api/auth/login", map[string]any{
		"password": "secret",
	}, nil)
	if oldLoginResponse.Code != http.StatusUnauthorized {
		t.Fatalf("POST /api/auth/login(old password) status = %d, want %d", oldLoginResponse.Code, http.StatusUnauthorized)
	}

	newLoginResponse := performJSONRequest(t, handler, http.MethodPost, "/api/auth/login", map[string]any{
		"password": "newsecret",
	}, nil)
	if newLoginResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login(new password) status = %d, want %d", newLoginResponse.Code, http.StatusOK)
	}
}

func TestHandlerCreateAndDeleteStackWithoutRuntime(t *testing.T) {
	t.Parallel()

	handler, cfg := newTestHandler(t)
	cookies := loginTestUser(t, handler, "secret")
	stackID := "fixture-create-delete"

	createResponse := performJSONRequest(t, handler, http.MethodPost, "/api/stacks", map[string]any{
		"stack_id":            stackID,
		"compose_yaml":        "services:\n  app:\n    image: nginx:alpine\n",
		"env":                 "PORT=8080\n",
		"create_config_dir":   true,
		"create_data_dir":     true,
		"deploy_after_create": false,
	}, cookies)
	if createResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/stacks status = %d, want %d", createResponse.Code, http.StatusOK)
	}
	var createPayload struct {
		Job struct {
			Action   string `json:"action"`
			StackID  string `json:"stack_id"`
			State    string `json:"state"`
			Workflow *struct {
				Steps []struct {
					Action string `json:"action"`
					State  string `json:"state"`
				} `json:"steps"`
			} `json:"workflow"`
		} `json:"job"`
	}
	decodeResponse(t, createResponse, &createPayload)
	if createPayload.Job.Action != "create_stack" || createPayload.Job.StackID != stackID || createPayload.Job.State != "succeeded" {
		t.Fatalf("unexpected create job payload: %#v", createPayload.Job)
	}
	if createPayload.Job.Workflow == nil || len(createPayload.Job.Workflow.Steps) != 1 {
		t.Fatalf("expected single-step create workflow, got %#v", createPayload.Job.Workflow)
	}

	assertPathExists(t, filepath.Join(cfg.RootDir, "stacks", stackID, "compose.yaml"))
	assertPathExists(t, filepath.Join(cfg.RootDir, "stacks", stackID, ".env"))
	assertPathExists(t, filepath.Join(cfg.RootDir, "config", stackID))
	assertPathExists(t, filepath.Join(cfg.RootDir, "data", stackID))

	detailResponse := performJSONRequest(t, handler, http.MethodGet, "/api/stacks/"+stackID, nil, cookies)
	if detailResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/stacks/%s status = %d, want %d", stackID, detailResponse.Code, http.StatusOK)
	}

	deleteResponse := performJSONRequest(t, handler, http.MethodDelete, "/api/stacks/"+stackID, map[string]any{
		"remove_runtime":    false,
		"remove_definition": true,
		"remove_config":     true,
		"remove_data":       true,
	}, cookies)
	if deleteResponse.Code != http.StatusOK {
		t.Fatalf("DELETE /api/stacks/%s status = %d, want %d", stackID, deleteResponse.Code, http.StatusOK)
	}
	var deletePayload struct {
		Job struct {
			Action string `json:"action"`
			State  string `json:"state"`
		} `json:"job"`
	}
	decodeResponse(t, deleteResponse, &deletePayload)
	if deletePayload.Job.Action != "remove_stack_definition" || deletePayload.Job.State != "succeeded" {
		t.Fatalf("unexpected delete job payload: %#v", deletePayload.Job)
	}

	assertPathMissing(t, filepath.Join(cfg.RootDir, "stacks", stackID))
	assertPathMissing(t, filepath.Join(cfg.RootDir, "config", stackID))
	assertPathMissing(t, filepath.Join(cfg.RootDir, "data", stackID))

	listResponse := performJSONRequest(t, handler, http.MethodGet, "/api/stacks", nil, cookies)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/stacks status = %d, want %d", listResponse.Code, http.StatusOK)
	}
	var listPayload struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	decodeResponse(t, listResponse, &listPayload)
	for _, item := range listPayload.Items {
		if item.ID == stackID {
			t.Fatalf("expected stack %q to be absent after delete, got %#v", stackID, listPayload.Items)
		}
	}

	auditResponse := performJSONRequest(t, handler, http.MethodGet, "/api/audit", nil, cookies)
	if auditResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/audit status = %d, want %d", auditResponse.Code, http.StatusOK)
	}
	var auditPayload struct {
		Items []struct {
			Action string `json:"action"`
		} `json:"items"`
	}
	decodeResponse(t, auditResponse, &auditPayload)
	if len(auditPayload.Items) < 2 {
		t.Fatalf("expected at least two audit entries, got %#v", auditPayload.Items)
	}
	if auditPayload.Items[0].Action != "remove_stack_definition" || auditPayload.Items[1].Action != "create_stack" {
		t.Fatalf("unexpected audit actions order: %#v", auditPayload.Items)
	}
}

func TestWebSocketReplaysJobEvents(t *testing.T) {
	t.Parallel()

	handler, _ := newTestHandler(t)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client := server.Client()
	loginRequestBody := bytes.NewBufferString(`{"password":"secret"}`)
	loginRequest, err := http.NewRequest(http.MethodPost, server.URL+"/api/auth/login", loginRequestBody)
	if err != nil {
		t.Fatalf("http.NewRequest(login) error = %v", err)
	}
	loginRequest.Header.Set("Origin", server.URL)
	loginRequest.Header.Set("Content-Type", "application/json")

	loginResponse, err := client.Do(loginRequest)
	if err != nil {
		t.Fatalf("client.Do(login) error = %v", err)
	}
	defer loginResponse.Body.Close()
	if loginResponse.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want %d", loginResponse.StatusCode, http.StatusOK)
	}

	cookies := loginResponse.Cookies()
	if len(cookies) == 0 {
		t.Fatalf("expected login to set cookies")
	}
	stackID := "fixture-jobs-replay"

	createRequestBody := bytes.NewBufferString(`{"stack_id":"` + stackID + `","compose_yaml":"services:\n  app:\n    image: nginx:alpine\n","env":"","create_config_dir":false,"create_data_dir":false,"deploy_after_create":false}`)
	createRequest, err := http.NewRequest(http.MethodPost, server.URL+"/api/stacks", createRequestBody)
	if err != nil {
		t.Fatalf("http.NewRequest(create) error = %v", err)
	}
	createRequest.Header.Set("Origin", server.URL)
	createRequest.Header.Set("Content-Type", "application/json")
	for _, cookie := range cookies {
		createRequest.AddCookie(cookie)
	}

	createResponse, err := client.Do(createRequest)
	if err != nil {
		t.Fatalf("client.Do(create) error = %v", err)
	}
	defer createResponse.Body.Close()
	if createResponse.StatusCode != http.StatusOK {
		t.Fatalf("create status = %d, want %d", createResponse.StatusCode, http.StatusOK)
	}

	var createPayload struct {
		Job struct {
			ID string `json:"id"`
		} `json:"job"`
	}
	if err := json.NewDecoder(createResponse.Body).Decode(&createPayload); err != nil {
		t.Fatalf("decode create response error = %v", err)
	}
	if createPayload.Job.ID == "" {
		t.Fatalf("expected create job id")
	}

	wsURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("url.Parse(server.URL) error = %v", err)
	}
	wsURL.Scheme = strings.Replace(wsURL.Scheme, "http", "ws", 1)
	wsURL.Path = "/api/ws"

	header := http.Header{}
	header.Set("Origin", server.URL)
	for _, cookie := range cookies {
		header.Add("Cookie", cookie.Name+"="+cookie.Value)
	}

	wsConn, wsResponse, err := websocket.DefaultDialer.Dial(wsURL.String(), header)
	if err != nil {
		if wsResponse != nil {
			body, _ := io.ReadAll(wsResponse.Body)
			_ = wsResponse.Body.Close()
			t.Fatalf("websocket dial error = %v (status=%d body=%q)", err, wsResponse.StatusCode, string(body))
		}
		t.Fatalf("websocket dial error = %v", err)
	}
	defer wsConn.Close()
	_ = wsConn.SetReadDeadline(time.Now().Add(5 * time.Second))

	var helloFrame struct {
		Type    string `json:"type"`
		Payload struct {
			ConnectionID        string `json:"connection_id"`
			ProtocolVersion     int    `json:"protocol_version"`
			HeartbeatIntervalMS int    `json:"heartbeat_interval_ms"`
		} `json:"payload"`
	}
	if err := wsConn.ReadJSON(&helloFrame); err != nil {
		t.Fatalf("ReadJSON(hello) error = %v", err)
	}
	if helloFrame.Type != "hello" || helloFrame.Payload.ConnectionID == "" || helloFrame.Payload.ProtocolVersion != 1 {
		t.Fatalf("unexpected hello frame: %#v", helloFrame)
	}

	subscribeFrame := map[string]any{
		"type":       "jobs.subscribe",
		"request_id": "req_1",
		"stream_id":  "job_demo",
		"payload": map[string]any{
			"job_id": createPayload.Job.ID,
		},
	}
	if err := wsConn.WriteJSON(subscribeFrame); err != nil {
		t.Fatalf("WriteJSON(subscribe) error = %v", err)
	}

	var ackFrame struct {
		Type      string `json:"type"`
		RequestID string `json:"request_id"`
		StreamID  string `json:"stream_id"`
		Payload   struct {
			Status string `json:"status"`
		} `json:"payload"`
	}
	if err := wsConn.ReadJSON(&ackFrame); err != nil {
		t.Fatalf("ReadJSON(ack) error = %v", err)
	}
	if ackFrame.Type != "ack" || ackFrame.RequestID != "req_1" || ackFrame.StreamID != "job_demo" || ackFrame.Payload.Status != "subscribed" {
		t.Fatalf("unexpected ack frame: %#v", ackFrame)
	}

	eventNames := make([]string, 0, 8)
	for {
		var eventFrame struct {
			Type     string `json:"type"`
			StreamID string `json:"stream_id"`
			Payload  struct {
				JobID   string `json:"job_id"`
				Event   string `json:"event"`
				State   string `json:"state"`
				Message string `json:"message"`
			} `json:"payload"`
		}
		if err := wsConn.ReadJSON(&eventFrame); err != nil {
			t.Fatalf("ReadJSON(job event) error = %v", err)
		}
		if eventFrame.Type != "jobs.event" {
			continue
		}
		if eventFrame.StreamID != "job_demo" || eventFrame.Payload.JobID != createPayload.Job.ID {
			t.Fatalf("unexpected job event frame: %#v", eventFrame)
		}
		eventNames = append(eventNames, eventFrame.Payload.Event)
		if eventFrame.Payload.Event == "job_finished" {
			if eventFrame.Payload.State != "succeeded" {
				t.Fatalf("job_finished state = %q, want %q", eventFrame.Payload.State, "succeeded")
			}
			break
		}
	}

	if !containsString(eventNames, "job_started") {
		t.Fatalf("expected replay to include job_started, got %#v", eventNames)
	}
	if !containsString(eventNames, "job_step_started") {
		t.Fatalf("expected replay to include job_step_started, got %#v", eventNames)
	}
	if !containsString(eventNames, "job_step_finished") {
		t.Fatalf("expected replay to include job_step_finished, got %#v", eventNames)
	}
	if !containsString(eventNames, "job_finished") {
		t.Fatalf("expected replay to include job_finished, got %#v", eventNames)
	}
}

func TestSaveDefinitionWarningPrecedesJobFinished(t *testing.T) {
	t.Parallel()

	handler, cfg := newTestHandler(t)
	cookies := loginTestUser(t, handler, "secret")
	stackID := "fixture-save-warning"

	createResponse := performJSONRequest(t, handler, http.MethodPost, "/api/stacks", map[string]any{
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

	updateResponse := performJSONRequest(t, handler, http.MethodPut, "/api/stacks/"+stackID+"/definition", map[string]any{
		"compose_yaml":        "services:\n  app:\n    image: nginx:alpine\n    environment:\n      REQUIRED: ${MISSING?required}\n",
		"env":                 "",
		"validate_after_save": true,
	}, cookies)
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("PUT /api/stacks/%s/definition status = %d, want %d", stackID, updateResponse.Code, http.StatusOK)
	}

	var updatePayload struct {
		Job struct {
			ID    string `json:"id"`
			State string `json:"state"`
		} `json:"job"`
	}
	decodeResponse(t, updateResponse, &updatePayload)
	if updatePayload.Job.ID == "" || updatePayload.Job.State != "succeeded" {
		t.Fatalf("unexpected update job payload: %#v", updatePayload.Job)
	}

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	wsURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("url.Parse(server.URL) error = %v", err)
	}
	wsURL.Scheme = strings.Replace(wsURL.Scheme, "http", "ws", 1)
	wsURL.Path = "/api/ws"

	header := http.Header{}
	header.Set("Origin", server.URL)
	for _, cookie := range cookies {
		header.Add("Cookie", cookie.Name+"="+cookie.Value)
	}

	wsConn, wsResponse, err := websocket.DefaultDialer.Dial(wsURL.String(), header)
	if err != nil {
		if wsResponse != nil {
			body, _ := io.ReadAll(wsResponse.Body)
			_ = wsResponse.Body.Close()
			t.Fatalf("websocket dial error = %v (status=%d body=%q)", err, wsResponse.StatusCode, string(body))
		}
		t.Fatalf("websocket dial error = %v", err)
	}
	defer wsConn.Close()
	_ = wsConn.SetReadDeadline(time.Now().Add(5 * time.Second))

	var helloFrame map[string]any
	if err := wsConn.ReadJSON(&helloFrame); err != nil {
		t.Fatalf("ReadJSON(hello) error = %v", err)
	}

	if err := wsConn.WriteJSON(map[string]any{
		"type":       "jobs.subscribe",
		"request_id": "req_save_1",
		"stream_id":  "job_save_demo",
		"payload": map[string]any{
			"job_id": updatePayload.Job.ID,
		},
	}); err != nil {
		t.Fatalf("WriteJSON(subscribe) error = %v", err)
	}

	var ackFrame map[string]any
	if err := wsConn.ReadJSON(&ackFrame); err != nil {
		t.Fatalf("ReadJSON(ack) error = %v", err)
	}

	eventNames := make([]string, 0, 8)
	for {
		var eventFrame struct {
			Type    string `json:"type"`
			Payload struct {
				Event string `json:"event"`
			} `json:"payload"`
		}
		if err := wsConn.ReadJSON(&eventFrame); err != nil {
			t.Fatalf("ReadJSON(job event) error = %v", err)
		}
		if eventFrame.Type != "jobs.event" {
			continue
		}
		eventNames = append(eventNames, eventFrame.Payload.Event)
		if eventFrame.Payload.Event == "job_finished" {
			break
		}
	}

	warningIndex := indexOfString(eventNames, "job_warning")
	finishedIndex := indexOfString(eventNames, "job_finished")
	if warningIndex == -1 {
		t.Fatalf("expected replay to include job_warning, got %#v", eventNames)
	}
	if finishedIndex == -1 {
		t.Fatalf("expected replay to include job_finished, got %#v", eventNames)
	}
	if warningIndex > finishedIndex {
		t.Fatalf("expected job_warning before job_finished, got %#v", eventNames)
	}

	assertPathExists(t, filepath.Join(cfg.RootDir, "stacks", stackID, "compose.yaml"))
}

func TestWebSocketTerminalAttachMissingSession(t *testing.T) {
	t.Parallel()

	handler, _ := newTestHandler(t)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client := server.Client()
	loginRequestBody := bytes.NewBufferString(`{"password":"secret"}`)
	loginRequest, err := http.NewRequest(http.MethodPost, server.URL+"/api/auth/login", loginRequestBody)
	if err != nil {
		t.Fatalf("http.NewRequest(login) error = %v", err)
	}
	loginRequest.Header.Set("Origin", server.URL)
	loginRequest.Header.Set("Content-Type", "application/json")

	loginResponse, err := client.Do(loginRequest)
	if err != nil {
		t.Fatalf("client.Do(login) error = %v", err)
	}
	defer loginResponse.Body.Close()
	if loginResponse.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want %d", loginResponse.StatusCode, http.StatusOK)
	}

	cookies := loginResponse.Cookies()
	if len(cookies) == 0 {
		t.Fatalf("expected login to set cookies")
	}

	wsURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("url.Parse(server.URL) error = %v", err)
	}
	wsURL.Scheme = strings.Replace(wsURL.Scheme, "http", "ws", 1)
	wsURL.Path = "/api/ws"

	header := http.Header{}
	header.Set("Origin", server.URL)
	for _, cookie := range cookies {
		header.Add("Cookie", cookie.Name+"="+cookie.Value)
	}

	wsConn, wsResponse, err := websocket.DefaultDialer.Dial(wsURL.String(), header)
	if err != nil {
		if wsResponse != nil {
			body, _ := io.ReadAll(wsResponse.Body)
			_ = wsResponse.Body.Close()
			t.Fatalf("websocket dial error = %v (status=%d body=%q)", err, wsResponse.StatusCode, string(body))
		}
		t.Fatalf("websocket dial error = %v", err)
	}
	defer wsConn.Close()
	_ = wsConn.SetReadDeadline(time.Now().Add(5 * time.Second))

	var helloFrame map[string]any
	if err := wsConn.ReadJSON(&helloFrame); err != nil {
		t.Fatalf("ReadJSON(hello) error = %v", err)
	}

	if err := wsConn.WriteJSON(map[string]any{
		"type":       "terminal.attach",
		"request_id": "req_attach_1",
		"stream_id":  "term_demo",
		"payload": map[string]any{
			"session_id": "term_missing",
			"cols":       120,
			"rows":       36,
		},
	}); err != nil {
		t.Fatalf("WriteJSON(terminal.attach) error = %v", err)
	}

	var errorFrame struct {
		Type      string `json:"type"`
		RequestID string `json:"request_id"`
		StreamID  string `json:"stream_id"`
		Error     struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := wsConn.ReadJSON(&errorFrame); err != nil {
		t.Fatalf("ReadJSON(error) error = %v", err)
	}
	if errorFrame.Type != "error" || errorFrame.RequestID != "req_attach_1" || errorFrame.StreamID != "term_demo" || errorFrame.Error.Code != "terminal_session_not_found" {
		t.Fatalf("unexpected terminal attach error frame: %#v", errorFrame)
	}
}

func newTestHandler(t *testing.T) (http.Handler, config.Config) {
	t.Helper()

	tempDir := t.TempDir()
	cfg := config.Config{
		RootDir:                 filepath.Join(tempDir, "root"),
		DataDir:                 filepath.Join(tempDir, "var"),
		DatabasePath:            filepath.Join(tempDir, "var", "stacklab.db"),
		FrontendDistDir:         filepath.Join(tempDir, "frontend"),
		BootstrapPassword:       "secret",
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

	handler, err := httpapi.NewHandler(cfg, logger, authService, auditService, jobService)
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}

	return handler, cfg
}

func loginTestUser(t *testing.T, handler http.Handler, password string) []*http.Cookie {
	t.Helper()

	response := performJSONRequest(t, handler, http.MethodPost, "/api/auth/login", map[string]any{
		"password": password,
	}, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login status = %d, want %d", response.Code, http.StatusOK)
	}

	return response.Result().Cookies()
}

func performJSONRequest(t *testing.T, handler http.Handler, method, path string, body any, cookies []*http.Cookie) *httptest.ResponseRecorder {
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
	handler.ServeHTTP(recorder, request)
	return recorder
}

func decodeResponse(t *testing.T, recorder *httptest.ResponseRecorder, destination any) {
	t.Helper()

	if err := json.NewDecoder(recorder.Body).Decode(destination); err != nil {
		t.Fatalf("json.Decode() error = %v; body = %s", err, recorder.Body.String())
	}
}

func assertPathExists(t *testing.T, path string) {
	t.Helper()

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected path %q to exist: %v", path, err)
	}
}

func assertPathMissing(t *testing.T, path string) {
	t.Helper()

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected path %q to be missing, got err = %v", path, err)
	}
}

func indexOfString(values []string, target string) int {
	for index, value := range values {
		if value == target {
			return index
		}
	}
	return -1
}

func containsString(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func writeTestDockerShim(t *testing.T, path string) {
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
