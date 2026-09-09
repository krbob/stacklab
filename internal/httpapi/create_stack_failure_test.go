package httpapi_test

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"stacklab/internal/store"
)

func TestCreateStackFailureRetainsCauseAndFinishesWorkflow(t *testing.T) {
	for _, scenario := range []string{"missing-template", "data-permission"} {
		t.Run(scenario, func(t *testing.T) {
			if scenario == "data-permission" && os.Geteuid() == 0 {
				t.Skip("root bypasses directory write permissions")
			}
			handler, cfg := newTestHandler(t)
			cookies := loginTestUser(t, handler, "test-password")
			body := map[string]any{
				"stack_id": "monitoring", "compose_yaml": "services:\n  app:\n    image: alpine:3.22\n",
				"create_config_dir": true, "create_data_dir": true, "deploy_after_create": true,
			}
			wantStatus, wantCause := http.StatusNotFound, "not found"
			if scenario == "missing-template" {
				body["template_id"] = "does-not-exist"
			} else {
				dataDir := filepath.Join(cfg.RootDir, "data")
				if err := os.MkdirAll(dataDir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(dataDir, 0o555); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(dataDir, 0o755) })
				wantStatus, wantCause = http.StatusInternalServerError, "permission denied"
			}
			response := performJSONRequest(t, handler, http.MethodPost, "/api/stacks", body, cookies)
			if response.Code != wantStatus {
				t.Fatalf("create status = %d, want %d: %s", response.Code, wantStatus, response.Body.String())
			}
			var failure struct {
				Error struct {
					Details struct {
						JobID string `json:"job_id"`
					} `json:"details"`
				} `json:"error"`
			}
			decodeResponse(t, response, &failure)
			jobID := failure.Error.Details.JobID
			if jobID == "" {
				t.Fatal("create failure must link to its retained job")
			}
			var detail struct {
				Job store.Job `json:"job"`
			}
			decodeResponse(t, performJSONRequest(t, handler, http.MethodGet, "/api/jobs/"+jobID, nil, cookies), &detail)
			if detail.Job.State != "failed" || detail.Job.FinishedAt == nil {
				t.Fatalf("job not finished: %+v", detail.Job)
			}
			if detail.Job.Workflow == nil || len(detail.Job.Workflow.Steps) != 2 {
				t.Fatalf("missing workflow: %+v", detail.Job.Workflow)
			}
			if detail.Job.Workflow.Steps[0].State != "failed" || detail.Job.Workflow.Steps[1].State != "skipped" {
				t.Fatalf("unexpected workflow: %+v", detail.Job.Workflow)
			}
			var history struct {
				Items []store.JobEvent `json:"items"`
			}
			decodeResponse(t, performJSONRequest(t, handler, http.MethodGet, "/api/jobs/"+jobID+"/events", nil, cookies), &history)
			var closedStep, foundCause bool
			for _, event := range history.Items {
				closedStep = closedStep || event.Event == "job_step_finished" && event.Step != nil && event.Step.State == "failed"
				foundCause = foundCause || event.Event == "job_error" && strings.Contains(event.Message, wantCause)
			}
			if !closedStep || !foundCause {
				t.Fatalf("incomplete failure history: %+v", history.Items)
			}
			var audit struct {
				Items []store.AuditEntry `json:"items"`
			}
			decodeResponse(t, performJSONRequest(t, handler, http.MethodGet, "/api/audit?stack_id=monitoring", nil, cookies), &audit)
			if len(audit.Items) != 1 || audit.Items[0].Result != "failed" {
				t.Fatalf("unexpected audit: %+v", audit.Items)
			}
			for _, category := range []string{"stacks", "config", "data"} {
				assertPathMissing(t, filepath.Join(cfg.RootDir, category, "monitoring"))
			}
		})
	}
}
