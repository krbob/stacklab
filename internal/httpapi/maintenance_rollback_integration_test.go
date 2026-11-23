//go:build integration

package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"stacklab/internal/stacks"
	"stacklab/internal/store"
)

func TestIntegrationMaintenanceRollsBackPartialUp(t *testing.T) {
	requireDockerComposeRuntime(t)
	handler, cfg := newTestHandler(t)
	cookies := loginTestUser(t, handler, "test-password")
	stackID := uniqueIntegrationStackID("rollback")
	candidate := "stacklab.local/" + stackID + ":candidate"
	broken := "stacklab.local/" + stackID + ":broken"
	docker := func(args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()
		out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("docker %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	// Commit a fixture with an unusable entrypoint; the normal Compose up must
	// actually replace the container before the replacement fails to start.
	fixture := docker("create", "--name", stackID+"-fixture", "busybox:1.36", "true")
	t.Cleanup(func() { _ = exec.Command("docker", "rm", fixture).Run() })
	docker("image", "tag", "busybox:1.36", candidate)
	t.Cleanup(func() { _ = exec.Command("docker", "image", "rm", candidate).Run() })
	docker("commit", "--change", `ENTRYPOINT ["/missing-test-entrypoint"]`, fixture, broken)
	t.Cleanup(func() { _ = exec.Command("docker", "image", "rm", broken).Run() })
	definition := fmt.Sprintf(`services:
  app:
    image: %s
    pull_policy: never
    command: [sleep, '86400']
    stop_grace_period: 1s
    healthcheck:
      test: [CMD, 'true']
      interval: 1s
    volumes: [witness:/state]
  observer:
    image: busybox:1.36
    pull_policy: never
    command: [sleep, '86400']
    stop_grace_period: 1s
volumes:
  witness: {}
`, candidate)
	created := performJSONRequest(t, handler, http.MethodPost, "/api/stacks", map[string]any{"stack_id": stackID, "compose_yaml": definition, "env": "", "deploy_after_create": false}, cookies)
	if created.Code != http.StatusOK {
		t.Fatalf("create stack: %d %s", created.Code, created.Body.String())
	}
	t.Cleanup(func() {
		// Stop any unfinished update before removing this test project's volumes
		// and runtime, including when an assertion fails during deployment.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := handler.Shutdown(ctx); err != nil {
			t.Errorf("stop test handler: %v", err)
		}
		composeFile := filepath.Join(cfg.RootDir, "stacks", stackID, "compose.yaml")
		cmd := exec.CommandContext(ctx, "docker", "compose", "-p", stackID, "-f", composeFile, "down", "--volumes", "--remove-orphans")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("remove test project: %v: %s", err, out)
		}
	})
	up := performJSONRequest(t, handler, http.MethodPost, "/api/stacks/"+stackID+"/actions/up", map[string]any{}, cookies)
	if up.Code != http.StatusOK {
		t.Fatalf("start stack: %d %s", up.Code, up.Body.String())
	}
	waitForIntegrationJobSucceeded(t, handler, cookies, assertIntegrationJobStarted(t, up, "up"))
	instance := func(service string) string {
		return docker("ps", "-q", "--filter", "label=com.docker.compose.project="+stackID, "--filter", "label=com.docker.compose.service="+service)
	}
	app, observer := instance("app"), instance("observer")
	if app == "" || observer == "" {
		t.Fatal("missing fixture service")
	}
	oldID := docker("inspect", "--format", "{{.Image}}", app)
	for deadline := time.Now().Add(15 * time.Second); docker("inspect", "--format", "{{.State.Health.Status}}", app) != "healthy"; {
		if time.Now().After(deadline) {
			t.Fatal("baseline did not become healthy")
		}
		time.Sleep(200 * time.Millisecond)
	}
	docker("exec", app, "sh", "-c", "printf 'before-update' > /state/witness")
	policyURL := "/api/stacks/" + stackID + "/update-protection"
	for i, method := range []string{http.MethodGet, http.MethodPut, http.MethodGet} {
		var body any
		if method == http.MethodPut {
			body = map[string]any{"auto_rollback": true}
		}
		response := performJSONRequest(t, handler, method, policyURL, body, cookies)
		if response.Code != http.StatusOK {
			t.Fatalf("%s policy: %d %s", method, response.Code, response.Body.String())
		}
		var policy struct {
			AutoRollback bool `json:"auto_rollback"`
		}
		decodeResponse(t, response, &policy)
		if policy.AutoRollback != (i > 0) {
			t.Fatalf("%s auto_rollback = %t", method, policy.AutoRollback)
		}
	}
	docker("image", "tag", broken, candidate)
	if id := docker("image", "inspect", "--format", "{{.Id}}", candidate); id == oldID {
		t.Fatal("test candidate still points to old image")
	}
	updateRequest := map[string]any{
		"target": map[string]any{
			"mode":              "selected",
			"stack_ids":         []string{stackID},
			"excluded_services": map[string][]string{stackID: {"observer"}},
		},
		"options": map[string]any{"pull_images": false, "build_images": false},
	}
	update := performJSONRequest(t, handler, http.MethodPost, "/api/maintenance/update-stacks", updateRequest, cookies)
	if update.Code != http.StatusOK {
		t.Fatalf("start maintenance: %d %s", update.Code, update.Body.String())
	}
	jobID := assertIntegrationJobStarted(t, update, "update_stacks")
	var status struct {
		Job store.Job `json:"job"`
	}
	for deadline := time.Now().Add(60 * time.Second); ; {
		response := performJSONRequest(t, handler, http.MethodGet, "/api/jobs/"+jobID, nil, cookies)
		decodeResponse(t, response, &status)
		if status.Job.State == "failed" {
			break
		}
		if status.Job.State != "running" || time.Now().After(deadline) {
			t.Fatalf("maintenance did not finish as failed: %+v", status.Job)
		}
		time.Sleep(250 * time.Millisecond)
	}
	states := map[string]string{}
	if status.Job.Workflow != nil {
		for _, step := range status.Job.Workflow.Steps {
			states[step.Action] = step.State
		}
	}
	if states["verify_before"] != "succeeded" || states["prepare_rollback"] != "succeeded" || states["up"] != "failed" || states["rollback_images"] != "succeeded" {
		t.Fatalf("unexpected recovery steps: %+v", states)
	}
	restored := instance("app")
	if restored == "" || restored == app {
		t.Fatalf("expected a recreated container, old=%s new=%s", app, restored)
	}
	if got := docker("inspect", "--format", "{{.Image}} {{.State.Health.Status}}", restored); got != oldID+" healthy" {
		t.Fatalf("recovered image/health = %s", got)
	}
	if got := docker("exec", restored, "cat", "/state/witness"); got != "before-update" {
		t.Fatalf("persisted volume changed: %q", got)
	}
	if got := instance("observer"); got != observer {
		t.Fatalf("excluded service replaced: %s -> %s", observer, got)
	}
	dashboard := func() stacks.StackListItem {
		t.Helper()
		response := performJSONRequest(t, handler, http.MethodGet, "/api/stacks", nil, cookies)
		if response.Code != http.StatusOK {
			t.Fatalf("list stacks: %d %s", response.Code, response.Body.String())
		}
		var list stacks.StackListResponse
		decodeResponse(t, response, &list)
		for _, item := range list.Items {
			if item.ID == stackID {
				return item
			}
		}
		t.Fatalf("dashboard is missing stack %s", stackID)
		return stacks.StackListItem{}
	}
	warnings := dashboard().UpdateWarnings
	if len(warnings) != 1 || warnings[0].ServiceName != "app" || warnings[0].Outcome != "rolled_back" || warnings[0].JobID != jobID {
		t.Fatalf("dashboard warnings after rollback: %+v", warnings)
	}
	name := strings.TrimPrefix(docker("inspect", "--format", "{{.Name}}", restored), "/")
	if len(warnings[0].ContainerNames) != 1 || warnings[0].ContainerNames[0] != name {
		t.Fatalf("wrong affected containers: %+v, expected %q", warnings, name)
	}

	docker("commit", "--change", "LABEL fixture=fixed", fixture, candidate)
	retry := performJSONRequest(t, handler, http.MethodPost, "/api/maintenance/update-stacks", updateRequest, cookies)
	waitForIntegrationJobSucceeded(t, handler, cookies, assertIntegrationJobStarted(t, retry, "update_stacks"))
	if got := docker("inspect", "--format", "{{.Image}}", instance("app")); got == oldID {
		t.Fatal("healthy replacement did not deploy")
	}
	if warnings := dashboard().UpdateWarnings; len(warnings) != 0 {
		t.Fatalf("stale dashboard warnings after verified deployment: %+v", warnings)
	}
}
