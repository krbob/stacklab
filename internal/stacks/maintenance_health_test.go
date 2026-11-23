package stacks

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"stacklab/internal/config"
)

func TestMaintenanceHealthTargets(t *testing.T) {
	definition := []byte(`name: actual-project
services:
  web:
    labels: {io.stacklab.health.http: /readyz, io.stacklab.health.port: "8080"}
    depends_on: {init: {condition: service_completed_successfully}}
    deploy: {replicas: 2}
  init: {}
  paused: {scale: 0, depends_on: [init]}
  excluded:
    labels: {io.stacklab.health.http: "invalid"}
`)
	project, targets, err := parseMaintenanceHealthTargets(definition, []string{"web", "paused", "init"})
	if err != nil || project != "actual-project" || len(targets) != 3 || targets["web"].replicas != 2 || targets["web"].httpCheck.port != "8080" || targets["paused"].replicas != 0 || !targets["init"].completionExpected {
		t.Fatalf("unexpected targets %v %v %v", project, targets, err)
	}
	if _, _, err := parseMaintenanceHealthTargets(definition, []string{"missing"}); err == nil {
		t.Fatal("accepted absent target")
	}
}

func maintenanceContainer(id, state, health string) dockerInspectContainer {
	var c dockerInspectContainer
	c.ID = id
	c.State.Status = state
	if health != "" {
		c.State.Health = &struct {
			Status string `json:"Status"`
		}{Status: health}
	}
	return c
}

func TestAssessMaintenanceService(t *testing.T) {
	normal := maintenanceHealthTarget{replicas: 1}
	oneShot := maintenanceHealthTarget{replicas: 1, completionExpected: true}
	good := maintenanceContainer("a", "running", "healthy")
	other := maintenanceContainer("b", "running", "healthy")
	unhealthy := maintenanceContainer("b", "running", "unhealthy")
	exited := maintenanceContainer("a", "exited", "")
	failed := exited
	failed.State.ExitCode = 1
	zero := maintenanceServiceHealth{}
	for _, tc := range []struct {
		name     string
		target   maintenanceHealthTarget
		items    []dockerInspectContainer
		before   maintenanceServiceHealth
		ready    bool
		contains string
	}{
		{"ready", normal, []dockerInspectContainer{good}, zero, true, "1 healthy"},
		{"replicas", normal, []dockerInspectContainer{good, unhealthy}, zero, false, "1 unhealthy"},
		{"fresh missing", normal, nil, zero, false, "1 missing container"},
		{"older scaled with CLI", normal, []dockerInspectContainer{good}, maintenanceServiceHealth{instances: 2}, false, "1 missing"},
		{"configured downscale", maintenanceHealthTarget{replicas: 1, explicitScale: true}, []dockerInspectContainer{good}, maintenanceServiceHealth{instances: 2}, true, "1 healthy"},
		{"ready replicas", normal, []dockerInspectContainer{good, other}, maintenanceServiceHealth{instances: 2}, true, "2 healthy"},
		{"pre-existing", normal, []dockerInspectContainer{unhealthy}, maintenanceServiceHealth{summary: "1 unhealthy"}, false, "before deployment"},
		{"no check", normal, []dockerInspectContainer{maintenanceContainer("a", "running", "")}, zero, true, "readiness unverified"},
		{"declared check absent", maintenanceHealthTarget{replicas: 1, requiresNative: true}, []dockerInspectContainer{maintenanceContainer("a", "running", "")}, zero, false, "configured Docker healthcheck missing"},
		{"removed check", normal, []dockerInspectContainer{maintenanceContainer("a", "running", "")}, maintenanceServiceHealth{native: 1}, false, "previous Docker healthcheck missing"},
		{"starting", normal, []dockerInspectContainer{maintenanceContainer("a", "running", "starting")}, zero, false, "starting"},
		{"crashing", normal, []dockerInspectContainer{maintenanceContainer("a", "restarting", "")}, zero, false, "restarting"},
		{"one-shot pending", oneShot, []dockerInspectContainer{good}, zero, false, "waiting"},
		{"one-shot success", oneShot, []dockerInspectContainer{exited}, zero, true, "completed"},
		{"one-shot failure", oneShot, []dockerInspectContainer{failed}, zero, false, "exited (1)"},
		{"unexpected exit zero", normal, []dockerInspectContainer{exited}, zero, false, "exited (0)"},
		{"scale zero", maintenanceHealthTarget{}, nil, zero, true, "skipped"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := assessMaintenanceService(tc.target, tc.items, tc.before, nil)
			if got.ready != tc.ready || !strings.Contains(got.summary, tc.contains) {
				t.Fatalf("%+v", got)
			}
		})
	}
}

func TestMaintenanceHealthCaptureFromFreshDocker(t *testing.T) {
	root := t.TempDir()
	stackDir := filepath.Join(root, "stacks", "site")
	if err := os.MkdirAll(stackDir, 0700); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
case "$1" in
compose)
	case "$*" in
		*" version") exit 0;;
		*" config") echo 'name: myproject'; echo 'services: {api: {}, other: {}}'; exit 0;;
	esac;;
ps)
	case "$*" in *'label=com.docker.compose.project=myproject'*) ;; *) exit 1;; esac
	echo 'idapi api False'; echo 'idother other False'; echo 'idjob api True'; exit 0;;
inspect)
	[ "$#" = 2 ] && [ "$2" = idapi ] || exit 1
	cat "$INSPECTION"; exit $?;;
esac
exit 1
`
	if err := os.WriteFile(filepath.Join(root, "docker"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	inspection := filepath.Join(root, "inspection")
	t.Setenv("INSPECTION", inspection)
	container := maintenanceContainer("idapi", "running", "healthy")
	container.Config.Labels = map[string]string{"com.docker.compose.project": "myproject", "com.docker.compose.service": "api"}
	data, err := json.Marshal([]dockerInspectContainer{container})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inspection, data, 0600); err != nil {
		t.Fatal(err)
	}
	ResetComposeCLICacheForTests()
	t.Cleanup(ResetComposeCLICacheForTests)
	reader := NewServiceReader(config.Config{RootDir: root}, nil)
	baseline, summary, err := reader.CaptureMaintenanceHealth(t.Context(), "site", []string{"api"})
	if err != nil || !strings.Contains(summary, "api: 1 healthy") || len(baseline.targets) != 1 {
		t.Fatalf("%s %v", summary, err)
	}
	if err := os.Remove(inspection); err != nil {
		t.Fatal(err)
	}
	if _, err := observeMaintenanceHealth(t.Context(), baseline.project, baseline.targets, baseline.before); err == nil {
		t.Fatal("failed inspection interpreted as no containers")
	}
}

func TestMaintenanceVerifyRequiresStableObservation(t *testing.T) {
	baseline := MaintenanceHealthBaseline{project: "stack", targets: map[string]maintenanceHealthTarget{"a": {replicas: 1}}}
	states := []maintenanceServiceHealth{{summary: "starting"}, {ready: true, identity: "old"}, {ready: true, identity: "new"}}
	count := 0
	observe := func(context.Context, string, map[string]maintenanceHealthTarget, map[string]maintenanceServiceHealth) (map[string]maintenanceServiceHealth, error) {
		count++
		if count == 2 {
			return nil, errors.New("docker unavailable")
		}
		i := min(max(0, count-2), len(states)-1)
		return map[string]maintenanceServiceHealth{"a": states[i]}, nil
	}
	if _, err := waitForMaintenanceHealth(t.Context(), baseline, time.Second, time.Millisecond, observe); err != nil || count != 5 {
		t.Fatalf("polls %d, %v", count, err)
	}
	unavailable := func(context.Context, string, map[string]maintenanceHealthTarget, map[string]maintenanceServiceHealth) (map[string]maintenanceServiceHealth, error) {
		return nil, errors.New("docker unavailable")
	}
	if _, err := waitForMaintenanceHealth(t.Context(), baseline, 25*time.Millisecond, time.Millisecond, unavailable); err == nil || !strings.Contains(err.Error(), "docker unavailable") {
		t.Fatalf("%v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := waitForMaintenanceHealth(ctx, baseline, time.Hour, time.Hour, observe); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestMaintenanceHealthReportNamesRunningServicesWithoutProbes(t *testing.T) {
	running := maintenanceContainer("container", "running", "")
	healthy := maintenanceContainer("other", "running", "healthy")
	normal := maintenanceHealthTarget{replicas: 1}
	before := maintenanceServiceHealth{}
	current := map[string]maintenanceServiceHealth{
		"worker":   assessMaintenanceService(normal, []dockerInspectContainer{running, healthy}, before, nil),
		"api":      assessMaintenanceService(normal, []dockerInspectContainer{healthy}, before, nil),
		"batch":    assessMaintenanceService(normal, []dockerInspectContainer{running}, before, nil),
		"disabled": assessMaintenanceService(maintenanceHealthTarget{}, nil, before, nil),
		"init":     assessMaintenanceService(maintenanceHealthTarget{replicas: 1, completionExpected: true}, []dockerInspectContainer{maintenanceContainer("init", "exited", "")}, before, nil),
	}
	baseline := MaintenanceHealthBaseline{targets: map[string]maintenanceHealthTarget{}}
	for name := range current {
		baseline.targets[name] = normal
	}
	observe := func(context.Context, string, map[string]maintenanceHealthTarget, map[string]maintenanceServiceHealth) (map[string]maintenanceServiceHealth, error) {
		return current, nil
	}
	report, err := waitForMaintenanceHealth(t.Context(), baseline, time.Second, time.Millisecond, observe)
	if err != nil || strings.Join(report.UnverifiedServices, ",") != "batch,worker" || !strings.Contains(report.Summary, "api: 1 healthy") {
		t.Fatalf("%+v, %v", report, err)
	}
}

func TestMaintenanceHealthTimeoutIdentifiesFailedService(t *testing.T) {
	baseline := MaintenanceHealthBaseline{targets: map[string]maintenanceHealthTarget{"api": {}, "web": {}}}
	observe := func(context.Context, string, map[string]maintenanceHealthTarget, map[string]maintenanceServiceHealth) (map[string]maintenanceServiceHealth, error) {
		return map[string]maintenanceServiceHealth{
			"api": {summary: "1 healthy", ready: true},
			"web": {summary: "1 unhealthy"},
		}, nil
	}
	report, err := waitForMaintenanceHealth(t.Context(), baseline, 25*time.Millisecond, time.Millisecond, observe)
	if err == nil || !strings.Contains(err.Error(), "web: 1 unhealthy") || strings.Contains(err.Error(), "api:") || !strings.Contains(report.Summary, "api: 1 healthy") {
		t.Fatalf("unexpected timeout diagnostics: %+v, %v", report, err)
	}
}

func TestMaintenanceHTTPPathAndPinnedTransport(t *testing.T) {
	labels := map[string]string{"io.stacklab.health.http": "/readyz", "io.stacklab.health.port": "8080"}
	for _, path := range []string{"", "http://outside.local/", "//outside.local/", "/%2foutside.local", "/a%0d%0aHost:bad", "/a\\b", "/x?secret=token", "/fragment#bad"} {
		labels["io.stacklab.health.http"] = path
		if _, err := newMaintenanceHTTPCheck(labels); err == nil {
			t.Fatalf("accepted path %q", path)
		}
	}
	labels["io.stacklab.health.http"] = "/readyz"
	check, err := newMaintenanceHTTPCheck(labels)
	if err != nil {
		t.Fatal(err)
	}
	var c dockerInspectContainer
	buildContainer := func(ip, mode string) {
		b, _ := json.Marshal(map[string]any{"HostConfig": map[string]string{"NetworkMode": mode}, "NetworkSettings": map[string]any{"Networks": map[string]any{"network": map[string]string{"IPAddress": ip}}}})
		if err := json.Unmarshal(b, &c); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct{ ip, mode string }{{"127.0.0.1", "bridge"}, {"169.254.169.254", "bridge"}, {"8.8.8.8", "bridge"}, {"172.18.0.7", "host"}, {"172.18.0.7", "container:other"}} {
		buildContainer(tc.ip, tc.mode)
		if result := check.probe(t.Context(), c, nil); result.ready || result.regression {
			t.Fatal(tc)
		}
	}
	buildContainer("172.18.0.7", "bridge")
	transport := maintenanceHTTPClient.Transport.(*http.Transport).Clone()
	if transport.Proxy != nil {
		t.Fatal("environment proxy enabled")
	}
	defer transport.CloseIdleConnections()
	for _, status := range []int{200, 301, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.Method != "GET" || r.URL.Path != "/readyz" || r.Host != "172.18.0.7:8080" {
					t.Errorf("unexpected request %v %v", r.Host, r.URL)
				}
				if status == 301 {
					w.Header().Set("Location", "http://outside.invalid/")
				}
				w.WriteHeader(status)
			}))
			defer server.Close()
			transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
				if addr != "172.18.0.7:8080" {
					t.Errorf("unexpected dial to %s", addr)
					return nil, errors.New("off-target address")
				}
				return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
			}
			client := *maintenanceHTTPClient
			client.Transport = transport
			result := check.probe(t.Context(), c, &client)
			if result.ready != (status == 200) || result.regression != (status >= 300) || requests != 1 {
				t.Fatalf("%+v (%d requests)", result, requests)
			}
		})
	}
}
