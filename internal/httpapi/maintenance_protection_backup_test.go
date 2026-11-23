package httpapi_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"stacklab/internal/backup"
	"stacklab/internal/maintenancejobs"
	"stacklab/internal/stacks"
	"stacklab/internal/store"
)

func TestStackCoverageApprovesBackupAndImageRollbackTogether(t *testing.T) {
	binDir := t.TempDir()
	const compose = "services:\n  app:\n    image: busybox:1.36\n"
	shim := `#!/bin/sh
case "$1" in
  ps) exit 0;;
  compose) case "$*" in
    *version*) echo 'Docker Compose version v2.39.0'; exit 0;;
    *config*) printf '%s' "$TEST_COMPOSE_CONFIG"; exit 0;;
  esac;;
esac
exit 1
`
	if err := os.WriteFile(filepath.Join(binDir, "docker"), []byte(shim), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	t.Setenv("TEST_COMPOSE_CONFIG", compose)
	stacks.ResetComposeCLICacheForTests()
	t.Cleanup(stacks.ResetComposeCLICacheForTests)
	h, cfg := newTestHandler(t)
	cookies := loginTestUser(t, h, "test-password")
	const coverageURL = "/api/stacks/demo/backups"
	const policyURL = "/api/stacks/demo/update-protection"
	const password = "fixture-secret-not-for-coverage-api"
	call := func(method, path string, input, output any) {
		t.Helper()
		result := performJSONRequest(t, h, method, path, input, cookies)
		if strings.Contains(result.Body.String(), password) {
			t.Fatalf("%s %s exposed repository password", method, path)
		}
		if result.Code != http.StatusOK {
			t.Fatalf("%s %s: %d %s", method, path, result.Code, result.Body.String())
		}
		if output != nil {
			decodeResponse(t, result, output)
		}
	}
	call(http.MethodPost, "/api/stacks", map[string]any{"stack_id": "demo", "compose_yaml": compose, "create_config_dir": true}, nil)
	type coverageResponse struct {
		Sources      []backup.Source      `json:"sources"`
		Plans        []backup.Plan        `json:"plans"`
		Destinations []backup.Destination `json:"destinations"`
	}
	var discovered coverageResponse
	call(http.MethodGet, coverageURL, nil, &discovered)
	if len(discovered.Sources) != 2 || len(discovered.Plans) != 0 {
		t.Fatalf("unexpected initial coverage: %+v", discovered)
	}
	var kinds []string
	for _, src := range discovered.Sources {
		if src.StackID != "demo" {
			t.Fatalf("wrong source scope: %+v", src)
		}
		kinds = append(kinds, src.Kind)
	}
	slices.Sort(kinds)
	if !slices.Equal(kinds, []string{"managed_config", "stack_definition"}) {
		t.Fatalf("source kinds: %v", kinds)
	}
	repository := func(name string) backup.Destination {
		t.Helper()
		var saved struct {
			Destination backup.Destination `json:"destination"`
		}
		call(http.MethodPost, "/api/backups/destinations", map[string]any{
			"name": name, "kind": "local", "repository": filepath.Join(t.TempDir(), "repo"),
			"expected_filesystem": "fixture-fs", "secrets": map[string]string{"password": password},
		}, &saved)
		if saved.Destination.ID == "" {
			t.Fatal("destination missing ID")
		}
		return saved.Destination
	}
	destination := repository("Private backup disk")
	repository("Unrelated disk")
	var created struct {
		Plan backup.Plan `json:"plan"`
	}
	call(http.MethodPost, "/api/backups/plans", backup.Plan{
		Name: "Configuration and definition", Enabled: true, Consistency: "files",
		Sources: discovered.Sources, Destinations: []backup.Assignment{{DestinationID: destination.ID, Enabled: true}},
	}, &created)
	var coverage coverageResponse
	call(http.MethodGet, coverageURL, nil, &coverage)
	if len(coverage.Plans) != 1 || len(coverage.Destinations) != 1 || !reflect.DeepEqual(discovered.Sources, coverage.Sources) {
		t.Fatalf("unexpected coverage: %+v", coverage)
	}
	plan, dest := coverage.Plans[0], coverage.Destinations[0]
	if plan.ID == "" || plan.ID != created.Plan.ID || !reflect.DeepEqual(plan.Sources, discovered.Sources) || len(plan.UpdateBackupFingerprint) != 64 {
		t.Fatalf("reviewed plan lacks the expected source selection or fingerprint: %+v", plan)
	}
	if len(plan.Destinations) != 1 || !plan.Destinations[0].Enabled || plan.Destinations[0].DestinationID != dest.ID {
		t.Fatalf("reviewed destination differs from plan assignment: %+v %+v", plan.Destinations, dest)
	}
	if dest.ID != destination.ID || dest.Name != destination.Name || dest.Kind != "local" || dest.Repository != destination.Repository || dest.ExpectedFilesystem != "fixture-fs" {
		t.Fatalf("repository identity differs from the requested destination: %+v", dest)
	}
	var initial maintenancejobs.UpdateProtectionPolicy
	call(http.MethodGet, policyURL, nil, &initial)
	sourceIDs := make([]string, 0, len(plan.Sources))
	for _, src := range plan.Sources {
		sourceIDs = append(sourceIDs, src.ID)
	}
	slices.Sort(sourceIDs)
	approved := maintenancejobs.UpdateProtectionPolicy{Revision: initial.Revision, AutoRollback: true, BackupPlanID: plan.ID, BackupSourceIDs: sourceIDs, BackupPlanFingerprint: plan.UpdateBackupFingerprint}
	var saved maintenancejobs.UpdateProtectionPolicy
	call(http.MethodPut, policyURL, approved, &saved)
	if saved.Revision == "" || saved.Revision == initial.Revision {
		t.Fatal("saved protection is missing a new revision")
	}
	approved.Revision = saved.Revision
	if !reflect.DeepEqual(saved, approved) {
		t.Fatalf("saved protection: %+v, want %+v", saved, approved)
	}
	var current maintenancejobs.UpdateProtectionPolicy
	call(http.MethodGet, policyURL, nil, &current)
	if !reflect.DeepEqual(current, saved) {
		t.Fatalf("read after save: %+v", current)
	}
	// Reopen a native SQLite snapshot to prove that both options, the selected
	// sources and the review fingerprint persist together outside the live handler.
	db, err := store.Open(cfg.DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	path := filepath.Join(t.TempDir(), "stacklab-snapshot.db")
	if err := db.Snapshot(t.Context(), path); err != nil {
		t.Fatal(err)
	}
	copy, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer copy.Close()
	raw, revision, err := copy.AppSettingWithRevision(t.Context(), store.StackUpdateProtectionKey("demo"))
	if err != nil {
		t.Fatal(err)
	}
	var restored maintenancejobs.UpdateProtectionPolicy
	if err := json.Unmarshal([]byte(raw), &restored); err != nil {
		t.Fatal(err)
	}
	restored.Revision = revision
	if !reflect.DeepEqual(restored, approved) {
		t.Fatalf("snapshot lost protection settings: %+v", restored)
	}
}
