package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"stacklab/internal/gitworkspace"
)

type failingGitPushReader struct {
	GitWorkspaceReader
	err error
}

func (reader failingGitPushReader) Push(context.Context) (gitworkspace.PushResponse, error) {
	return gitworkspace.PushResponse{}, reader.err
}

func TestGitPushErrorsExplainServiceAccountRemediation(t *testing.T) {
	handler, served, _ := newInternalTestHandler(t)
	cookies := loginInternalTestUser(t, served, "test-password")
	for _, tc := range []struct {
		err     error
		status  int
		code    string
		message string
	}{
		{gitworkspace.ErrAuthFailed, http.StatusBadGateway, "git_auth_failed", "Stacklab service account"},
		{gitworkspace.ErrHostKeyFailed, http.StatusBadGateway, "git_host_key_failed", "known_hosts"},
		{gitworkspace.ErrPermissionDenied, http.StatusConflict, "permission_denied", "ownership and permissions"},
		{gitworkspace.ErrPushRejected, http.StatusConflict, "push_rejected", "Remote rejected"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			handler.gitStatus = failingGitPushReader{err: tc.err}
			response := performInternalJSONRequest(t, served, http.MethodPost, "/api/git/workspace/push", nil, cookies)
			if response.Code != tc.status {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, tc.status, response.Body.String())
			}
			var payload struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if payload.Error.Code != tc.code || !strings.Contains(payload.Error.Message, tc.message) {
				t.Fatalf("unexpected push error: %s", response.Body.String())
			}
		})
	}
}
