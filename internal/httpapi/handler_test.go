package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	"stacklab/internal/maintenance"
	"stacklab/internal/maintenancejobs"
	"stacklab/internal/notifications"
	"stacklab/internal/scheduler"
	"stacklab/internal/selfupdate"
	"stacklab/internal/stacks"
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
		"password": "test-password",
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

	weakPasswordResponse := performJSONRequest(t, handler, http.MethodPost, "/api/settings/password", map[string]any{
		"current_password": "test-password",
		"new_password":     "too-short",
	}, cookies)
	if weakPasswordResponse.Code != http.StatusUnprocessableEntity {
		t.Fatalf("POST /api/settings/password with short password status = %d, want %d", weakPasswordResponse.Code, http.StatusUnprocessableEntity)
	}
	var weakPasswordPayload struct {
		Error struct {
			Code    string `json:"code"`
			Details struct {
				MinLength int `json:"min_length"`
				MaxLength int `json:"max_length"`
			} `json:"details"`
		} `json:"error"`
	}
	decodeResponse(t, weakPasswordResponse, &weakPasswordPayload)
	if weakPasswordPayload.Error.Code != "validation_failed" ||
		weakPasswordPayload.Error.Details.MinLength != auth.PasswordMinimumLength ||
		weakPasswordPayload.Error.Details.MaxLength != auth.PasswordMaximumLength {
		t.Fatalf("unexpected password validation error: %#v", weakPasswordPayload)
	}
	longPasswordResponse := performJSONRequest(t, handler, http.MethodPost, "/api/settings/password", map[string]any{
		"current_password": "test-password",
		"new_password":     strings.Repeat("x", auth.PasswordMaximumLength+1),
	}, cookies)
	if longPasswordResponse.Code != http.StatusUnprocessableEntity {
		t.Fatalf("POST /api/settings/password with long password status = %d, want %d", longPasswordResponse.Code, http.StatusUnprocessableEntity)
	}

	passwordResponse := performJSONRequest(t, handler, http.MethodPost, "/api/settings/password", map[string]any{
		"current_password": "test-password",
		"new_password":     "new-test-password",
	}, cookies)
	if passwordResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/settings/password status = %d, want %d", passwordResponse.Code, http.StatusOK)
	}
	passwordCookies := passwordResponse.Result().Cookies()
	cleared := false
	for _, cookie := range passwordCookies {
		cleared = cleared || cookie.MaxAge == -1
	}
	if !cleared {
		t.Fatalf("password update did not clear the session cookie: %#v", passwordCookies)
	}
	staleSessionResponse := performJSONRequest(t, handler, http.MethodGet, "/api/session", nil, cookies)
	if staleSessionResponse.Code != http.StatusUnauthorized {
		t.Fatalf("GET /api/session with pre-change cookie status = %d, want %d", staleSessionResponse.Code, http.StatusUnauthorized)
	}

	oldLoginResponse := performJSONRequest(t, handler, http.MethodPost, "/api/auth/login", map[string]any{
		"password": "test-password",
	}, nil)
	if oldLoginResponse.Code != http.StatusUnauthorized {
		t.Fatalf("POST /api/auth/login(old password) status = %d, want %d", oldLoginResponse.Code, http.StatusUnauthorized)
	}

	newLoginResponse := performJSONRequest(t, handler, http.MethodPost, "/api/auth/login", map[string]any{
		"password": "new-test-password",
	}, nil)
	if newLoginResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login(new password) status = %d, want %d", newLoginResponse.Code, http.StatusOK)
	}
}

func TestAuthenticatedResponsesRefreshAbsoluteCookieAcrossRESTAndWebSocket(t *testing.T) {
	handler, cfg := newTestHandler(t)
	cookies := loginTestUser(t, handler, "test-password")
	loginCookie := findTestCookie(t, cookies, cfg.SessionCookieName)
	remaining := time.Until(loginCookie.Expires)
	if remaining < cfg.SessionAbsoluteLifetime-time.Minute || remaining > cfg.SessionAbsoluteLifetime+time.Minute {
		t.Fatalf("login cookie lifetime = %s, want approximately %s", remaining, cfg.SessionAbsoluteLifetime)
	}

	for _, path := range []string{"/api/session", "/api/meta"} {
		response := performJSONRequest(t, handler, http.MethodGet, path, nil, cookies)
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, want %d", path, response.Code, http.StatusOK)
		}
		refreshed := findTestCookie(t, response.Result().Cookies(), cfg.SessionCookieName)
		if refreshed.Value != loginCookie.Value || !refreshed.Expires.Equal(loginCookie.Expires) {
			t.Fatalf("GET %s refreshed cookie = %#v, want session value and absolute expiry %#v", path, refreshed, loginCookie)
		}
	}

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	wsURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("url.Parse(server.URL) error = %v", err)
	}
	wsURL.Scheme = strings.Replace(wsURL.Scheme, "http", "ws", 1)
	wsURL.Path = "/api/ws"
	header := http.Header{"Origin": []string{server.URL}}
	for _, cookie := range cookies {
		header.Add("Cookie", cookie.Name+"="+cookie.Value)
	}
	wsConn, wsResponse, err := websocket.DefaultDialer.Dial(wsURL.String(), header)
	if err != nil {
		if wsResponse != nil {
			_ = wsResponse.Body.Close()
		}
		t.Fatalf("websocket.Dial() error = %v", err)
	}
	defer wsConn.Close()
	refreshed := findTestCookie(t, wsResponse.Cookies(), cfg.SessionCookieName)
	if refreshed.Value != loginCookie.Value || !refreshed.Expires.Equal(loginCookie.Expires) {
		t.Fatalf("websocket refreshed cookie = %#v, want session value and absolute expiry %#v", refreshed, loginCookie)
	}
}

func TestSessionDatabaseFailuresReturn500WithoutClearingCookie(t *testing.T) {
	handler, cfg := newTestHandler(t)
	cookies := loginTestUser(t, handler, "test-password")

	for _, path := range []string{"/api/session", "/api/meta", "/api/auth/logout", "/api/ws"} {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "http://stacklab.test"+path, nil)
			if path == "/api/auth/logout" {
				request.Method = http.MethodPost
			}
			request.Header.Set("Origin", "http://stacklab.test")
			for _, cookie := range cookies {
				request.AddCookie(cookie)
			}
			ctx, cancel := context.WithCancel(request.Context())
			cancel()
			request = request.WithContext(ctx)
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			if response.Code != http.StatusInternalServerError {
				t.Fatalf("%s status = %d, want %d", path, response.Code, http.StatusInternalServerError)
			}
			for _, cookie := range response.Result().Cookies() {
				if cookie.Name == cfg.SessionCookieName && cookie.MaxAge < 0 {
					t.Fatalf("%s cleared cookie on database failure: %#v", path, cookie)
				}
			}
		})
	}
}

func TestHandlerSetsSecurityHeaders(t *testing.T) {
	t.Parallel()

	handler, _ := newTestHandler(t)

	response := performJSONRequest(t, handler, http.MethodGet, "/api/health", nil, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("GET /api/health status = %d, want %d", response.Code, http.StatusOK)
	}

	headers := response.Result().Header
	if got := headers.Get("Content-Security-Policy"); got != "default-src 'self'; connect-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'" {
		t.Fatalf("Content-Security-Policy = %q", got)
	}
	if got := headers.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := headers.Get("Referrer-Policy"); got != "same-origin" {
		t.Fatalf("Referrer-Policy = %q, want same-origin", got)
	}
	if got := headers.Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("X-Frame-Options = %q, want DENY", got)
	}
	if got := headers.Get("X-Request-ID"); got == "" {
		t.Fatal("X-Request-ID is empty")
	}
}

func TestHandlerCorrelatesRequestWithStartedJob(t *testing.T) {
	t.Parallel()

	handler, _ := newTestHandler(t)
	cookies := loginTestUser(t, handler, "test-password")
	payload, err := json.Marshal(map[string]any{
		"scope": map[string]any{
			"images":             true,
			"build_cache":        false,
			"stopped_containers": false,
			"volumes":            false,
		},
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	const requestID = "req_prune_support_123"
	request := httptest.NewRequest(http.MethodPost, "http://stacklab.test/api/maintenance/prune", bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://stacklab.test")
	request.Header.Set("X-Request-ID", requestID)
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("POST /api/maintenance/prune status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	if got := response.Header().Get("X-Request-ID"); got != requestID {
		t.Fatalf("response X-Request-ID = %q, want %q", got, requestID)
	}
	var started struct {
		Job struct {
			ID        string `json:"id"`
			RequestID string `json:"request_id"`
		} `json:"job"`
	}
	decodeResponse(t, response, &started)
	if started.Job.ID == "" || started.Job.RequestID != requestID {
		t.Fatalf("started job correlation = %#v", started.Job)
	}

	detailResponse := performJSONRequest(t, handler, http.MethodGet, "/api/jobs/"+started.Job.ID, nil, cookies)
	if detailResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/jobs/%s status = %d; body=%s", started.Job.ID, detailResponse.Code, detailResponse.Body.String())
	}
	var detail struct {
		Job struct {
			RequestID string `json:"request_id"`
		} `json:"job"`
	}
	decodeResponse(t, detailResponse, &detail)
	if detail.Job.RequestID != requestID {
		t.Fatalf("persisted job request ID = %q, want %q", detail.Job.RequestID, requestID)
	}
}

func TestHandlerHealthRoutesSeparateLiveAndReady(t *testing.T) {
	t.Parallel()

	handler, cfg := newTestHandler(t)
	for _, path := range []string{"/api/live", "/api/ready", "/api/health"} {
		response := performJSONRequest(t, handler, http.MethodGet, path, nil, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, want %d; body=%s", path, response.Code, http.StatusOK, response.Body.String())
		}
	}

	if err := os.Remove(filepath.Join(cfg.FrontendDistDir, "index.html")); err != nil {
		t.Fatalf("Remove(frontend index) error = %v", err)
	}
	for _, path := range []string{"/api/ready", "/api/health"} {
		response := performJSONRequest(t, handler, http.MethodGet, path, nil, nil)
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("GET %s without assets status = %d, want %d; body=%s", path, response.Code, http.StatusServiceUnavailable, response.Body.String())
		}
	}
	liveResponse := performJSONRequest(t, handler, http.MethodGet, "/api/live", nil, nil)
	if liveResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/live without assets status = %d, want %d; body=%s", liveResponse.Code, http.StatusOK, liveResponse.Body.String())
	}
}

func TestHandlerRateLimitsRepeatedLoginFailures(t *testing.T) {
	t.Parallel()

	handler, _ := newTestHandler(t)

	for i := 0; i < 5; i++ {
		response := performJSONRequest(t, handler, http.MethodPost, "/api/auth/login", map[string]any{
			"password": "wrong",
		}, nil)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("POST /api/auth/login wrong attempt %d status = %d, want %d", i+1, response.Code, http.StatusUnauthorized)
		}
	}

	response := performJSONRequest(t, handler, http.MethodPost, "/api/auth/login", map[string]any{
		"password": "test-password",
	}, nil)
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("POST /api/auth/login locked status = %d, want %d", response.Code, http.StatusTooManyRequests)
	}

	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	decodeResponse(t, response, &payload)
	if payload.Error.Code != "rate_limited" {
		t.Fatalf("error code = %q, want rate_limited", payload.Error.Code)
	}
}

func TestHandlerRejectsOversizedLoginBody(t *testing.T) {
	t.Parallel()

	const loginBodyLimit int64 = 4 << 10
	handler, _ := newTestHandler(t)
	body := `{"password":"` + strings.Repeat("x", int(loginBodyLimit)) + `"}`
	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("POST /api/auth/login oversized status = %d, want %d", response.Code, http.StatusRequestEntityTooLarge)
	}
	var payload struct {
		Error struct {
			Code    string `json:"code"`
			Details struct {
				MaxBytes int64 `json:"max_bytes"`
			} `json:"details"`
		} `json:"error"`
	}
	decodeResponse(t, response, &payload)
	if payload.Error.Code != "request_too_large" {
		t.Fatalf("error code = %q, want request_too_large", payload.Error.Code)
	}
	if payload.Error.Details.MaxBytes != loginBodyLimit {
		t.Fatalf("max_bytes = %d, want %d", payload.Error.Details.MaxBytes, loginBodyLimit)
	}
}

func TestHandlerNotificationSettingsAndTestWebhook(t *testing.T) {
	t.Parallel()

	var received struct {
		Event string `json:"event"`
	}
	webhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("webhook method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("X-Stacklab-Event"); got != "test_notification" {
			t.Fatalf("X-Stacklab-Event = %q, want %q", got, "test_notification")
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("Decode(webhook payload) error = %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer webhook.Close()

	handler, _ := newTestHandler(t)
	cookies := loginTestUser(t, handler, "test-password")

	getResponse := performJSONRequest(t, handler, http.MethodGet, "/api/settings/notifications", nil, cookies)
	if getResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/settings/notifications status = %d, want %d; body=%s", getResponse.Code, http.StatusOK, getResponse.Body.String())
	}
	var initial struct {
		Enabled    bool `json:"enabled"`
		Configured bool `json:"configured"`
		Events     struct {
			JobFailed                bool `json:"job_failed"`
			JobSucceededWithWarnings bool `json:"job_succeeded_with_warnings"`
			MaintenanceSucceeded     bool `json:"maintenance_succeeded"`
		} `json:"events"`
	}
	decodeResponse(t, getResponse, &initial)
	if initial.Enabled || initial.Configured || !initial.Events.JobFailed || !initial.Events.JobSucceededWithWarnings || initial.Events.MaintenanceSucceeded {
		t.Fatalf("unexpected initial notification settings: %#v", initial)
	}

	updateBody := map[string]any{
		"enabled":     true,
		"webhook_url": webhook.URL,
		"events": map[string]any{
			"job_failed":                  true,
			"job_succeeded_with_warnings": true,
			"maintenance_succeeded":       true,
		},
	}
	updateResponse := performJSONRequest(t, handler, http.MethodPut, "/api/settings/notifications", updateBody, cookies)
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("PUT /api/settings/notifications status = %d, want %d; body=%s", updateResponse.Code, http.StatusOK, updateResponse.Body.String())
	}

	testResponse := performJSONRequest(t, handler, http.MethodPost, "/api/settings/notifications/test", updateBody, cookies)
	if testResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/settings/notifications/test status = %d, want %d; body=%s", testResponse.Code, http.StatusOK, testResponse.Body.String())
	}
	if received.Event != "test_notification" {
		t.Fatalf("received webhook event = %q, want %q", received.Event, "test_notification")
	}
}

func TestHandlerCreateAndDeleteStackWithoutRuntime(t *testing.T) {
	t.Parallel()

	handler, cfg := newTestHandler(t)
	cookies := loginTestUser(t, handler, "test-password")
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
	if deleteResponse.Code != http.StatusAccepted {
		t.Fatalf("DELETE /api/stacks/%s status = %d, want %d", stackID, deleteResponse.Code, http.StatusAccepted)
	}
	var deletePayload struct {
		Job struct {
			ID     string `json:"id"`
			Action string `json:"action"`
			State  string `json:"state"`
		} `json:"job"`
	}
	decodeResponse(t, deleteResponse, &deletePayload)
	if deletePayload.Job.ID == "" || deletePayload.Job.Action != "remove_stack_definition" || deletePayload.Job.State != "running" {
		t.Fatalf("unexpected delete job payload: %#v", deletePayload.Job)
	}
	waitForTestJobState(t, handler, cookies, deletePayload.Job.ID, "succeeded")

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

func TestHandlerDeleteStackContinuesAfterRequestDisconnect(t *testing.T) {
	t.Parallel()

	handler, cfg := newTestHandler(t)
	cookies := loginTestUser(t, handler, "test-password")
	stackID := "fixture-delete-disconnect"

	createResponse := performJSONRequest(t, handler, http.MethodPost, "/api/stacks", map[string]any{
		"stack_id":            stackID,
		"compose_yaml":        "services:\n  app:\n    image: nginx:alpine\n",
		"env":                 "",
		"create_config_dir":   true,
		"create_data_dir":     true,
		"deploy_after_create": false,
	}, cookies)
	if createResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/stacks status = %d, want %d", createResponse.Code, http.StatusOK)
	}

	body, err := json.Marshal(map[string]any{
		"remove_runtime":    false,
		"remove_definition": true,
		"remove_config":     true,
		"remove_data":       true,
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	request := httptest.NewRequest(http.MethodDelete, "http://stacklab.test/api/stacks/"+stackID, bytes.NewReader(body)).WithContext(requestCtx)
	request.Header.Set("Origin", "http://stacklab.test")
	request.Header.Set("Content-Type", "application/json")
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	recorder := &cancelOnWriteHeaderRecorder{ResponseRecorder: httptest.NewRecorder(), cancel: cancelRequest}

	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("DELETE /api/stacks/%s status = %d, want %d; body=%s", stackID, recorder.Code, http.StatusAccepted, recorder.Body.String())
	}
	if !errors.Is(requestCtx.Err(), context.Canceled) {
		t.Fatalf("request context error = %v, want context.Canceled", requestCtx.Err())
	}
	var payload struct {
		Job struct {
			ID    string `json:"id"`
			State string `json:"state"`
		} `json:"job"`
	}
	decodeResponse(t, recorder.ResponseRecorder, &payload)
	if payload.Job.ID == "" || payload.Job.State != "running" {
		t.Fatalf("unexpected delete job payload: %#v", payload.Job)
	}

	waitForTestJobState(t, handler, cookies, payload.Job.ID, "succeeded")
	assertPathMissing(t, filepath.Join(cfg.RootDir, "stacks", stackID))
	assertPathMissing(t, filepath.Join(cfg.RootDir, "config", stackID))
	assertPathMissing(t, filepath.Join(cfg.RootDir, "data", stackID))
}

type cancelOnWriteHeaderRecorder struct {
	*httptest.ResponseRecorder
	cancel context.CancelFunc
}

func (r *cancelOnWriteHeaderRecorder) WriteHeader(statusCode int) {
	r.cancel()
	r.ResponseRecorder.WriteHeader(statusCode)
}

func TestHandlerShutdownClosesWebSocketsAndWaitsForDisconnect(t *testing.T) {
	handler, _ := newTestHandler(t)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	cookies := loginTestUser(t, handler, "test-password")

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
	wsConn, response, err := websocket.DefaultDialer.Dial(wsURL.String(), header)
	if err != nil {
		if response != nil {
			_ = response.Body.Close()
		}
		t.Fatalf("websocket.Dial() error = %v", err)
	}
	defer wsConn.Close()
	if _, _, err := wsConn.ReadMessage(); err != nil {
		t.Fatalf("read hello frame error = %v", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := handler.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Handler.Shutdown() error = %v", err)
	}
	_ = wsConn.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err := wsConn.ReadMessage(); err == nil {
		t.Fatal("ReadMessage() error = nil after handler shutdown")
	}
}

func TestWebSocketReplaysJobEvents(t *testing.T) {
	t.Parallel()

	handler, _ := newTestHandler(t)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client := server.Client()
	loginRequestBody := bytes.NewBufferString(`{"password":"test-password"}`)
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
	cookies := loginTestUser(t, handler, "test-password")
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

func TestWebSocketClosesWhileInFlightWhenSessionIsRevoked(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		body string
	}{
		{name: "logout", path: "/api/auth/logout", body: `{}`},
		{name: "password change", path: "/api/settings/password", body: `{"current_password":"test-password","new_password":"new-test-password"}`},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			handler, _ := newTestHandler(t)
			server := httptest.NewServer(handler)
			t.Cleanup(server.Close)

			loginRequest, err := http.NewRequest(http.MethodPost, server.URL+"/api/auth/login", bytes.NewBufferString(`{"password":"test-password"}`))
			if err != nil {
				t.Fatalf("http.NewRequest(login) error = %v", err)
			}
			loginRequest.Header.Set("Origin", server.URL)
			loginRequest.Header.Set("Content-Type", "application/json")
			loginResponse, err := server.Client().Do(loginRequest)
			if err != nil {
				t.Fatalf("login error = %v", err)
			}
			cookies := loginResponse.Cookies()
			_ = loginResponse.Body.Close()
			if loginResponse.StatusCode != http.StatusOK || len(cookies) == 0 {
				t.Fatalf("login status = %d, cookies = %#v", loginResponse.StatusCode, cookies)
			}

			wsURL, err := url.Parse(server.URL)
			if err != nil {
				t.Fatalf("url.Parse(server.URL) error = %v", err)
			}
			wsURL.Scheme = strings.Replace(wsURL.Scheme, "http", "ws", 1)
			wsURL.Path = "/api/ws"
			header := http.Header{"Origin": []string{server.URL}}
			for _, cookie := range cookies {
				header.Add("Cookie", cookie.Name+"="+cookie.Value)
			}
			wsConn, response, err := websocket.DefaultDialer.Dial(wsURL.String(), header)
			if err != nil {
				if response != nil {
					_ = response.Body.Close()
				}
				t.Fatalf("websocket dial error = %v", err)
			}
			defer wsConn.Close()
			_ = wsConn.SetReadDeadline(time.Now().Add(5 * time.Second))
			var hello map[string]any
			if err := wsConn.ReadJSON(&hello); err != nil {
				t.Fatalf("ReadJSON(hello) error = %v", err)
			}

			revokeRequest, err := http.NewRequest(http.MethodPost, server.URL+test.path, bytes.NewBufferString(test.body))
			if err != nil {
				t.Fatalf("http.NewRequest(revoke) error = %v", err)
			}
			revokeRequest.Header.Set("Origin", server.URL)
			revokeRequest.Header.Set("Content-Type", "application/json")
			for _, cookie := range cookies {
				revokeRequest.AddCookie(cookie)
			}
			revokeResponse, err := server.Client().Do(revokeRequest)
			if err != nil {
				t.Fatalf("revoke request error = %v", err)
			}
			_ = revokeResponse.Body.Close()
			if revokeResponse.StatusCode != http.StatusOK {
				t.Fatalf("revoke status = %d, want %d", revokeResponse.StatusCode, http.StatusOK)
			}

			_ = wsConn.SetReadDeadline(time.Now().Add(5 * time.Second))
			_, _, err = wsConn.ReadMessage()
			var closeError *websocket.CloseError
			if !errors.As(err, &closeError) {
				t.Fatalf("ReadMessage() error = %v, want websocket close error", err)
			}
			if closeError.Code != websocket.ClosePolicyViolation {
				t.Fatalf("websocket close code = %d, want %d", closeError.Code, websocket.ClosePolicyViolation)
			}
		})
	}
}

func TestWebSocketTerminalAttachMissingSession(t *testing.T) {
	t.Parallel()

	handler, _ := newTestHandler(t)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client := server.Client()
	loginRequestBody := bytes.NewBufferString(`{"password":"test-password"}`)
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

func TestWebSocketLimitsSubscriptionsPerConnection(t *testing.T) {
	t.Parallel()

	handler, _ := newTestHandler(t)
	cookies := loginTestUser(t, handler, "test-password")
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

	readForRequest := func(requestID string) struct {
		Type      string `json:"type"`
		RequestID string `json:"request_id"`
		StreamID  string `json:"stream_id"`
		Error     *struct {
			Code string `json:"code"`
		} `json:"error"`
	} {
		t.Helper()
		for {
			var frame struct {
				Type      string `json:"type"`
				RequestID string `json:"request_id"`
				StreamID  string `json:"stream_id"`
				Error     *struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := wsConn.ReadJSON(&frame); err != nil {
				t.Fatalf("ReadJSON(%s) error = %v", requestID, err)
			}
			if frame.RequestID == requestID {
				return frame
			}
		}
	}

	for i := 0; i < 32; i++ {
		requestID := fmt.Sprintf("req_activity_%d", i)
		if err := wsConn.WriteJSON(map[string]any{
			"type":       "activity.subscribe",
			"request_id": requestID,
			"stream_id":  fmt.Sprintf("activity_%d", i),
		}); err != nil {
			t.Fatalf("WriteJSON(activity %d) error = %v", i, err)
		}
		frame := readForRequest(requestID)
		if frame.Type != "ack" {
			t.Fatalf("activity subscribe %d frame type = %q, want ack: %#v", i, frame.Type, frame)
		}
	}

	if err := wsConn.WriteJSON(map[string]any{
		"type":       "activity.subscribe",
		"request_id": "req_activity_over_limit",
		"stream_id":  "activity_over_limit",
	}); err != nil {
		t.Fatalf("WriteJSON(activity over limit) error = %v", err)
	}
	frame := readForRequest("req_activity_over_limit")
	if frame.Type != "error" || frame.Error == nil || frame.Error.Code != "limit_exceeded" {
		t.Fatalf("over-limit frame = %#v, want limit_exceeded error", frame)
	}
}

func newTestHandler(t *testing.T) (*httpapi.Handler, config.Config) {
	t.Helper()

	tempDir := t.TempDir()
	cfg := config.Config{
		RootDir:                 filepath.Join(tempDir, "root"),
		DataDir:                 filepath.Join(tempDir, "var"),
		DatabasePath:            filepath.Join(tempDir, "var", "stacklab.db"),
		FrontendDistDir:         filepath.Join(tempDir, "frontend"),
		BootstrapPassword:       "test-password",
		SessionCookieName:       "stacklab_session",
		SessionIdleTimeout:      30 * time.Minute,
		SessionAbsoluteLifetime: 24 * time.Hour,
	}
	if err := os.MkdirAll(cfg.FrontendDistDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(frontend dist) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(cfg.FrontendDistDir, "index.html"), []byte("<!doctype html><title>Stacklab</title>"), 0o644); err != nil {
		t.Fatalf("WriteFile(frontend index) error = %v", err)
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
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	auditService := audit.NewService(testStore)
	jobService := jobs.NewService(testStore)
	notificationService := notifications.NewService(testStore, logger)
	stackReader := stacks.NewServiceReader(cfg, logger)
	maintenanceService := maintenance.NewService()
	maintenanceRunner := maintenancejobs.NewService(logger, jobService, auditService, stackReader, maintenanceService)
	schedulerService := scheduler.NewService(testStore, auditService, maintenanceRunner, stackReader, logger)
	selfUpdateService := selfupdate.NewService(cfg, testStore, jobService, auditService, notificationService, logger)
	jobService.SetTerminalHook(notificationService.DispatchJobAsync)

	handler, err := httpapi.NewHandler(cfg, logger, authService, auditService, jobService, notificationService, schedulerService, selfUpdateService)
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := handler.Shutdown(shutdownCtx); err != nil {
			t.Errorf("Handler.Shutdown() error = %v", err)
		}
	})

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

func findTestCookie(t *testing.T, cookies []*http.Cookie, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range cookies {
		if cookie.Name == name && cookie.MaxAge >= 0 {
			return cookie
		}
	}
	t.Fatalf("cookie %q not found in %#v", name, cookies)
	return nil
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

func waitForTestJobState(t *testing.T, handler http.Handler, cookies []*http.Cookie, jobID, wantState string) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	lastState := ""
	for time.Now().Before(deadline) {
		response := performJSONRequest(t, handler, http.MethodGet, "/api/jobs/"+jobID, nil, cookies)
		if response.Code == http.StatusOK {
			var payload struct {
				Job struct {
					State string `json:"state"`
				} `json:"job"`
			}
			decodeResponse(t, response, &payload)
			lastState = payload.Job.State
			if lastState == wantState {
				return
			}
			if lastState == "failed" || lastState == "cancelled" || lastState == "timed_out" {
				t.Fatalf("job %q reached %q, want %q", jobID, lastState, wantState)
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job %q did not reach %q; last state %q", jobID, wantState, lastState)
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
  shift
  case "$*" in
    *"--filter"*)
      exit 0
      ;;
    "-aq")
      echo "container-used"
      exit 0
      ;;
    *)
      exit 0
      ;;
  esac
fi

if [ "$1" = "inspect" ]; then
  echo '[{"Image":"sha256:used","Config":{"Image":"ghcr.io/example/app:latest","Labels":{"com.docker.compose.project":"demo","com.docker.compose.service":"app"}},"Mounts":[{"Name":"demo_data","Type":"volume"}],"NetworkSettings":{"Networks":{"demo_default":{},"external_shared":{}}}}]'
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

if [ "$1" = "system" ] && [ "$2" = "df" ]; then
  echo '{"Active":"1","Reclaimable":"123MB (100%)","Size":"123MB","TotalCount":"2","Type":"Images"}'
  echo '{"Active":"1","Reclaimable":"0B (0%)","Size":"81.9kB","TotalCount":"1","Type":"Containers"}'
  echo '{"Active":"0","Reclaimable":"0B","Size":"0B","TotalCount":"0","Type":"Local Volumes"}'
  echo '{"Active":"0","Reclaimable":"5MB","Size":"5MB","TotalCount":"3","Type":"Build Cache"}'
  exit 0
fi

if [ "$1" = "image" ] && [ "$2" = "ls" ]; then
  echo '{"ID":"sha256:used","Repository":"ghcr.io/example/app","Tag":"latest"}'
  echo '{"ID":"sha256:unused","Repository":"ghcr.io/example/old","Tag":"1.0.0"}'
  exit 0
fi

if [ "$1" = "image" ] && [ "$2" = "inspect" ]; then
  echo '[{"Id":"sha256:used","Created":"2026-04-04T12:11:00Z","Size":1000},{"Id":"sha256:unused","Created":"2026-04-03T12:11:00Z","Size":2000}]'
  exit 0
fi

if [ "$1" = "image" ] && [ "$2" = "prune" ]; then
  shift 2
  append_log "docker image prune $*"
  echo "Deleted Images:"
  exit 0
fi

if [ "$1" = "network" ] && [ "$2" = "ls" ]; then
  echo '{"ID":"network-demo","Name":"demo_default","Driver":"bridge","Scope":"local"}'
  echo '{"ID":"network-ext","Name":"external_shared","Driver":"bridge","Scope":"local"}'
  echo '{"ID":"network-unused","Name":"external_unused","Driver":"bridge","Scope":"local"}'
  exit 0
fi

if [ "$1" = "network" ] && [ "$2" = "inspect" ]; then
  echo '[{"Id":"network-demo","Name":"demo_default","Driver":"bridge","Scope":"local","Internal":false,"Attachable":false,"Ingress":false,"Labels":{"com.docker.compose.project":"demo"}},{"Id":"network-ext","Name":"external_shared","Driver":"bridge","Scope":"local","Internal":false,"Attachable":false,"Ingress":false,"Labels":{}},{"Id":"network-unused","Name":"external_unused","Driver":"bridge","Scope":"local","Internal":false,"Attachable":false,"Ingress":false,"Labels":{}}]'
  exit 0
fi

if [ "$1" = "network" ] && [ "$2" = "create" ]; then
  append_log "docker network create $3"
  echo "$3"
  exit 0
fi

if [ "$1" = "network" ] && [ "$2" = "rm" ]; then
  append_log "docker network rm $3"
  echo "$3"
  exit 0
fi

if [ "$1" = "volume" ] && [ "$2" = "ls" ]; then
  echo '{"Name":"demo_data","Driver":"local"}'
  echo '{"Name":"external_media","Driver":"local"}'
  exit 0
fi

if [ "$1" = "volume" ] && [ "$2" = "inspect" ]; then
  echo '[{"Name":"demo_data","Driver":"local","Mountpoint":"/var/lib/docker/volumes/demo_data/_data","Scope":"local","Labels":{"com.docker.compose.project":"demo"},"Options":{}},{"Name":"external_media","Driver":"local","Mountpoint":"/var/lib/docker/volumes/external_media/_data","Scope":"local","Labels":{},"Options":{"type":"nfs"}}]'
  exit 0
fi

if [ "$1" = "volume" ] && [ "$2" = "create" ]; then
  append_log "docker volume create $3"
  echo "$3"
  exit 0
fi

if [ "$1" = "volume" ] && [ "$2" = "rm" ]; then
  append_log "docker volume rm $3"
  echo "$3"
  exit 0
fi

if [ "$1" = "builder" ] && [ "$2" = "prune" ]; then
  shift 2
  append_log "docker builder prune $*"
  echo "Deleted build cache"
  exit 0
fi

if [ "$1" = "container" ] && [ "$2" = "prune" ]; then
  shift 2
  append_log "docker container prune $*"
  echo "Deleted Containers:"
  exit 0
fi

if [ "$1" = "volume" ] && [ "$2" = "prune" ]; then
  shift 2
  append_log "docker volume prune $*"
  echo "Deleted Volumes:"
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
