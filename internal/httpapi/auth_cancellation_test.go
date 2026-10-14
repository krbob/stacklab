package httpapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSessionCancellationDoesNotEmitServiceFailure(t *testing.T) {
	for _, tc := range []struct {
		name    string
		err     error
		failure bool
	}{
		{"cancelled", context.Canceled, false},
		{"wrapped", fmt.Errorf("lookup: %w", context.Canceled), false},
		{"database", errors.New("database unavailable"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			h := &Handler{logger: slog.New(slog.NewTextHandler(&output, nil))}
			response := httptest.NewRecorder()
			h.writeSessionAuthenticationError(response, tc.err)
			logged := strings.Contains(output.String(), "level=ERROR")
			if logged != tc.failure {
				t.Fatalf("unexpected failure log: %s", output.String())
			}
			if response.Code != 500 {
				t.Fatalf("status=%d", response.Code)
			}
		})
	}
}
