package stacks

import (
	"archive/tar"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"stacklab/internal/config"
)

// STACKLAB_DOCKER_ROLLBACK_TEST=1 go test -run TestDockerMaintenanceImageRollback ./internal/stacks
// The opt-in test uses only local scratch images and a unique Compose project.
func TestDockerMaintenanceImageRollback(t *testing.T) {
	if os.Getenv("STACKLAB_DOCKER_ROLLBACK_TEST") != "1" {
		t.Skip("set STACKLAB_DOCKER_ROLLBACK_TEST=1 to use a local Docker daemon")
	}
	contextName := os.Getenv("DOCKER_CONTEXT")
	if contextName == "" {
		contextName = "default"
	}
	t.Setenv("DOCKER_CONTEXT", contextName)
	t.Setenv("DOCKER_HOST", "")
	docker := func(args ...string) string {
		t.Helper()
		out, err := exec.CommandContext(t.Context(), "docker", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("docker %s: %v\n%s", args[0], err, out)
		}
		return strings.TrimSpace(string(out))
	}
	if !strings.HasPrefix(docker("context", "inspect", contextName, "--format", `{{(index .Endpoints "docker").Host}}`), "unix://") {
		t.Fatal("test requires a local Unix Docker socket")
	}
	platform := docker("version", "--format", "{{.Server.Os}}/{{.Server.Arch}}")
	arch, ok := strings.CutPrefix(platform, "linux/")
	if !ok {
		t.Fatalf("Linux containers required: %s", platform)
	}
	var unique [8]byte
	if _, err := rand.Read(unique[:]); err != nil {
		t.Fatal(err)
	}
	stackID := "stacklab-test-rollback-" + hex.EncodeToString(unique[:])
	root := t.TempDir()
	projectDir := filepath.Join(root, "stacks", stackID)
	dataDir := filepath.Join(root, "state")
	for _, p := range []string{projectDir, dataDir} {
		if err := os.MkdirAll(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	tag := stackID + ":test"
	compose := []string{"compose", "--project-directory", projectDir, "-f", filepath.Join(projectDir, "compose.yaml")}
	runCompose := func(args ...string) string { return docker(append(slices.Clone(compose), args...)...) }
	var oldID, newID string
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, "docker", append(slices.Clone(compose), "down", "--volumes", "--remove-orphans")...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("clean up test project: %v %s", err, out)
		}
		for _, ref := range []string{tag, oldID, newID} {
			if ref != "" {
				_ = exec.CommandContext(ctx, "docker", "image", "rm", "--force", "--no-prune", ref).Run()
			}
		}
	})
	program := `package main
import ("os";"time")
func main(){
	if len(os.Args)==1 { for {time.Sleep(time.Hour)} }
	switch os.Args[1] {
	case "check": b,err:=os.ReadFile("/version");if err!=nil || string(b)!="old"{os.Exit(1)}
	case "mark": if os.WriteFile("/state/marker",[]byte(os.Args[2]),0600)!=nil{os.Exit(1)}
	case "marker": b,err:=os.ReadFile("/state/marker");if err!=nil{os.Exit(1)};os.Stdout.Write(b)
	default: os.Exit(2)
	}
}`
	source := filepath.Join(root, "main.go")
	if err := os.WriteFile(source, []byte(program), 0600); err != nil {
		t.Fatal(err)
	}
	build := exec.CommandContext(t.Context(), "go", "build", "-trimpath", "-o", filepath.Join(root, "probe"), source)
	build.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH="+arch, "GOWORK=off")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build probe: %v %s", err, out)
	}
	probe, err := os.ReadFile(filepath.Join(root, "probe"))
	if err != nil {
		t.Fatal(err)
	}
	image := func(version string) string {
		t.Helper()
		file, err := os.Create(filepath.Join(root, "rootfs.tar"))
		if err != nil {
			t.Fatal(err)
		}
		tw := tar.NewWriter(file)
		for _, entry := range []struct {
			name string
			mode int64
			data []byte
		}{{"probe", 0755, probe}, {"version", 0444, []byte(version)}} {
			if err := tw.WriteHeader(&tar.Header{Name: entry.name, Mode: entry.mode, Size: int64(len(entry.data))}); err != nil {
				t.Fatal(err)
			}
			if _, err := tw.Write(entry.data); err != nil {
				t.Fatal(err)
			}
		}
		if err := tw.Close(); err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		docker("image", "import", "--platform", platform, "--change", `ENTRYPOINT ["/probe"]`, file.Name(), tag)
		return docker("image", "inspect", "--format", "{{.Id}}", tag)
	}
	yaml := fmt.Sprintf(`services:
  app:
    image: %s
    pull_policy: never
    env_file: [service.env]
    environment:
      STACKLAB_TEST_MODE: ${TEST_MODE}
      STACKLAB_TEST_DOLLAR: "$$LITERAL"
    healthcheck:
      test: ["CMD", "/probe", "check", "$$LITERAL"]
      interval: 1s
      timeout: 1s
      retries: 1
    volumes: ["data:/state"]
volumes:
  data: {}
`, tag)
	write := func(name, value string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(projectDir, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("compose.yaml", yaml)
	write(".env", "TEST_MODE=baseline\n")
	write("service.env", "STACKLAB_FILE_VALUE=previous\n")
	oldID = image("old")
	runCompose("up", "-d", "--pull", "never", "--wait", "--wait-timeout", "30")
	id := runCompose("ps", "-q", "app")
	docker("exec", id, "/probe", "mark", stackID)
	reader := NewServiceReader(config.Config{RootDir: root, DataDir: dataDir}, nil)
	assertDrift := func(file, newText, original string) {
		t.Helper()
		write(file, newText)
		defer write(file, original)
		b, _, err := reader.CaptureMaintenanceHealth(t.Context(), stackID, nil)
		if err != nil {
			t.Fatal(err)
		}
		d, summary, err := reader.PrepareMaintenanceRollback(t.Context(), "job_drift", b)
		if d.Eligible() {
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				if err := reader.ReleaseMaintenanceRollback(ctx, d); err != nil {
					t.Error(err)
				}
			})
		}
		if err != nil || d.Eligible() || d.Warning == "" {
			t.Fatalf("pending edit of %s eligible, result %q: %v", file, summary, err)
		}
	}
	assertDrift(".env", "TEST_MODE=edited\n", "TEST_MODE=baseline\n")
	assertDrift("service.env", "STACKLAB_FILE_VALUE=edited\n", "STACKLAB_FILE_VALUE=previous\n")
	assertDrift("compose.yaml", strings.Replace(yaml, "    pull_policy: never", "    command: [unexpected]\n    pull_policy: never", 1), yaml)
	assertDrift("compose.yaml", strings.Replace(yaml, "data:/state", "data:/other", 1), yaml)
	assertDrift("compose.yaml", strings.Replace(yaml, "  data: {}", "  data: {name: "+stackID+"-unused}", 1), yaml)
	baseline, _, err := reader.CaptureMaintenanceHealth(t.Context(), stackID, nil)
	if err != nil {
		t.Fatal(err)
	}
	r, description, err := reader.PrepareMaintenanceRollback(t.Context(), "job_native", baseline)
	if err != nil || !r.Eligible() {
		t.Fatalf("prepare: %s, %v, old: %s, observed: %+v", description, err, oldID, baseline.before)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := reader.ReleaseMaintenanceRollback(ctx, r); err != nil {
			t.Error(err)
		}
	})
	newID = image("new")
	if newID == oldID {
		t.Fatal("fixture images have the same identity")
	}
	runCompose("up", "-d", "--pull", "never", "--force-recreate")
	if got := docker("inspect", "--format", "{{.Image}}", runCompose("ps", "-q", "app")); got != newID {
		t.Fatalf("update deployed %s, expected %s", got, newID)
	}
	failed, err := waitForMaintenanceHealth(t.Context(), baseline, 6*time.Second, 200*time.Millisecond, observeMaintenanceHealth)
	if err == nil || !slices.Contains(failed.FailedServices, "app") {
		t.Fatalf("new image did not fail verification: %+v, %v", failed, err)
	}
	pin := r.manifest.Services["app"].Tag
	docker("image", "rm", "--force", "--no-prune", pin)
	if got := docker("image", "ls", "--no-trunc", "--quiet", pin); got != "" {
		t.Fatalf("pin still exists before recovery: %s", got)
	}
	out, err := reader.RollbackMaintenanceImages(t.Context(), r, failed, false)
	if err != nil {
		t.Fatalf("recover: %+v %v", out, err)
	}
	id = runCompose("ps", "-q", "app")
	if got := docker("inspect", "--format", "{{.Image}} {{.State.Health.Status}}", id); got != oldID+" healthy" {
		t.Fatalf("recovered %s; expected %s healthy", got, oldID)
	}
	if got := docker("exec", id, "/probe", "marker"); got != stackID {
		t.Fatalf("volume marker changed: %s", got)
	}
	if got := docker("image", "inspect", "--format", "{{.Id}}", tag); got != newID {
		t.Fatalf("shared tag was rewound to %s", got)
	}
	if err := reader.ReleaseMaintenanceRollback(t.Context(), r); err != nil {
		t.Fatal(err)
	}
	if got := docker("image", "ls", "--no-trunc", "--quiet", pin); got != "" {
		t.Fatalf("pin still exists after successful recovery: %s", got)
	}
	next, _, err := reader.CaptureMaintenanceHealth(t.Context(), stackID, nil)
	if err != nil {
		t.Fatal(err)
	}
	again, why, err := reader.PrepareMaintenanceRollback(t.Context(), "job_again", next)
	if err != nil || !again.Eligible() {
		t.Fatalf("unchanged service after rollback should remain protected: %s %v runtime=%+v", why, err, next.before)
	}
	if err := reader.ReleaseMaintenanceRollback(t.Context(), again); err != nil {
		t.Fatal(err)
	}
	assertDrift(".env", "TEST_MODE=edited\n", "TEST_MODE=baseline\n")
	t.Logf("restored healthy image %s from archive; mutable tag still resolves to %s; volume marker preserved", oldID, newID)
}
