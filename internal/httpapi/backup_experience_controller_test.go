package httpapi

import (
	"archive/tar"
	"context"
	"encoding/json"
	"mime"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"stacklab/internal/backup"
)

func TestBackupExperienceScheduleAndReadBoundaries(t *testing.T) {
	h, served, cfg := newInternalTestHandler(t)
	h.backupCapture = backup.NewNativeCapture(cfg, h.stackReader, h.jobs.Store().Snapshot)
	h.backups = backup.NewService(cfg, h.jobs, h.backupCapture)
	cookies := loginInternalTestUser(t, served, "test-password")
	for _, tc := range []struct {
		body   map[string]any
		status int
		count  int
	}{
		{map[string]any{"schedule": map[string]any{"frequency": "interval", "interval_hours": 6}, "count": 3}, 200, 3},
		{map[string]any{"schedule": map[string]any{"frequency": "manual"}}, 200, 0},
		{map[string]any{"schedule": map[string]any{"frequency": "interval", "interval_hours": 0}}, 400, 0},
		{map[string]any{"schedule": map[string]any{"frequency": "daily", "time": "25:00"}}, 400, 0},
		{map[string]any{"schedule": map[string]any{"frequency": "manual"}, "count": 11}, 400, 0},
		{map[string]any{"schedule": map[string]any{"frequency": "manual"}, "count": 0}, 400, 0},
		{map[string]any{"schedule": map[string]any{"frequency": "manual"}, "count": -1}, 400, 0},
		{map[string]any{}, 400, 0},
		{map[string]any{"schedule": nil}, 400, 0},
		{map[string]any{"schedule": map[string]any{}}, 400, 0},
	} {
		response := performInternalJSONRequest(t, served, http.MethodPost, "/api/backups/schedule-preview", tc.body, cookies)
		if response.Code != tc.status {
			t.Fatalf("preview status %d: %s", response.Code, response.Body.String())
		}
		if response.Code == 200 {
			var preview backup.SchedulePreview
			if err := json.Unmarshal(response.Body.Bytes(), &preview); err != nil {
				t.Fatal(err)
			}
			if len(preview.NextOccurrences) != tc.count {
				t.Fatalf("preview %+v", preview)
			}
		}
	}
	request := httptest.NewRequest(http.MethodPost, "http://stacklab.test/api/backups/schedule-preview", strings.NewReader(`{"schedule":{"frequency":"manual"}}`))
	request.Header.Set("Origin", "https://outside.example")
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	served.ServeHTTP(response, request)
	if response.Code != 403 {
		t.Fatalf("cross-origin preview %d", response.Code)
	}
	for _, route := range []string{
		"/api/backups/restores/unknown/files?limit=501",
		"/api/backups/restores/unknown/files?limit=0",
		"/api/backups/restores/unknown/files?offset=-1",
		"/api/backups/destinations/unknown/snapshots/abcd/files?offset=oops",
	} {
		got := performInternalJSONRequest(t, served, http.MethodGet, route, nil, cookies)
		if got.Code != 400 {
			t.Fatalf("%s = %d %s", route, got.Code, got.Body.String())
		}
	}
	response = performInternalJSONRequest(t, served, http.MethodGet, "/api/backups/restores", nil, cookies)
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"items":[]`) {
		t.Fatalf("empty library %d %s", response.Code, response.Body.String())
	}
}

func TestRecoveredFileHTTPDownloadsOnlyRegisteredRegularFiles(t *testing.T) {
	h, served, cfg := newInternalTestHandler(t)
	target, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(filepath.Join(target, "source.tar"))
	if err != nil {
		t.Fatal(err)
	}
	archive := tar.NewWriter(file)
	content := "<html>exact private recovered bytes</html>"
	for _, header := range []*tar.Header{
		{Name: "report.html", Mode: 0600, Size: int64(len(content)), Typeflag: tar.TypeReg},
		{Name: "link", Mode: 0600, Linkname: "report.html", Typeflag: tar.TypeSymlink},
	} {
		if err = archive.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if header.Typeflag == tar.TypeReg {
			if _, err = archive.Write([]byte(content)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err = archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	manifest := backup.Manifest{Version: 1, Sources: []backup.ManifestSource{{Source: backup.Source{ID: "source", Kind: "managed_config"}, SavedPath: "source.tar", Archive: true}}}
	raw, _ := json.Marshal(manifest)
	if err = os.WriteFile(filepath.Join(target, "manifest.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	result := backup.RestoreResult{ID: "restore_registered", DestinationID: "destination", Target: target, Manifest: &manifest}
	raw, _ = json.Marshal(map[string]any{"version": 1, "instance_id": "test_instance", "restores": map[string]backup.RestoreResult{target: result}})
	if err = h.jobs.Store().SetAppSetting(context.Background(), "backup.v1", string(raw), time.Now()); err != nil {
		t.Fatal(err)
	}
	h.backupCapture = backup.NewNativeCapture(cfg, h.stackReader, h.jobs.Store().Snapshot)
	h.backups = backup.NewService(cfg, h.jobs, h.backupCapture)
	cookies := loginInternalTestUser(t, served, "test-password")
	route := "/api/backups/restores/restore_registered/download?source_id=source&path=report.html"
	response := performInternalJSONRequest(t, served, http.MethodGet, route, nil, nil)
	if response.Code != 401 {
		t.Fatalf("unauthenticated download %d", response.Code)
	}
	response = performInternalJSONRequest(t, served, http.MethodGet, route, nil, cookies)
	if response.Code != 200 || response.Body.String() != content {
		t.Fatalf("download %d %s", response.Code, response.Body.String())
	}
	media, params, err := mime.ParseMediaType(response.Header().Get("Content-Disposition"))
	if err != nil || media != "attachment" || params["filename"] != "report.html" || response.Header().Get("Content-Type") != "application/octet-stream" || response.Header().Get("X-Content-Type-Options") != "nosniff" || response.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("unsafe headers %v", response.Header())
	}
	for _, tc := range []struct {
		route  string
		status int
	}{
		{"/api/backups/restores/restore_registered/download?source_id=source&path=../etc/passwd", 400},
		{"/api/backups/restores/restore_registered/download?source_id=source&path=link", 400},
		{"/api/backups/restores/unknown/download?source_id=source&path=report.html", 404},
		{"/api/backups/restores/restore_registered/download?source_id=unknown&path=report.html", 404},
	} {
		response = performInternalJSONRequest(t, served, http.MethodGet, tc.route, nil, cookies)
		if response.Code != tc.status {
			t.Fatalf("%s = %d %s", tc.route, response.Code, response.Body.String())
		}
	}
	response = performInternalJSONRequest(t, served, http.MethodGet, "/api/backups/restores/restore_registered/files?source_id=source", nil, cookies)
	if response.Code != 200 || !strings.Contains(response.Body.String(), "report.html") {
		t.Fatalf("legacy catalog %d %s", response.Code, response.Body.String())
	}
}
