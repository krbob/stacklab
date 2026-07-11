package httpapi

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"stacklab/internal/requestid"
)

func TestRequestIDIsEchoedInResponseContextAndLog(t *testing.T) {
	t.Parallel()

	var logOutput bytes.Buffer
	handler := &Handler{logger: slog.New(slog.NewJSONHandler(&logOutput, nil))}
	const supplied = "edge-01:req_123"
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := requestid.FromContext(r.Context()); got != supplied {
			t.Fatalf("request ID in context = %q, want %q", got, supplied)
		}
		w.WriteHeader(http.StatusAccepted)
	})
	served := handler.withRequestID(handler.withLogging(next))

	request := httptest.NewRequest(http.MethodPost, "http://stacklab.test/api/stacks/demo/up", nil)
	request.Header.Set(requestid.Header, supplied)
	response := httptest.NewRecorder()
	served.ServeHTTP(response, request)

	if got := response.Header().Get(requestid.Header); got != supplied {
		t.Fatalf("response %s = %q, want %q", requestid.Header, got, supplied)
	}
	var record map[string]any
	if err := json.Unmarshal(logOutput.Bytes(), &record); err != nil {
		t.Fatalf("decode request log: %v; output = %q", err, logOutput.String())
	}
	if got := record["request_id"]; got != supplied {
		t.Fatalf("logged request_id = %#v, want %q", got, supplied)
	}
	if got := record["status"]; got != float64(http.StatusAccepted) {
		t.Fatalf("logged status = %#v, want %d", got, http.StatusAccepted)
	}
}

func TestRequestIDMiddlewareReplacesUnsafeHeader(t *testing.T) {
	t.Parallel()

	handler := &Handler{}
	served := handler.withRequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requestid.FromContext(r.Context()) == "unsafe value" {
			t.Fatal("unsafe request ID reached context")
		}
	}))
	request := httptest.NewRequest(http.MethodGet, "http://stacklab.test/api/live", nil)
	request.Header.Set(requestid.Header, "unsafe value")
	response := httptest.NewRecorder()

	served.ServeHTTP(response, request)

	generated := response.Header().Get(requestid.Header)
	if generated == "" || generated == "unsafe value" {
		t.Fatalf("generated request ID = %q", generated)
	}
}
