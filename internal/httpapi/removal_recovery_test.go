package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"stacklab/internal/stacks"
	"stacklab/internal/store"
	"stacklab/internal/workspacerepair"
)

type testDirectoryRepair struct{ target string }

func (testDirectoryRepair) Capability(context.Context) workspacerepair.Capability {
	return workspacerepair.Capability{Supported: true, Recursive: true}
}
func (r testDirectoryRepair) RepairRemovalDirectories(_ context.Context, target string) error {
	if target != r.target {
		return errors.New("unexpected repair target")
	}
	return os.Chmod(target, 0700)
}

func TestRemovalRecoveryFromHistory(t *testing.T) {
	h, served, cfg := newInternalTestHandler(t)
	ctx := context.Background()
	cookies := loginInternalTestUser(t, served, "test-password")
	dir := filepath.Join(cfg.RootDir, "data", "uptime-kuma")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "kuma.db"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })
	h.removalRepair = testDirectoryRepair{dir}
	steps := []store.JobWorkflowStep{
		{Action: "down", State: "succeeded"},
		{Action: "remove_stack_definition", State: "succeeded"},
		{Action: "remove_config", State: "succeeded"},
		{Action: "remove_data", State: "failed"},
	}
	job, err := h.jobs.StartWithWorkflow(ctx, "uptime-kuma", "remove_stack_definition", "local", steps)
	if err != nil {
		t.Fatal(err)
	}
	job, err = h.jobs.FinishFailed(ctx, job, "remove_stack_failed", "remove data dir: unlinkat kuma.db: permission denied")
	if err != nil {
		t.Fatal(err)
	}
	url := "/api/jobs/" + job.ID
	snapshot := performInternalJSONRequest(t, served, http.MethodGet, url, nil, cookies)
	var detail struct {
		Recovery struct{ Targets []string } `json:"removal_recovery"`
	}
	if err := json.Unmarshal(snapshot.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if len(detail.Recovery.Targets) != 1 || detail.Recovery.Targets[0] != "data/uptime-kuma" {
		t.Fatal(snapshot.Body.String())
	}

	// A definition restored outside Stacklab must also block a historical retry.
	root := filepath.Join(cfg.RootDir, "stacks", "uptime-kuma")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	compose := filepath.Join(root, "compose.yaml")
	if err := os.WriteFile(compose, []byte("services:\n  app:\n    image: example\n"), 0600); err != nil {
		t.Fatal(err)
	}
	post := func() int {
		return performInternalJSONRequest(t, served, http.MethodPost, url+"/retry-removal", nil, cookies).Code
	}
	if code := post(); code != 409 {
		t.Fatalf("recreated stack status %d", code)
	}
	if err := os.Remove(compose); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(root); err != nil {
		t.Fatal(err)
	}
	response := performInternalJSONRequest(t, served, http.MethodPost, url+"/retry-removal", nil, cookies)
	id := assertInternalRunningJob(t, response, "remove_stack_definition")
	waitForInternalJobState(t, served, cookies, id, "succeeded")
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("remaining data: %v", err)
	}
	if code := post(); code != 409 {
		t.Fatalf("replay status %d", code)
	}
}

func TestRemovalKeepsDefinitionOnDataPermissionError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires unprivileged remove")
	}
	h, served, cfg := newInternalTestHandler(t)
	cookies := loginInternalTestUser(t, served, "test-password")
	ctx := context.Background()
	err := h.stackReader.CreateStack(ctx, stacks.CreateStackRequest{StackID: "demo", ComposeYAML: "services:\n  app:\n    image: example\n", CreateDataDir: true})
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(cfg.RootDir, "data", "demo")
	if err := os.WriteFile(filepath.Join(dir, "test.db"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })
	h.removalRepair = testDirectoryRepair{dir}
	res := performInternalJSONRequest(t, served, http.MethodDelete, "/api/stacks/demo", map[string]bool{"remove_data": true, "remove_definition": true}, cookies)
	id := assertInternalRunningJob(t, res, "remove_stack_definition")
	waitForInternalJobState(t, served, cookies, id, "failed")
	compose := filepath.Join(cfg.RootDir, "stacks", "demo", "compose.yaml")
	if _, err := os.Stat(compose); err != nil {
		t.Fatalf("definition lost: %v", err)
	}
	response := performInternalJSONRequest(t, served, http.MethodPost, "/api/jobs/"+id+"/retry-removal", nil, cookies)
	retryID := assertInternalRunningJob(t, response, "remove_stack_definition")
	waitForInternalJobState(t, served, cookies, retryID, "succeeded")
	for _, path := range []string{dir, compose} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s still exists: %v", path, err)
		}
	}
}
