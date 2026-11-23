package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"stacklab/internal/maintenancejobs"
	"stacklab/internal/store"
)

func TestUpdateProtectionSettingsSurviveStateBackup(t *testing.T) {
	shim := t.TempDir()
	writeInternalDockerShim(t, filepath.Join(shim, "docker"))
	t.Setenv("PATH", shim+string(os.PathListSeparator)+os.Getenv("PATH"))
	h, served, cfg := newInternalTestHandler(t)
	cookies := loginInternalTestUser(t, served, "test-password")
	root := filepath.Join(cfg.RootDir, "stacks", "demo")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "compose.yaml"), []byte("services:\n  app:\n    image: demo:latest\n"), 0600); err != nil {
		t.Fatal(err)
	}
	const path = "/api/stacks/demo/update-protection"
	if r := performInternalJSONRequest(t, served, http.MethodGet, path, nil, nil); r.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous read: %d", r.Code)
	}
	read := func() maintenancejobs.UpdateProtectionPolicy {
		t.Helper()
		r := performInternalJSONRequest(t, served, http.MethodGet, path, nil, cookies)
		if r.Code != http.StatusOK {
			t.Fatalf("get policy: %d %s", r.Code, r.Body.String())
		}
		var policy maintenancejobs.UpdateProtectionPolicy
		decodeInternalResponse(t, r, &policy)
		return policy
	}
	initial := read()
	if initial.AutoRollback || initial.Revision == "" {
		t.Fatal("rollback enabled by default")
	}
	cross := httptest.NewRequest(http.MethodPut, path, strings.NewReader(`{"auto_rollback":true}`))
	cross.Header.Set("Content-Type", "application/json")
	cross.Header.Set("Origin", "https://other.example")
	for _, cookie := range cookies {
		cross.AddCookie(cookie)
	}
	result := httptest.NewRecorder()
	served.ServeHTTP(result, cross)
	if result.Code != http.StatusForbidden {
		t.Fatalf("cross-origin update: %d", result.Code)
	}
	if r := performInternalJSONRequest(t, served, http.MethodPut, path, map[string]any{"auto_rollback": true, "revision": initial.Revision}, cookies); r.Code != http.StatusOK {
		t.Fatalf("save policy: %d %s", r.Code, r.Body.String())
	}
	current := read()
	if !current.AutoRollback || current.Revision == initial.Revision {
		t.Fatal("save did not return a fresh revision")
	}
	stale := performInternalJSONRequest(t, served, http.MethodPut, path, map[string]any{"auto_rollback": false, "revision": initial.Revision}, cookies)
	if stale.Code != http.StatusConflict || !strings.Contains(stale.Body.String(), "reload") || !read().AutoRollback {
		t.Fatalf("stale client overwrote policy: %d %s", stale.Code, stale.Body.String())
	}
	if r := performInternalJSONRequest(t, served, http.MethodPut, path, map[string]any{"auto_rollback": true}, cookies); r.Code != http.StatusOK {
		t.Fatalf("unconditional API save: %d", r.Code)
	}
	if r := performInternalJSONRequest(t, served, http.MethodPut, path, map[string]any{}, cookies); r.Code != http.StatusBadRequest || !read().AutoRollback {
		t.Fatal("incomplete update cleared the saved policy")
	}
	snapshot := filepath.Join(t.TempDir(), "snapshot.db")
	if err := h.jobs.Store().Snapshot(t.Context(), snapshot); err != nil {
		t.Fatal(err)
	}
	copy, err := store.Open(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	defer copy.Close()
	value, found, err := copy.AppSetting(t.Context(), "maintenance.protection.demo")
	var restored maintenancejobs.UpdateProtectionPolicy
	if err != nil || !found || json.Unmarshal([]byte(value), &restored) != nil || !restored.AutoRollback {
		t.Fatalf("policy missing from private state backup: %+v, %v", restored, err)
	}
}
