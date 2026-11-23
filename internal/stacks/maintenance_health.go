package stacks

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
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
	configDigest string
	serviceNames []string
	targets      map[string]maintenanceHealthTarget
	before       map[string]maintenanceServiceHealth
}

// MaintenanceHealthReport records service outcomes and names any running services
// whose readiness could not be checked because their containers have no probe.
type MaintenanceHealthReport struct {
	Summary            string
	UnverifiedServices []string
	FailedServices     []string
	verified           map[string]maintenanceServiceHealth
}

type maintenanceHealthTarget struct {
	image               string
	replicas            int
	explicitScale       bool
	requiresNative      bool
	completionExpected  bool
	httpCheck           *maintenanceHTTPCheck
	externalDefinitions bool
	namedVolumes        map[string]string
	networks            []string
}

type maintenanceServiceHealth struct {
	summary         string
	ready           bool
	unverified      bool
	regression      bool
	imageID         string
	runningImageIDs []string
	configHash      string
	resourceDrift   bool
	imageRef        string
	containerCount  int
	native          int
	instances       int
	identity        string
}

func (s *ServiceReader) CaptureMaintenanceHealth(ctx context.Context, stackID string, serviceNames []string) (MaintenanceHealthBaseline, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var baseline MaintenanceHealthBaseline
	if err := s.validateStackRoot(stackID, false); err != nil {
		return baseline, "", err
	}
	resolved, err := s.maintenanceHealthConfig(ctx, stackID, serviceNames)
	if err != nil {
		return baseline, "", err
	}
	project, targets, err := parseMaintenanceHealthTargets(resolved, serviceNames)
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
	baseline = MaintenanceHealthBaseline{stackID: stackID, project: project, configDigest: contentSHA256(string(resolved)), serviceNames: slices.Clone(serviceNames), targets: targets, before: before}
	return baseline, "Before deployment: " + describeMaintenanceHealth(before), nil
}

func (s *ServiceReader) maintenanceHealthConfig(ctx context.Context, stackID string, serviceNames []string) ([]byte, error) {
	root, err := filepath.Abs(s.cfg.RootDir)
	if err != nil {
		return nil, err
	}
	paths := stackPaths(root, stackID)
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
		return nil, errors.New("could not resolve maintenance health targets from Compose")
	}
	return output.Bytes(), nil
}

func parseMaintenanceHealthTargets(content []byte, names []string) (string, map[string]maintenanceHealthTarget, error) {
	var config struct {
		Name    string `yaml:"name"`
		Volumes map[string]struct {
			Name string `yaml:"name"`
		} `yaml:"volumes"`
		Networks map[string]struct {
			Name string `yaml:"name"`
		} `yaml:"networks"`
		Services map[string]struct {
			Image  string `yaml:"image"`
			Scale  *int   `yaml:"scale"`
			Deploy struct {
				Replicas *int `yaml:"replicas"`
			} `yaml:"deploy"`
			Volumes     []any             `yaml:"volumes"`
			Networks    any               `yaml:"networks"`
			Configs     []any             `yaml:"configs"`
			Secrets     []any             `yaml:"secrets"`
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
		target := maintenanceHealthTarget{
			image:               service.Image,
			replicas:            replicas,
			explicitScale:       service.Deploy.Replicas != nil || service.Scale != nil,
			requiresNative:      !service.Healthcheck.Disable && len(healthTest) > 0 && healthTest[0] != "NONE",
			completionExpected:  completion[name],
			httpCheck:           check,
			externalDefinitions: len(service.Configs) > 0 || len(service.Secrets) > 0,
		}
		// Service hashes contain logical resource references. Check project-level
		// Docker names separately; a renamed volume could select different data.
		for _, mount := range service.Volumes {
			fields, _ := mount.(map[string]any)
			if fields["type"] != "volume" {
				continue
			}
			src, _ := fields["source"].(string)
			dst, _ := fields["target"].(string)
			if src != "" && dst != "" && config.Volumes[src].Name != "" {
				if target.namedVolumes == nil {
					target.namedVolumes = map[string]string{}
				}
				target.namedVolumes[dst] = config.Volumes[src].Name
			}
		}
		networks, _ := service.Networks.(map[string]any)
		for key := range networks {
			if n := config.Networks[key].Name; n != "" {
				target.networks = append(target.networks, n)
			}
		}
		targets[name] = target
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
					if found && last.regression && health.regression {
						report.FailedServices = append(report.FailedServices, name)
					}
					if !found || !last.ready || !health.ready || health.identity != last.identity {
						stable = false
					}
				}
				if stable {
					report.verified = current
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
	byService, err := inspectMaintenanceTargets(ctx, project, targets)
	if err != nil {
		return nil, err
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

func inspectMaintenanceTargets(ctx context.Context, project string, targets map[string]maintenanceHealthTarget) (map[string][]dockerInspectContainer, error) {
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
	return byService, nil
}

type maintenanceHTTPResult struct {
	note       string
	ready      bool
	regression bool
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
				result := target.httpCheck.probe(ctx, c, maintenanceHTTPClient)
				mu.Lock()
				results[c.ID] = result
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
	health := maintenanceServiceHealth{ready: true, containerCount: len(containers)}
	if len(containers) > 0 {
		health.imageID = containers[0].Image
		health.imageRef = containers[0].Config.Image
		health.configHash = containers[0].Config.Labels["com.docker.compose.config-hash"]
	}
	expected := target.replicas
	if !target.explicitScale {
		expected = max(expected, before.instances)
	}
	notes := make(map[string]int)
	ids := make([]string, 0, len(containers))
	if len(containers) < expected {
		health.ready = false
		health.regression = true
		notes["missing container"] = expected - len(containers)
	}
	for _, c := range containers {
		for _, name := range target.networks {
			if _, ok := c.NetworkSettings.Networks[name]; !ok {
				health.resourceDrift = true
			}
		}
		for destination, name := range target.namedVolumes {
			present := slices.ContainsFunc(c.Mounts, func(m struct {
				Type        string
				Name        string
				Destination string
			}) bool {
				return m.Type == "volume" && m.Name == name && m.Destination == destination
			})
			if !present {
				health.resourceDrift = true
			}
		}
		if c.Config.Labels["com.docker.compose.config-hash"] != health.configHash {
			health.configHash = ""
		}
		if c.Config.Image != health.imageRef {
			health.imageRef = ""
		}
		if c.Image != health.imageID {
			health.imageID = ""
		}
		ids = append(ids, c.ID+"/"+strconv.Itoa(c.RestartCount)+"/"+c.State.StartedAt)
		hasNative := c.State.Health != nil
		if hasNative && c.State.Status == "running" {
			health.native++
		}
		if c.State.Status == "running" {
			health.runningImageIDs = append(health.runningImageIDs, c.Image)
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
			health.regression = true
			note = fmt.Sprintf("exited (%d)", c.State.ExitCode)
		case c.State.Status != "running":
			health.regression = health.regression || c.State.Status == "restarting" || c.State.Status == "dead"
			note = "container " + normalizeContainerStatus(c.State.Status)
		case hasNative && c.State.Health.Status != "healthy":
			switch c.State.Health.Status {
			case "starting", "unhealthy":
				health.regression = health.regression || c.State.Health.Status == "unhealthy"
				note = c.State.Health.Status
			default:
				note = "unknown Docker health status"
			}
		case !hasNative && target.requiresNative:
			health.regression = true
			note = "configured Docker healthcheck missing"
		case target.httpCheck != nil:
			if result, found := httpResults[c.ID]; found {
				note, ready = result.note, result.ready
				health.regression = health.regression || result.regression
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
		health.regression = true
		health.ready = false
	}
	parts := make([]string, 0, len(notes))
	for note, count := range notes {
		parts = append(parts, fmt.Sprintf("%d %s", count, note))
	}
	sort.Strings(parts)
	sort.Strings(ids)
	slices.Sort(health.runningImageIDs)
	health.runningImageIDs = slices.Compact(health.runningImageIDs)
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
