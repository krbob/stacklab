package stacks

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"stacklab/internal/config"
)

func TestMaintenanceImageRollback(t *testing.T) {
	root := t.TempDir()
	stackDir := filepath.Join(root, "stacks", "demo")
	dir := filepath.Join(root, "state")
	for _, path := range []string{stackDir, dir} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path, stringData string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(stringData), 0600); err != nil {
			t.Fatal(err)
		}
	}
	definition := `name: actual
services:
  web: {image: example:latest, depends_on: {worker: {condition: service_healthy}}}
  worker: {build: .}
  unchecked: {}
  paused: {}
  excluded: {}
`
	write(filepath.Join(stackDir, "compose.yaml"), definition)
	script := `#!/bin/sh
set -eu
echo "$*" >> "$SHIM/calls"
case "$1" in
compose)
	case "$*" in
		*" version") exit 0;;
		*" config --quiet") exit 0;;
		*" config --hash "*) cat "$SHIM/expected-hashes"; exit 0;;
		*" config") cat "$SHIM/stacks/demo/compose.yaml"; exit 0;;
		*" up --help") [ ! -e "$SHIM/unsupported" ] && echo '--no-build --pull'; exit 0;;
		*" up -d"*)
			[ ! -e "$SHIM/up-fails" ] || exit 1
			[ -e "$SHIM/skip-recreate" ] || cp "$SHIM/original" "$SHIM/current"
			exit 0;;
	esac;;
ps) printf 'a web False\nb web False\nc worker False\nd unchecked False\ne paused False\nf excluded False\n'; exit 0;;
inspect) if [ "$#" = 4 ]; then cat "$SHIM/group"; else cat "$SHIM/current"; fi; exit 0;;
image)
	case "$2" in
	ls) tag=${5#*:}; if [ -f "$SHIM/$tag.pin" ]; then cat "$SHIM/$tag.pin"; fi; exit 0;;
	tag) tag=${4#*:}; echo "$3" > "$SHIM/$tag.pin"; exit 0;;
	rm) tag=${5#*:}; rm -f "$SHIM/$tag.pin"; exit 0;;
	save) [ ! -e "$SHIM/save-fails" ] || exit 1
		shift 2; for pin in "$@"; do tag=${pin#*:}; echo "$tag $(cat "$SHIM/$tag.pin")"; done; exit 0;;
	load) while read -r tag id; do echo "$id" > "$SHIM/$tag.pin"; done; exit 0;;
	esac;;
esac
exit 1
`
	docker := filepath.Join(root, "docker")
	if err := os.WriteFile(docker, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("SHIM", root)
	ResetComposeCLICacheForTests()
	t.Cleanup(ResetComposeCLICacheForTests)
	write(filepath.Join(root, "expected-hashes"), "web "+strings.Repeat("1", 64)+"\nworker "+strings.Repeat("1", 64)+"\n")
	services := []string{"web", "web", "worker", "unchecked", "paused"}
	original := make([]dockerInspectContainer, len(services))
	for i, name := range services {
		c := maintenanceContainer(string(rune('a'+i)), "running", "healthy")
		c.Config.Labels = map[string]string{"com.docker.compose.project": "actual", "com.docker.compose.service": name, "com.docker.compose.config-hash": strings.Repeat("1", 64)}
		c.Image = "sha256:" + strings.Repeat("a", 64)
		if name == "worker" {
			c.Image = "sha256:" + strings.Repeat("b", 64)
		}
		if name == "unchecked" {
			c.State.Health = nil
		}
		if name == "paused" {
			c.State.Status = "exited"
		}
		original[i] = c
	}
	encode := func(value any) string {
		t.Helper()
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	write(filepath.Join(root, "group"), encode(original[:3]))
	write(filepath.Join(root, "original"), encode(original))
	write(filepath.Join(root, "current"), encode(original))
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relativeRoot, err := filepath.Rel(cwd, root)
	if err != nil {
		t.Fatal(err)
	}
	reader := NewServiceReader(config.Config{RootDir: relativeRoot, DataDir: filepath.Join(relativeRoot, "state")}, nil)
	names := []string{"web", "worker", "unchecked", "paused"}
	baseline, _, err := reader.CaptureMaintenanceHealth(t.Context(), "demo", names)
	if err != nil {
		t.Fatal(err)
	}
	prep := func(job string) MaintenanceRollback {
		t.Helper()
		r, out, err := reader.PrepareMaintenanceRollback(t.Context(), job, baseline)
		if err != nil || !r.Eligible() || !strings.Contains(out, "unchecked") {
			t.Fatalf("prepare: %s, %v", out, err)
		}
		return r
	}
	r := prep("job_restore")
	if len(r.manifest.Services) != 2 || r.manifest.Services["web"].Replicas != 2 || r.manifest.Services["worker"].Image != original[2].Image {
		t.Fatalf("bad manifest %+v", r.manifest)
	}
	if other := prep("job_restore"); other.manifest.ArchiveHash != r.manifest.ArchiveHash {
		t.Fatal("failed to reuse archived originals")
	}
	for _, name := range []string{"manifest.json", "images.tar", "compose.rollback.yaml"} {
		info, err := os.Stat(filepath.Join(r.dir, name))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("private artifact %s: %v", name, err)
		}
	}
	var changed []dockerInspectContainer
	if err := json.Unmarshal([]byte(encode(original)), &changed); err != nil {
		t.Fatal(err)
	}
	for i := range changed[:3] {
		changed[i].Image = "sha256:" + strings.Repeat("c", 64)
	}
	changed[0].State.Health.Status = "unhealthy"
	write(filepath.Join(root, "current"), encode(changed))
	if _, err := reader.RollbackMaintenanceImages(t.Context(), r, MaintenanceHealthReport{FailedServices: []string{"unchecked"}}, false); !errors.Is(err, ErrMaintenanceRollbackUncertain) {
		t.Fatalf("unrelated failure: %v", err)
	}
	if _, err := reader.RollbackMaintenanceImages(t.Context(), r, MaintenanceHealthReport{}, false); !errors.Is(err, ErrMaintenanceRollbackUncertain) {
		t.Fatalf("absent evidence: %v", err)
	}
	failed := MaintenanceHealthReport{FailedServices: []string{"web"}}
	write(filepath.Join(stackDir, "compose.yaml"), definition+"\nnetworks: {}\n")
	if _, err := reader.RollbackMaintenanceImages(t.Context(), r, failed, false); err == nil || !strings.Contains(err.Error(), "configuration changed") {
		t.Fatalf("config drift: %v", err)
	}
	write(filepath.Join(stackDir, "compose.yaml"), definition)
	pins, err := filepath.Glob(filepath.Join(root, "*.pin"))
	if err != nil || len(pins) != 2 {
		t.Fatalf("pins: %v, %v", pins, err)
	}
	for _, pin := range pins {
		if err := os.Remove(pin); err != nil {
			t.Fatal(err)
		}
	} // simulate aggressive image pruning
	archive := filepath.Join(r.dir, "images.tar")
	goodArchive, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	write(archive, "damaged")
	if _, err := reader.RollbackMaintenanceImages(t.Context(), r, failed, false); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("corrupt archive: %v", err)
	}
	write(archive, string(goodArchive))
	write(filepath.Join(root, "up-fails"), "1")
	if _, err := reader.RollbackMaintenanceImages(t.Context(), r, failed, false); err == nil || !strings.Contains(err.Error(), r.dir) {
		t.Fatalf("failed rollback without recovery location: %v", err)
	}
	if err := os.Remove(filepath.Join(root, "up-fails")); err != nil {
		t.Fatal(err)
	}
	out, err := reader.RollbackMaintenanceImages(t.Context(), r, failed, false)
	if err != nil || !strings.Contains(out.Summary, "web, worker") || len(out.restored) != 2 {
		t.Fatalf("rollback: %+v %v", out, err)
	}
	calls, err := os.ReadFile(filepath.Join(root, "calls"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(calls), "--no-build --pull never --no-deps --force-recreate --scale web=2 --scale worker=1 -- web worker") || !strings.Contains(string(calls), "image load") {
		t.Fatalf("unexpected commands:\n%s", calls)
	}
	if _, err := reader.RollbackMaintenanceImages(t.Context(), r, failed, false); !errors.Is(err, ErrMaintenanceRollbackNotApplicable) {
		t.Fatalf("already recovered: %v", err)
	}
	if _, err := reader.RollbackMaintenanceImages(t.Context(), r, MaintenanceHealthReport{}, true); !errors.Is(err, ErrMaintenanceRollbackNotApplicable) {
		t.Fatalf("up failed before any replacements: %v", err)
	}
	write(filepath.Join(root, "current"), encode(changed))
	if _, err := reader.RollbackMaintenanceImages(t.Context(), r, MaintenanceHealthReport{}, true); err != nil {
		t.Fatalf("partially applied up failure: %v", err)
	}
	for range 2 {
		if err := reader.ReleaseMaintenanceRollback(t.Context(), r); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(r.dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("job archive left behind")
	}
	write(filepath.Join(root, "save-fails"), "1")
	if _, _, err := reader.PrepareMaintenanceRollback(t.Context(), "job_nospace", baseline); err == nil {
		t.Fatal("continued despite failed archive")
	}
	pins, _ = filepath.Glob(filepath.Join(root, "*.pin"))
	if len(pins) != 0 {
		t.Fatal("leaked temporary tags", pins)
	}
	if _, err := os.Stat(filepath.Join(dir, "maintenance-rollback", "job_nospace", "demo")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("partial archive retained after clean preflight failure")
	}
	if _, _, err := reader.PrepareMaintenanceRollback(t.Context(), "../outside", baseline); err == nil {
		t.Fatal("unsafe job ID accepted")
	}
}

func TestMaintenanceRollbackOnlyReportsConfirmedHealthRegressions(t *testing.T) {
	baseline := MaintenanceHealthBaseline{targets: map[string]maintenanceHealthTarget{"api": {replicas: 1}}}
	for _, tc := range []struct {
		name    string
		state   maintenanceServiceHealth
		failure bool
	}{
		{"still starting", maintenanceServiceHealth{summary: "starting"}, false},
		{"unhealthy", maintenanceServiceHealth{summary: "unhealthy", regression: true}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			observe := func(context.Context, string, map[string]maintenanceHealthTarget, map[string]maintenanceServiceHealth) (map[string]maintenanceServiceHealth, error) {
				return map[string]maintenanceServiceHealth{"api": tc.state}, nil
			}
			report, err := waitForMaintenanceHealth(t.Context(), baseline, 30*time.Millisecond, time.Millisecond, observe)
			if err == nil || slices.Contains(report.FailedServices, "api") != tc.failure {
				t.Fatalf("%v: %+v", err, report)
			}
		})
	}
	w := rollbackArchiveWriter{written: maxRollbackArchiveBytes, output: io.Discard}
	if _, err := w.Write([]byte("overflow")); err == nil {
		t.Fatal("archive limit not enforced")
	}
}

func TestRollbackOverrideWithInstalledCompose(t *testing.T) {
	if err := exec.Command("docker", "compose", "version").Run(); err != nil {
		t.Skip("Docker Compose CLI unavailable")
	}
	root := t.TempDir()
	stackDir := filepath.Join(root, "stacks", "demo")
	if err := os.MkdirAll(stackDir, 0700); err != nil {
		t.Fatal(err)
	}
	definition := `services:
  app:
    image: original:latest
    build: .
    depends_on: [db]
    volumes: [persist:/data]
  local:
    build: .
  db:
    image: untouched:latest
volumes:
  persist: {}
`
	if err := os.WriteFile(filepath.Join(stackDir, "compose.yaml"), []byte(definition), 0600); err != nil {
		t.Fatal(err)
	}
	r := MaintenanceRollback{root: root, dir: root, manifest: rollbackManifest{Stack: "demo", Project: "demo", Services: map[string]rollbackService{"app": {Tag: "stacklab.local/maintenance-rollback:pin"}, "local": {Tag: "stacklab.local/maintenance-rollback:build"}}}}
	reader := NewServiceReader(config.Config{RootDir: root}, nil)
	override, err := r.writeOverride([]string{"app", "local"})
	if err != nil {
		t.Fatal(err)
	}
	command, args := reader.rollbackComposeCommand(t.Context(), r, override)
	bytes, err := exec.CommandContext(t.Context(), command, append(args, "config", "--format", "json")...).Output()
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Services map[string]struct {
			Image      string
			PullPolicy string `json:"pull_policy"`
			Build      any
			DependsOn  map[string]any `json:"depends_on"`
			Volumes    []any
		}
	}
	if err := json.Unmarshal(bytes, &config); err != nil {
		t.Fatal(err)
	}
	local := config.Services["local"]
	if local.Image != r.manifest.Services["local"].Tag || local.Build == nil || local.PullPolicy != "never" {
		t.Fatalf("build-only override did not preserve the build model: %+v", local)
	}
	app := config.Services["app"]
	if app.Image != r.manifest.Services["app"].Tag || app.PullPolicy != "never" || app.Build == nil || app.DependsOn["db"] == nil || len(app.Volumes) != 1 || config.Services["db"].Image != "untouched:latest" {
		t.Fatalf("unexpected effective override %s", bytes)
	}
}
