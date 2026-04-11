//go:build integration

package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestIntegrationResolvedConfigAndSaveDefinitionWithRealCompose(t *testing.T) {
	requireDockerComposeRuntime(t)

	handler, _ := newTestHandler(t)
	cookies := loginTestUser(t, handler, "secret")
	stackID := uniqueIntegrationStackID("resolved")
	t.Cleanup(func() {
		cleanupIntegrationStack(t, handler, cookies, stackID)
	})

	createResponse := performJSONRequest(t, handler, http.MethodPost, "/api/stacks", map[string]any{
		"stack_id": stackID,
		"compose_yaml": strings.Join([]string{
			"services:",
			"  app:",
			"    image: nginx:${TAG}",
			"",
		}, "\n"),
		"env":                 "TAG=alpine\n",
		"create_config_dir":   false,
		"create_data_dir":     false,
		"deploy_after_create": false,
	}, cookies)
	if createResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/stacks status = %d, want %d; body=%s", createResponse.Code, http.StatusOK, createResponse.Body.String())
	}

	currentResolvedResponse := performJSONRequest(t, handler, http.MethodGet, "/api/stacks/"+stackID+"/resolved-config?source=current", nil, cookies)
	if currentResolvedResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/stacks/%s/resolved-config status = %d, want %d; body=%s", stackID, currentResolvedResponse.Code, http.StatusOK, currentResolvedResponse.Body.String())
	}
	var currentResolvedPayload struct {
		StackID string `json:"stack_id"`
		Valid   bool   `json:"valid"`
		Content string `json:"content"`
	}
	decodeResponse(t, currentResolvedResponse, &currentResolvedPayload)
	if !currentResolvedPayload.Valid {
		t.Fatalf("expected resolved current config to be valid: %#v", currentResolvedPayload)
	}
	if !strings.Contains(currentResolvedPayload.Content, "image: nginx:alpine") {
		t.Fatalf("resolved current config did not contain expected image: %q", currentResolvedPayload.Content)
	}

	draftResolvedResponse := performJSONRequest(t, handler, http.MethodPost, "/api/stacks/"+stackID+"/resolved-config", map[string]any{
		"compose_yaml": strings.Join([]string{
			"services:",
			"  app:",
			"    image: nginx:${TAG}",
			"",
		}, "\n"),
		"env": "TAG=stable-alpine3.21\n",
	}, cookies)
	if draftResolvedResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/stacks/%s/resolved-config status = %d, want %d; body=%s", stackID, draftResolvedResponse.Code, http.StatusOK, draftResolvedResponse.Body.String())
	}
	var draftResolvedPayload struct {
		Valid   bool   `json:"valid"`
		Content string `json:"content"`
	}
	decodeResponse(t, draftResolvedResponse, &draftResolvedPayload)
	if !draftResolvedPayload.Valid {
		t.Fatalf("expected draft resolved config to be valid: %#v", draftResolvedPayload)
	}
	if !strings.Contains(draftResolvedPayload.Content, "image: nginx:stable-alpine3.21") {
		t.Fatalf("resolved draft config did not contain expected image: %q", draftResolvedPayload.Content)
	}

	saveResponse := performJSONRequest(t, handler, http.MethodPut, "/api/stacks/"+stackID+"/definition", map[string]any{
		"compose_yaml": strings.Join([]string{
			"services:",
			"  app:",
			"    image: nginx:${TAG}",
			"",
		}, "\n"),
		"env":                 "TAG=stable-alpine3.21\n",
		"validate_after_save": true,
	}, cookies)
	if saveResponse.Code != http.StatusOK {
		t.Fatalf("PUT /api/stacks/%s/definition status = %d, want %d; body=%s", stackID, saveResponse.Code, http.StatusOK, saveResponse.Body.String())
	}
	var savePayload struct {
		Job struct {
			Action string `json:"action"`
			State  string `json:"state"`
		} `json:"job"`
	}
	decodeResponse(t, saveResponse, &savePayload)
	if savePayload.Job.Action != "save_definition" || savePayload.Job.State != "succeeded" {
		t.Fatalf("unexpected save job payload: %#v", savePayload.Job)
	}

	definitionResponse := performJSONRequest(t, handler, http.MethodGet, "/api/stacks/"+stackID+"/definition", nil, cookies)
	if definitionResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/stacks/%s/definition status = %d, want %d; body=%s", stackID, definitionResponse.Code, http.StatusOK, definitionResponse.Body.String())
	}
	var definitionPayload struct {
		Files struct {
			Env struct {
				Content string `json:"content"`
				Exists  bool   `json:"exists"`
			} `json:"env"`
		} `json:"files"`
	}
	decodeResponse(t, definitionResponse, &definitionPayload)
	if !definitionPayload.Files.Env.Exists || definitionPayload.Files.Env.Content != "TAG=stable-alpine3.21\n" {
		t.Fatalf("unexpected env definition payload: %#v", definitionPayload.Files.Env)
	}

	updatedResolvedResponse := performJSONRequest(t, handler, http.MethodGet, "/api/stacks/"+stackID+"/resolved-config?source=current", nil, cookies)
	if updatedResolvedResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/stacks/%s/resolved-config after save status = %d, want %d; body=%s", stackID, updatedResolvedResponse.Code, http.StatusOK, updatedResolvedResponse.Body.String())
	}
	var updatedResolvedPayload struct {
		Valid   bool   `json:"valid"`
		Content string `json:"content"`
	}
	decodeResponse(t, updatedResolvedResponse, &updatedResolvedPayload)
	if !updatedResolvedPayload.Valid {
		t.Fatalf("expected resolved current config after save to be valid: %#v", updatedResolvedPayload)
	}
	if !strings.Contains(updatedResolvedPayload.Content, "image: nginx:stable-alpine3.21") {
		t.Fatalf("resolved current config after save did not contain updated image: %q", updatedResolvedPayload.Content)
	}
}

func TestIntegrationComposeLifecycleActionsWithRealDocker(t *testing.T) {
	requireDockerComposeRuntime(t)

	handler, _ := newTestHandler(t)
	cookies := loginTestUser(t, handler, "secret")
	stackID := uniqueIntegrationStackID("lifecycle")
	t.Cleanup(func() {
		cleanupIntegrationStack(t, handler, cookies, stackID)
	})

	createResponse := performJSONRequest(t, handler, http.MethodPost, "/api/stacks", map[string]any{
		"stack_id": stackID,
		"compose_yaml": strings.Join([]string{
			"services:",
			"  app:",
			"    image: busybox:1.36",
			"    command: [\"sh\", \"-c\", \"while true; do echo lifecycle; sleep 2; done\"]",
			"",
		}, "\n"),
		"env":                 "",
		"create_config_dir":   false,
		"create_data_dir":     false,
		"deploy_after_create": false,
	}, cookies)
	if createResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/stacks status = %d, want %d; body=%s", createResponse.Code, http.StatusOK, createResponse.Body.String())
	}

	detail := getIntegrationStackDetail(t, handler, cookies, stackID)
	if detail.Stack.RuntimeState != "defined" {
		t.Fatalf("runtime_state after create = %q, want %q", detail.Stack.RuntimeState, "defined")
	}
	if !containsString(detail.Stack.AvailableActions, "up") {
		t.Fatalf("expected available actions to include up, got %#v", detail.Stack.AvailableActions)
	}

	upResponse := performJSONRequest(t, handler, http.MethodPost, "/api/stacks/"+stackID+"/actions/up", map[string]any{}, cookies)
	if upResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/stacks/%s/actions/up status = %d, want %d; body=%s", stackID, upResponse.Code, http.StatusOK, upResponse.Body.String())
	}
	waitForIntegrationJobSucceeded(t, handler, cookies, assertIntegrationJobStarted(t, upResponse, "up"))
	waitForIntegrationStackRuntimeState(t, handler, cookies, stackID, "running")

	runningDetail := getIntegrationStackDetail(t, handler, cookies, stackID)
	if !containsString(runningDetail.Stack.AvailableActions, "restart") || !containsString(runningDetail.Stack.AvailableActions, "down") {
		t.Fatalf("expected running stack available actions to include restart/down, got %#v", runningDetail.Stack.AvailableActions)
	}

	restartResponse := performJSONRequest(t, handler, http.MethodPost, "/api/stacks/"+stackID+"/actions/restart", map[string]any{}, cookies)
	if restartResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/stacks/%s/actions/restart status = %d, want %d; body=%s", stackID, restartResponse.Code, http.StatusOK, restartResponse.Body.String())
	}
	waitForIntegrationJobSucceeded(t, handler, cookies, assertIntegrationJobStarted(t, restartResponse, "restart"))
	waitForIntegrationStackRuntimeState(t, handler, cookies, stackID, "running")

	downResponse := performJSONRequest(t, handler, http.MethodPost, "/api/stacks/"+stackID+"/actions/down", map[string]any{}, cookies)
	if downResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/stacks/%s/actions/down status = %d, want %d; body=%s", stackID, downResponse.Code, http.StatusOK, downResponse.Body.String())
	}
	waitForIntegrationJobSucceeded(t, handler, cookies, assertIntegrationJobStarted(t, downResponse, "down"))
	waitForIntegrationStackRuntimeState(t, handler, cookies, stackID, "defined")
}

func TestIntegrationMaintenanceUpdateStacksWithRealDocker(t *testing.T) {
	requireDockerComposeRuntime(t)

	handler, _ := newTestHandler(t)
	cookies := loginTestUser(t, handler, "secret")
	stackID := uniqueIntegrationStackID("maint")
	t.Cleanup(func() {
		cleanupIntegrationStack(t, handler, cookies, stackID)
	})

	createResponse := performJSONRequest(t, handler, http.MethodPost, "/api/stacks", map[string]any{
		"stack_id": stackID,
		"compose_yaml": strings.Join([]string{
			"services:",
			"  app:",
			"    image: busybox:1.36",
			"    command: [\"sh\", \"-c\", \"while true; do echo maintenance; sleep 2; done\"]",
			"",
		}, "\n"),
		"env":                 "",
		"create_config_dir":   false,
		"create_data_dir":     false,
		"deploy_after_create": false,
	}, cookies)
	if createResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/stacks status = %d, want %d; body=%s", createResponse.Code, http.StatusOK, createResponse.Body.String())
	}

	maintenanceResponse := performJSONRequest(t, handler, http.MethodPost, "/api/maintenance/update-stacks", map[string]any{
		"target": map[string]any{
			"mode":      "selected",
			"stack_ids": []string{stackID},
		},
		"options": map[string]any{
			"pull_images":    true,
			"build_images":   true,
			"remove_orphans": true,
			"prune_after": map[string]any{
				"enabled":         false,
				"include_volumes": false,
			},
		},
	}, cookies)
	if maintenanceResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/maintenance/update-stacks status = %d, want %d; body=%s", maintenanceResponse.Code, http.StatusOK, maintenanceResponse.Body.String())
	}

	var payload struct {
		Job struct {
			StackID  *string `json:"stack_id"`
			Action   string  `json:"action"`
			State    string  `json:"state"`
			Workflow *struct {
				Steps []struct {
					Action        string `json:"action"`
					TargetStackID string `json:"target_stack_id"`
				} `json:"steps"`
			} `json:"workflow"`
		} `json:"job"`
	}
	decodeResponse(t, maintenanceResponse, &payload)
	if payload.Job.StackID != nil || payload.Job.Action != "update_stacks" || payload.Job.State != "succeeded" {
		t.Fatalf("unexpected maintenance job payload: %#v", payload.Job)
	}
	if payload.Job.Workflow == nil || len(payload.Job.Workflow.Steps) != 2 {
		t.Fatalf("unexpected maintenance workflow: %#v", payload.Job.Workflow)
	}
	if payload.Job.Workflow.Steps[0].Action != "pull" || payload.Job.Workflow.Steps[0].TargetStackID != stackID {
		t.Fatalf("unexpected first maintenance step: %#v", payload.Job.Workflow.Steps[0])
	}
	waitForIntegrationStackRuntimeState(t, handler, cookies, stackID, "running")
}

func TestIntegrationCreateDeployAndOrphanedLifecycleWithRealDocker(t *testing.T) {
	requireDockerComposeRuntime(t)

	handler, _ := newTestHandler(t)
	cookies := loginTestUser(t, handler, "secret")
	stackID := uniqueIntegrationStackID("orphaned")
	t.Cleanup(func() {
		cleanupIntegrationStack(t, handler, cookies, stackID)
	})

	createResponse := performJSONRequest(t, handler, http.MethodPost, "/api/stacks", map[string]any{
		"stack_id": stackID,
		"compose_yaml": strings.Join([]string{
			"services:",
			"  app:",
			"    image: busybox:1.36",
			"    command: [\"sh\", \"-c\", \"while true; do echo orphaned; sleep 2; done\"]",
			"",
		}, "\n"),
		"env":                 "",
		"create_config_dir":   false,
		"create_data_dir":     false,
		"deploy_after_create": true,
	}, cookies)
	if createResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/stacks deploy_after_create status = %d, want %d; body=%s", createResponse.Code, http.StatusOK, createResponse.Body.String())
	}
	var createPayload struct {
		Job struct {
			Action   string `json:"action"`
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
	if createPayload.Job.Action != "create_stack" || createPayload.Job.State != "succeeded" {
		t.Fatalf("unexpected create job payload: %#v", createPayload.Job)
	}
	if createPayload.Job.Workflow == nil || len(createPayload.Job.Workflow.Steps) != 2 {
		t.Fatalf("expected two-step create workflow, got %#v", createPayload.Job.Workflow)
	}
	waitForIntegrationStackRuntimeState(t, handler, cookies, stackID, "running")

	removeDefinitionResponse := performJSONRequest(t, handler, http.MethodDelete, "/api/stacks/"+stackID, map[string]any{
		"remove_runtime":    false,
		"remove_definition": true,
		"remove_config":     false,
		"remove_data":       false,
	}, cookies)
	if removeDefinitionResponse.Code != http.StatusOK {
		t.Fatalf("DELETE remove_definition-only status = %d, want %d; body=%s", removeDefinitionResponse.Code, http.StatusOK, removeDefinitionResponse.Body.String())
	}
	assertIntegrationJobSucceeded(t, removeDefinitionResponse, "remove_stack_definition")

	orphanedDetail := waitForIntegrationStackRuntimeState(t, handler, cookies, stackID, "orphaned")
	if orphanedDetail.Stack.Capabilities.CanEditDefinition {
		t.Fatalf("expected orphaned stack to disable editing, got capabilities %#v", orphanedDetail.Stack.Capabilities)
	}
	if len(orphanedDetail.Stack.AvailableActions) != 1 || orphanedDetail.Stack.AvailableActions[0] != "down" {
		t.Fatalf("expected orphaned stack available actions to be [down], got %#v", orphanedDetail.Stack.AvailableActions)
	}

	listResponse := performJSONRequest(t, handler, http.MethodGet, "/api/stacks", nil, cookies)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/stacks status = %d, want %d; body=%s", listResponse.Code, http.StatusOK, listResponse.Body.String())
	}
	var listPayload struct {
		Summary struct {
			OrphanedCount int `json:"orphaned_count"`
		} `json:"summary"`
	}
	decodeResponse(t, listResponse, &listPayload)
	if listPayload.Summary.OrphanedCount < 1 {
		t.Fatalf("expected orphaned_count >= 1, got %#v", listPayload.Summary)
	}

	removeRuntimeResponse := performJSONRequest(t, handler, http.MethodDelete, "/api/stacks/"+stackID, map[string]any{
		"remove_runtime":    true,
		"remove_definition": false,
		"remove_config":     false,
		"remove_data":       false,
	}, cookies)
	if removeRuntimeResponse.Code != http.StatusOK {
		t.Fatalf("DELETE remove_runtime-only status = %d, want %d; body=%s", removeRuntimeResponse.Code, http.StatusOK, removeRuntimeResponse.Body.String())
	}
	assertIntegrationJobSucceeded(t, removeRuntimeResponse, "remove_stack_definition")
	waitForIntegrationStackAbsent(t, handler, cookies, stackID)
}

type integrationStackDetailResponse struct {
	Stack struct {
		ID               string   `json:"id"`
		RuntimeState     string   `json:"runtime_state"`
		ConfigState      string   `json:"config_state"`
		AvailableActions []string `json:"available_actions"`
		Capabilities     struct {
			CanEditDefinition bool `json:"can_edit_definition"`
		} `json:"capabilities"`
	} `json:"stack"`
}

func requireDockerComposeRuntime(t *testing.T) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dockerVersion := exec.CommandContext(ctx, "docker", "version", "--format", "{{.Server.Version}}")
	if output, err := dockerVersion.CombinedOutput(); err != nil {
		t.Skipf("docker runtime unavailable: %v (%s)", err, strings.TrimSpace(string(output)))
	}

	for _, candidate := range []struct {
		command string
		args    []string
	}{
		{command: "docker", args: []string{"compose", "version"}},
		{command: "docker-compose", args: []string{"version"}},
	} {
		cmd := exec.CommandContext(ctx, candidate.command, candidate.args...)
		if err := cmd.Run(); err == nil {
			return
		}
	}

	t.Skip("docker compose runtime unavailable")
}

func uniqueIntegrationStackID(prefix string) string {
	return fmt.Sprintf("itest-%s-%d", prefix, time.Now().UnixNano())
}

func cleanupIntegrationStack(t *testing.T, handler http.Handler, cookies []*http.Cookie, stackID string) {
	t.Helper()

	response := performJSONRequest(t, handler, http.MethodDelete, "/api/stacks/"+stackID, map[string]any{
		"remove_runtime":    true,
		"remove_definition": true,
		"remove_config":     true,
		"remove_data":       true,
	}, cookies)
	if response.Code == http.StatusOK || response.Code == http.StatusNotFound {
		return
	}

	t.Logf("cleanup stack %q returned status %d: %s", stackID, response.Code, response.Body.String())
}

func getIntegrationStackDetail(t *testing.T, handler http.Handler, cookies []*http.Cookie, stackID string) integrationStackDetailResponse {
	t.Helper()

	response := performJSONRequest(t, handler, http.MethodGet, "/api/stacks/"+stackID, nil, cookies)
	if response.Code != http.StatusOK {
		t.Fatalf("GET /api/stacks/%s status = %d, want %d; body=%s", stackID, response.Code, http.StatusOK, response.Body.String())
	}

	var payload integrationStackDetailResponse
	decodeResponse(t, response, &payload)
	return payload
}

func waitForIntegrationStackRuntimeState(t *testing.T, handler http.Handler, cookies []*http.Cookie, stackID, want string) integrationStackDetailResponse {
	t.Helper()

	deadline := time.Now().Add(60 * time.Second)
	var lastState string
	for time.Now().Before(deadline) {
		response := performJSONRequest(t, handler, http.MethodGet, "/api/stacks/"+stackID, nil, cookies)
		if response.Code == http.StatusOK {
			var payload integrationStackDetailResponse
			decodeResponse(t, response, &payload)
			lastState = payload.Stack.RuntimeState
			if payload.Stack.RuntimeState == want {
				return payload
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

	t.Fatalf("stack %q did not reach runtime_state %q before deadline; last_state=%q", stackID, want, lastState)
	return integrationStackDetailResponse{}
}

func waitForIntegrationStackAbsent(t *testing.T, handler http.Handler, cookies []*http.Cookie, stackID string) {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		response := performJSONRequest(t, handler, http.MethodGet, "/api/stacks/"+stackID, nil, cookies)
		if response.Code == http.StatusNotFound {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}

	t.Fatalf("stack %q still present after deadline", stackID)
}

func assertIntegrationJobStarted(t *testing.T, response *httptest.ResponseRecorder, wantAction string) string {
	t.Helper()

	var payload struct {
		Job struct {
			ID     string `json:"id"`
			Action string `json:"action"`
			State  string `json:"state"`
		} `json:"job"`
	}
	decodeResponse(t, response, &payload)
	if payload.Job.Action != wantAction || payload.Job.State != "running" || payload.Job.ID == "" {
		t.Fatalf("unexpected job payload: %#v", payload.Job)
	}
	return payload.Job.ID
}

func assertIntegrationJobSucceeded(t *testing.T, response *httptest.ResponseRecorder, wantAction string) {
	t.Helper()

	var payload struct {
		Job struct {
			Action string `json:"action"`
			State  string `json:"state"`
		} `json:"job"`
	}
	decodeResponse(t, response, &payload)
	if payload.Job.Action != wantAction || payload.Job.State != "succeeded" {
		t.Fatalf("unexpected job payload: %#v", payload.Job)
	}
}

func waitForIntegrationJobSucceeded(t *testing.T, handler http.Handler, cookies []*http.Cookie, jobID string) {
	t.Helper()

	deadline := time.Now().Add(60 * time.Second)
	var lastState string
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
			if payload.Job.State == "succeeded" {
				return
			}
			if payload.Job.State == "failed" || payload.Job.State == "cancelled" || payload.Job.State == "timed_out" {
				t.Fatalf("job %q finished in terminal state %q", jobID, payload.Job.State)
			}
		}
		time.Sleep(250 * time.Millisecond)
	}

	t.Fatalf("job %q did not reach succeeded before deadline; last_state=%q", jobID, lastState)
}
