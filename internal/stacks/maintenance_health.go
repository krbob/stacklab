package stacks

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"stacklab/internal/limitedio"
)

const (
	maintenanceHealthWindow = 3 * time.Minute
	maintenanceHealthPoll   = 2 * time.Second
)

// MaintenanceHealthBaseline belongs to a single deployment and its target services.
type MaintenanceHealthBaseline struct {
	stackID      string
	project      string
	serviceNames []string
	targets      map[string]maintenanceHealthTarget
	before       map[string]maintenanceServiceHealth
}

// MaintenanceHealthReport records service outcomes and names any running services
// whose readiness could not be checked because their containers have no probe.
type MaintenanceHealthReport struct {
	Summary            string
	UnverifiedServices []string
}

type maintenanceHealthTarget struct {
	replicas           int
	explicitScale      bool
	requiresNative     bool
	completionExpected bool
	httpCheck          *maintenanceHTTPCheck
}

type maintenanceServiceHealth struct {
	summary    string
	ready      bool
	unverified bool
	native     int
	instances  int
	identity   string
}

func (s *ServiceReader) CaptureMaintenanceHealth(ctx context.Context, stackID string, serviceNames []string) (MaintenanceHealthBaseline, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var baseline MaintenanceHealthBaseline
	if err := s.validateStackRoot(stackID, false); err != nil {
		return baseline, "", err
	}
	paths := stackPaths(s.cfg.RootDir, stackID)
	command, args := composeCommand(ctx, paths.RootPath, paths.ComposeFilePath, paths.EnvFilePath)
	if len(serviceNames) > 0 {
		// Explicitly selected services can activate profiles in Compose up.
		args = append(args, "--profile", "*")
	}
	cmd := exec.CommandContext(ctx, command, append(args, "config")...)
	cmd.Dir = paths.RootPath
	// Resolved Compose output contains interpolated secrets. Keep it out of diagnostics.
	output := limitedio.NewBuffer(MaxComposeOutputBytes)
	cmd.Stdout = output
	if err := cmd.Run(); err != nil || output.Err() != nil {
		return baseline, "", errors.New("could not resolve maintenance health targets from Compose")
	}
	project, targets, err := parseMaintenanceHealthTargets(output.Bytes(), serviceNames)
	if err != nil {
		return baseline, "", err
	}
	if project == "" {
		project = stackID
	}
	before, err := observeMaintenanceHealth(ctx, project, targets, nil)
	if err != nil {
		return baseline, "", err
	}
	baseline = MaintenanceHealthBaseline{stackID: stackID, project: project, serviceNames: slices.Clone(serviceNames), targets: targets, before: before}
	return baseline, "Before deployment: " + describeMaintenanceHealth(before), nil
}

func parseMaintenanceHealthTargets(content []byte, names []string) (string, map[string]maintenanceHealthTarget, error) {
	var config struct {
		Name     string `yaml:"name"`
		Services map[string]struct {
			Scale  *int `yaml:"scale"`
			Deploy struct {
				Replicas *int `yaml:"replicas"`
			} `yaml:"deploy"`
			DependsOn   any               `yaml:"depends_on"`
			Labels      map[string]string `yaml:"labels"`
			Healthcheck struct {
				Test    []string `yaml:"test"`
				Disable bool     `yaml:"disable"`
			} `yaml:"healthcheck"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(content, &config); err != nil {
		return "", nil, errors.New("could not read resolved Compose health configuration")
	}
	targets := map[string]maintenanceHealthTarget{}
	completion := map[string]bool{}
	for _, service := range config.Services {
		// Older Compose versions can retain short-form dependencies; only
		// the mapping form carries a completion condition.
		dependencies, _ := service.DependsOn.(map[string]any)
		for name, value := range dependencies {
			dependency, _ := value.(map[string]any)
			if dependency["condition"] == "service_completed_successfully" {
				completion[name] = true
			}
		}
	}
	for name, service := range config.Services {
		if names != nil && !slices.Contains(names, name) {
			continue
		}
		replicas := 1
		if service.Deploy.Replicas != nil {
			replicas = *service.Deploy.Replicas
		} else if service.Scale != nil {
			replicas = *service.Scale
		}
		check, err := newMaintenanceHTTPCheck(service.Labels)
		if err != nil {
			return "", nil, fmt.Errorf("%s: %w", name, err)
		}
		healthTest := service.Healthcheck.Test
		targets[name] = maintenanceHealthTarget{
			replicas:           replicas,
			explicitScale:      service.Deploy.Replicas != nil || service.Scale != nil,
			requiresNative:     !service.Healthcheck.Disable && len(healthTest) > 0 && healthTest[0] != "NONE",
			completionExpected: completion[name],
			httpCheck:          check,
		}
	}
	for _, name := range names {
		if _, found := targets[name]; !found {
			return "", nil, fmt.Errorf("service %q is missing from the resolved Compose configuration", name)
		}
	}
	if len(targets) == 0 {
		return "", nil, errors.New("no active Compose service targets for health verification")
	}
	return config.Name, targets, nil
}

func (s *ServiceReader) VerifyMaintenanceHealth(ctx context.Context, stackID string, serviceNames []string, baseline MaintenanceHealthBaseline) (MaintenanceHealthReport, error) {
	if baseline.stackID != stackID || len(baseline.targets) == 0 || !slices.Equal(baseline.serviceNames, serviceNames) {
		return MaintenanceHealthReport{}, errors.New("missing or mismatched pre-deployment health baseline")
	}
	return waitForMaintenanceHealth(ctx, baseline, maintenanceHealthWindow, maintenanceHealthPoll, observeMaintenanceHealth)
}

type maintenanceObserver func(context.Context, string, map[string]maintenanceHealthTarget, map[string]maintenanceServiceHealth) (map[string]maintenanceServiceHealth, error)

func waitForMaintenanceHealth(ctx context.Context, baseline MaintenanceHealthBaseline, window, interval time.Duration, observe maintenanceObserver) (MaintenanceHealthReport, error) {
	waitCtx, cancel := context.WithTimeout(ctx, window)
	defer cancel()
	var previous map[string]maintenanceServiceHealth
	var report MaintenanceHealthReport
	for {
		current, err := observe(waitCtx, baseline.project, baseline.targets, baseline.before)
		if waitCtx.Err() == nil {
			switch {
			case err != nil:
				report = MaintenanceHealthReport{Summary: err.Error()}
				previous = nil
			default:
				report = MaintenanceHealthReport{Summary: describeMaintenanceHealth(current)}
				stable := len(current) == len(baseline.targets)
				for name, health := range current {
					if health.unverified {
						report.UnverifiedServices = append(report.UnverifiedServices, name)
					}
					last, found := previous[name]
					if !found || !last.ready || !health.ready || health.identity != last.identity {
						stable = false
					}
				}
				if stable {
					sort.Strings(report.UnverifiedServices)
					return report, nil
				}
				previous = current
			}
		}
		timer := time.NewTimer(interval)
		select {
		case <-waitCtx.Done():
			timer.Stop()
			if ctx.Err() != nil {
				return report, ctx.Err()
			}
			if report.Summary == "" {
				report.Summary = "health observations unavailable"
			}
			reason := report.Summary
			if len(previous) > 0 {
				failed := make(map[string]maintenanceServiceHealth)
				for name, health := range previous {
					if !health.ready {
						failed[name] = health
					}
				}
				reason = "containers did not remain stable across two observations"
				if len(failed) > 0 {
					reason = describeMaintenanceHealth(failed)
				}
			}
			return report, fmt.Errorf("health verification timed out: %s", reason)
		case <-timer.C:
		}
	}
}

func observeMaintenanceHealth(ctx context.Context, project string, targets map[string]maintenanceHealthTarget, before map[string]maintenanceServiceHealth) (map[string]maintenanceServiceHealth, error) {
	ps := exec.CommandContext(ctx, "docker", "ps", "-a", "--filter", "label=com.docker.compose.project="+project, "--format", `{{.ID}} {{.Label "com.docker.compose.service"}} {{.Label "com.docker.compose.oneoff"}}`)
	output, err := ps.Output()
	if err != nil {
		return nil, errors.New("could not list containers for health verification")
	}
	byService := make(map[string][]dockerInspectContainer, len(targets))
	var ids []string
	for line := range strings.SplitSeq(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		_, targeted := targets[fields[1]]
		if targeted && (len(fields) < 3 || !strings.EqualFold(fields[2], "true")) {
			ids = append(ids, fields[0])
		}
	}
	if len(ids) != 0 {
		inspected, err := inspectDockerContainers(ctx, ids)
		if err != nil || len(inspected) != len(ids) {
			return nil, errors.New("could not inspect all listed containers for health verification")
		}
		for _, container := range inspected {
			labels := container.Config.Labels
			if labels["com.docker.compose.project"] != project || strings.EqualFold(labels["com.docker.compose.oneoff"], "true") {
				continue
			}
			name := labels["com.docker.compose.service"]
			if _, ok := targets[name]; ok {
				byService[name] = append(byService[name], container)
			}
		}
	}
	httpResults := sampleMaintenanceHTTP(ctx, targets, byService)
	result := make(map[string]maintenanceServiceHealth, len(targets))
	for name, target := range targets {
		prior := before[name]
		result[name] = assessMaintenanceService(target, byService[name], prior, httpResults)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

type maintenanceHTTPResult struct {
	note  string
	ready bool
}

// Limit simultaneous requests across replicas and services on a stack.
func sampleMaintenanceHTTP(ctx context.Context, targets map[string]maintenanceHealthTarget, byService map[string][]dockerInspectContainer) map[string]maintenanceHTTPResult {
	results := map[string]maintenanceHTTPResult{}
	limit := make(chan struct{}, 8)
	var workers sync.WaitGroup
	var mu sync.Mutex
	for name, target := range targets {
		if target.httpCheck == nil || target.replicas == 0 || target.completionExpected {
			continue
		}
		for _, c := range byService[name] {
			if c.State.Status != "running" || (c.State.Health != nil && c.State.Health.Status != "healthy") {
				continue
			}
			select {
			case limit <- struct{}{}:
			case <-ctx.Done():
				workers.Wait()
				return results
			}
			workers.Go(func() {
				defer func() { <-limit }()
				note, ready := target.httpCheck.probe(ctx, c, maintenanceHTTPClient)
				mu.Lock()
				results[c.ID] = maintenanceHTTPResult{note, ready}
				mu.Unlock()
			})
		}
	}
	workers.Wait()
	return results
}

func assessMaintenanceService(target maintenanceHealthTarget, containers []dockerInspectContainer, before maintenanceServiceHealth, httpResults map[string]maintenanceHTTPResult) maintenanceServiceHealth {
	if target.replicas == 0 {
		return maintenanceServiceHealth{summary: "skipped (scaled to zero)", ready: true}
	}
	health := maintenanceServiceHealth{ready: true}
	expected := target.replicas
	if !target.explicitScale {
		expected = max(expected, before.instances)
	}
	notes := make(map[string]int)
	ids := make([]string, 0, len(containers))
	if len(containers) < expected {
		health.ready = false
		notes["missing container"] = expected - len(containers)
	}
	for _, c := range containers {
		ids = append(ids, c.ID+"/"+strconv.Itoa(c.RestartCount)+"/"+c.State.StartedAt)
		hasNative := c.State.Health != nil
		if hasNative && c.State.Status == "running" {
			health.native++
		}
		if c.State.Status == "running" || c.State.Status == "restarting" {
			health.instances++
		}
		var note string
		var ready bool
		switch {
		case target.completionExpected && c.State.Status == "exited" && c.State.ExitCode == 0:
			note, ready = "one-shot completed", true
		case target.completionExpected && c.State.Status == "running":
			note = "waiting for one-shot completion"
		case c.State.Status == "exited":
			note = fmt.Sprintf("exited (%d)", c.State.ExitCode)
		case c.State.Status != "running":
			note = "container " + normalizeContainerStatus(c.State.Status)
		case hasNative && c.State.Health.Status != "healthy":
			switch c.State.Health.Status {
			case "starting", "unhealthy":
				note = c.State.Health.Status
			default:
				note = "unknown Docker health status"
			}
		case !hasNative && target.requiresNative:
			note = "configured Docker healthcheck missing"
		case target.httpCheck != nil:
			if result, found := httpResults[c.ID]; found {
				note, ready = result.note, result.ready
			} else {
				note = "HTTP readiness unavailable"
			}
		case hasNative:
			note, ready = "healthy", true
		default:
			health.unverified = true
			note, ready = "running (readiness unverified, no healthcheck)", true
		}
		health.ready = health.ready && ready
		notes[note]++
	}
	if missingNative := min(before.native, expected) - health.native; missingNative > 0 && !target.completionExpected {
		notes["previous Docker healthcheck missing"] = missingNative
		health.ready = false
	}
	parts := make([]string, 0, len(notes))
	for note, count := range notes {
		parts = append(parts, fmt.Sprintf("%d %s", count, note))
	}
	sort.Strings(parts)
	sort.Strings(ids)
	health.identity = strings.Join(ids, ",")
	health.summary = strings.Join(parts, ", ")
	if before.summary != "" && !before.ready {
		health.summary += " (before deployment: " + before.summary + ")"
	}
	return health
}

func describeMaintenanceHealth(observations map[string]maintenanceServiceHealth) string {
	services := make([]string, 0, len(observations))
	for name, health := range observations {
		services = append(services, name+": "+health.summary)
	}
	sort.Strings(services)
	return strings.Join(services, "; ")
}
