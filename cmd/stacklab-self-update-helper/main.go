package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"stacklab/internal/store"
)

const (
	defaultDataDir      = "/var/lib/stacklab"
	defaultPackageName  = "stacklab"
	defaultHealthURL    = "http://127.0.0.1:8080/api/ready"
	defaultSystemdUnit  = "stacklab"
	defaultRuntimeKey   = "self_update_runtime_v1"
	defaultDatabaseName = "stacklab.db"
)

var stacklabEnvFilePath = "/etc/stacklab/stacklab.env"

type emittedError struct {
	error
}

type runtimeState struct {
	JobID            string     `json:"job_id,omitempty"`
	RequestedVersion string     `json:"requested_version,omitempty"`
	InstalledVersion string     `json:"installed_version,omitempty"`
	Result           string     `json:"result,omitempty"`
	Message          string     `json:"message,omitempty"`
	StartedAt        *time.Time `json:"started_at,omitempty"`
	FinishedAt       *time.Time `json:"finished_at,omitempty"`
	PendingFinalize  bool       `json:"pending_finalize"`
}

func main() {
	if len(os.Args) < 2 {
		failJSON(fmt.Errorf("usage: stacklab-self-update-helper run --db-path <path> --job-id <id>"))
	}

	switch os.Args[1] {
	case "probe":
		if err := probe(); err != nil {
			failJSON(err)
		}
	case "run":
		if err := run(os.Args[2:]); err != nil {
			var emitted *emittedError
			if errors.As(err, &emitted) {
				os.Exit(1)
			}
			failJSON(err)
		}
	default:
		failJSON(fmt.Errorf("unknown subcommand %q", os.Args[1]))
	}
}

func probe() error {
	fmt.Fprintln(os.Stdout, `{"result":"ok"}`)
	return nil
}

func run(args []string) error {
	var dbPath string
	var jobID string
	var packageName string
	var requestedVersion string
	var healthURL string
	var serviceUnit string
	var runtimeKey string
	var skipAPTUpdate bool

	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.StringVar(&dbPath, "db-path", "", "absolute path to sqlite database")
	flags.StringVar(&jobID, "job-id", "", "job id to update")
	flags.StringVar(&packageName, "package-name", defaultPackageName, "APT package name")
	flags.StringVar(&requestedVersion, "requested-version", "", "candidate version requested by the UI")
	flags.StringVar(&healthURL, "health-url", defaultHealthURL, "health endpoint to verify after upgrade")
	flags.StringVar(&serviceUnit, "service-unit", defaultSystemdUnit, "systemd unit name")
	flags.StringVar(&runtimeKey, "runtime-key", defaultRuntimeKey, "app_settings runtime key")
	flags.BoolVar(&skipAPTUpdate, "skip-apt-update", false, "skip apt-get update before install")
	if err := flags.Parse(args); err != nil {
		return err
	}

	if strings.TrimSpace(dbPath) == "" || strings.TrimSpace(jobID) == "" {
		return errors.New("db-path and job-id are required")
	}
	if err := validateRequestedVersion(requestedVersion); err != nil {
		return err
	}
	if err := validateRunPolicy(dbPath, packageName, healthURL, serviceUnit, runtimeKey); err != nil {
		return err
	}

	appStore, err := store.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open sqlite store: %w", err)
	}
	defer appStore.Close()

	job, err := appStore.JobByID(context.Background(), jobID)
	if err != nil {
		return fmt.Errorf("load self-update job: %w", err)
	}
	if job.Workflow == nil {
		return errors.New("self-update job workflow is missing")
	}

	now := time.Now().UTC()
	_ = saveRuntimeState(appStore, runtimeKey, runtimeState{
		JobID:            job.ID,
		RequestedVersion: requestedVersion,
		StartedAt:        &now,
		PendingFinalize:  false,
	})

	nextIndex := 0
	if !skipAPTUpdate {
		if err := runStep(appStore, &job, nextIndex, "Refreshing package index.", func() (string, error) {
			return runAPTUpdate()
		}); err != nil {
			return finalizeFailure(appStore, runtimeKey, job, packageName, requestedVersion, err)
		}
		nextIndex++
	}

	if err := runStep(appStore, &job, nextIndex, "Upgrading Stacklab package.", func() (string, error) {
		return runAPTUpgrade(packageName, requestedVersion)
	}); err != nil {
		return finalizeFailure(appStore, runtimeKey, job, packageName, requestedVersion, err)
	}
	nextIndex++

	if err := runStep(appStore, &job, nextIndex, "Verifying Stacklab restart.", func() (string, error) {
		return verifyRecovery(serviceUnit, healthURL)
	}); err != nil {
		return finalizeFailure(appStore, runtimeKey, job, packageName, requestedVersion, err)
	}

	installedVersion, _ := installedVersion(packageName)
	finishedAt := time.Now().UTC()
	job.State = "succeeded"
	job.FinishedAt = &finishedAt
	job.ErrorCode = ""
	job.ErrorMessage = ""
	if err := appStore.UpdateJob(context.Background(), job); err != nil {
		return err
	}
	if err := publishEvent(appStore, job, "job_finished", fmt.Sprintf("Stacklab updated successfully%s.", versionSuffix(installedVersion)), "", nil); err != nil {
		return err
	}

	if err := saveRuntimeState(appStore, runtimeKey, runtimeState{
		JobID:            job.ID,
		RequestedVersion: requestedVersion,
		InstalledVersion: installedVersion,
		Result:           "succeeded",
		Message:          "Stacklab updated successfully.",
		StartedAt:        job.StartedAt,
		FinishedAt:       &finishedAt,
		PendingFinalize:  true,
	}); err != nil {
		return err
	}

	emitResult(runtimeState{
		JobID:            job.ID,
		RequestedVersion: requestedVersion,
		InstalledVersion: installedVersion,
		Result:           "succeeded",
		Message:          "Stacklab updated successfully.",
		StartedAt:        job.StartedAt,
		FinishedAt:       &finishedAt,
		PendingFinalize:  true,
	})
	return nil
}

type selfUpdatePolicy struct {
	DBPath      string
	PackageName string
	HealthURL   string
	ServiceUnit string
	RuntimeKey  string
}

func validateRunPolicy(dbPath, packageName, healthURL, serviceUnit, runtimeKey string) error {
	policy, err := loadSelfUpdatePolicy()
	if err != nil {
		return err
	}

	cleanDBPath, err := cleanAbsolutePath(dbPath)
	if err != nil {
		return fmt.Errorf("db-path is invalid: %w", err)
	}
	if cleanDBPath != policy.DBPath {
		return fmt.Errorf("db-path %q is not allowed", dbPath)
	}
	if strings.TrimSpace(packageName) != policy.PackageName {
		return fmt.Errorf("package-name %q is not allowed", packageName)
	}
	if strings.TrimSpace(healthURL) != policy.HealthURL {
		return fmt.Errorf("health-url %q is not allowed", healthURL)
	}
	if strings.TrimSpace(serviceUnit) != policy.ServiceUnit {
		return fmt.Errorf("service-unit %q is not allowed", serviceUnit)
	}
	if strings.TrimSpace(runtimeKey) != policy.RuntimeKey {
		return fmt.Errorf("runtime-key %q is not allowed", runtimeKey)
	}
	return nil
}

func loadSelfUpdatePolicy() (selfUpdatePolicy, error) {
	values, err := loadStacklabEnvValues(stacklabEnvFilePath)
	if err != nil {
		return selfUpdatePolicy{}, err
	}

	dataDir := valueOrDefault(values["STACKLAB_DATA_DIR"], defaultDataDir)
	dbPath := valueOrDefault(values["STACKLAB_DATABASE_PATH"], filepath.Join(dataDir, defaultDatabaseName))
	cleanDBPath, err := cleanAbsolutePath(dbPath)
	if err != nil {
		return selfUpdatePolicy{}, fmt.Errorf("configured database path is invalid: %w", err)
	}

	packageName := valueOrDefault(values["STACKLAB_SELF_UPDATE_PACKAGE_NAME"], defaultPackageName)
	if !safeAPTToken(packageName) {
		return selfUpdatePolicy{}, fmt.Errorf("configured package name %q is invalid", packageName)
	}

	healthURL := valueOrDefault(values["STACKLAB_SELF_UPDATE_HEALTH_URL"], defaultHealthURL)
	if err := validateHealthURL(healthURL); err != nil {
		return selfUpdatePolicy{}, fmt.Errorf("configured health URL is invalid: %w", err)
	}

	serviceUnit := valueOrDefault(values["STACKLAB_SYSTEMD_UNIT"], defaultSystemdUnit)
	if !safeSystemdUnit(serviceUnit) {
		return selfUpdatePolicy{}, fmt.Errorf("configured service unit %q is invalid", serviceUnit)
	}

	return selfUpdatePolicy{
		DBPath:      cleanDBPath,
		PackageName: packageName,
		HealthURL:   healthURL,
		ServiceUnit: serviceUnit,
		RuntimeKey:  defaultRuntimeKey,
	}, nil
}

func validateRequestedVersion(version string) error {
	version = strings.TrimSpace(version)
	if version == "" {
		return nil
	}
	if !safeAPTToken(version) {
		return fmt.Errorf("requested-version %q is invalid", version)
	}
	return nil
}

func validateHealthURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil {
		return err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("URL must use http or https")
	}
	if parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return errors.New("URL must be absolute and must not include userinfo or fragments")
	}
	return nil
}

func safeAPTToken(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && !strings.HasPrefix(value, "-") && !strings.ContainsAny(value, "/\\ \t\r\n\x00")
}

func safeSystemdUnit(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && !strings.HasPrefix(value, "-") && !strings.ContainsAny(value, "/\\ \t\r\n\x00")
}

func loadStacklabEnvValues(path string) (map[string]string, error) {
	values := map[string]string{}
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return values, nil
		}
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		values[key] = strings.TrimSpace(value)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return values, nil
}

func valueOrDefault(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func cleanAbsolutePath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if !filepath.IsAbs(path) {
		return "", errors.New("path must be absolute")
	}
	return filepath.Clean(path), nil
}

func runStep(appStore *store.Store, job *store.Job, index int, message string, run func() (string, error)) error {
	workflow := cloneWorkflow(job.Workflow.Steps)
	workflow = markWorkflowRunning(workflow, index)
	job.Workflow = &store.JobWorkflow{Steps: workflow}
	if err := appStore.UpdateJob(context.Background(), *job); err != nil {
		return err
	}
	if err := publishEvent(appStore, *job, "job_step_started", message, "", stepRef(workflow, index)); err != nil {
		return err
	}

	output, err := run()
	if strings.TrimSpace(output) != "" {
		if logErr := publishEvent(appStore, *job, "job_log", "Step output.", strings.TrimSpace(output), stepRef(workflow, index)); logErr != nil {
			return logErr
		}
	}
	if err != nil {
		workflow = markWorkflowFailed(workflow, index)
		job.Workflow = &store.JobWorkflow{Steps: workflow}
		_ = appStore.UpdateJob(context.Background(), *job)
		_ = publishEvent(appStore, *job, "job_error", err.Error(), "", stepRef(workflow, index))
		return err
	}

	workflow = markWorkflowSucceeded(workflow, index)
	if index+1 < len(workflow) {
		workflow = markWorkflowQueued(workflow, index+1)
	}
	job.Workflow = &store.JobWorkflow{Steps: workflow}
	if err := appStore.UpdateJob(context.Background(), *job); err != nil {
		return err
	}
	return publishEvent(appStore, *job, "job_step_finished", "Step finished successfully.", "", stepRef(workflow, index))
}

func finalizeFailure(appStore *store.Store, runtimeKey string, job store.Job, packageName, requestedVersion string, stepErr error) error {
	finishedAt := time.Now().UTC()
	job.State = "failed"
	job.FinishedAt = &finishedAt
	job.ErrorCode = "self_update_failed"
	job.ErrorMessage = stepErr.Error()
	if err := appStore.UpdateJob(context.Background(), job); err != nil {
		return err
	}
	if err := publishEvent(appStore, job, "job_finished", "Stacklab self-update failed.", "", nil); err != nil {
		return err
	}
	installedVersion, _ := installedVersion(packageName)
	if err := saveRuntimeState(appStore, runtimeKey, runtimeState{
		JobID:            job.ID,
		RequestedVersion: requestedVersion,
		InstalledVersion: installedVersion,
		Result:           "failed",
		Message:          stepErr.Error(),
		StartedAt:        job.StartedAt,
		FinishedAt:       &finishedAt,
		PendingFinalize:  true,
	}); err != nil {
		return err
	}
	emitResult(runtimeState{
		JobID:            job.ID,
		RequestedVersion: requestedVersion,
		InstalledVersion: installedVersion,
		Result:           "failed",
		Message:          stepErr.Error(),
		StartedAt:        job.StartedAt,
		FinishedAt:       &finishedAt,
		PendingFinalize:  true,
	})
	return &emittedError{stepErr}
}

func runAPTUpdate() (string, error) {
	cmd := exec.Command("apt-get", "update")
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("apt-get update failed: %w", err)
	}
	return string(output), nil
}

func runAPTUpgrade(packageName, requestedVersion string) (string, error) {
	cmd := exec.Command("apt-get", aptUpgradeArgs(packageName, requestedVersion)...)
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("apt-get install failed: %w", err)
	}
	return string(output), nil
}

func aptUpgradeArgs(packageName, requestedVersion string) []string {
	target := strings.TrimSpace(packageName)
	if target == "" {
		target = "stacklab"
	}
	if version := strings.TrimSpace(requestedVersion); version != "" {
		target += "=" + version
	}
	return []string{"install", "-y", "--only-upgrade", "-o", "Dpkg::Options::=--force-confold", "--", target}
}

func verifyRecovery(serviceUnit, healthURL string) (string, error) {
	output := []string{}

	show := exec.Command("systemctl", "is-active", serviceUnit)
	serviceOutput, err := show.CombinedOutput()
	output = append(output, strings.TrimSpace(string(serviceOutput)))
	if err != nil || strings.TrimSpace(string(serviceOutput)) != "active" {
		return strings.Join(output, "\n"), fmt.Errorf("service %s is not active", serviceUnit)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	var lastErr error
	for i := 0; i < 20; i++ {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, healthURL, nil)
		if err != nil {
			return strings.Join(output, "\n"), err
		}
		resp, err := client.Do(req)
		if err == nil && resp != nil {
			_ = resp.Body.Close()
		}
		if err == nil && resp != nil && resp.StatusCode >= 200 && resp.StatusCode < 300 {
			output = append(output, "health check passed")
			return strings.Join(output, "\n"), nil
		}
		if err != nil {
			lastErr = err
		} else if resp != nil {
			lastErr = fmt.Errorf("health returned status %d", resp.StatusCode)
		}
		time.Sleep(2 * time.Second)
	}
	if lastErr == nil {
		lastErr = errors.New("health check failed")
	}
	return strings.Join(output, "\n"), lastErr
}

func installedVersion(packageName string) (string, error) {
	output, err := exec.Command("dpkg-query", "-W", "-f=${Version}\n", packageName).CombinedOutput()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func cloneWorkflow(steps []store.JobWorkflowStep) []store.JobWorkflowStep {
	return append([]store.JobWorkflowStep(nil), steps...)
}

func markWorkflowRunning(steps []store.JobWorkflowStep, index int) []store.JobWorkflowStep {
	steps[index].State = "running"
	return steps
}

func markWorkflowSucceeded(steps []store.JobWorkflowStep, index int) []store.JobWorkflowStep {
	steps[index].State = "succeeded"
	return steps
}

func markWorkflowFailed(steps []store.JobWorkflowStep, index int) []store.JobWorkflowStep {
	steps[index].State = "failed"
	return steps
}

func markWorkflowQueued(steps []store.JobWorkflowStep, index int) []store.JobWorkflowStep {
	steps[index].State = "queued"
	return steps
}

func stepRef(steps []store.JobWorkflowStep, index int) *store.JobEventStep {
	return &store.JobEventStep{
		Index:         index + 1,
		Total:         len(steps),
		Action:        steps[index].Action,
		TargetStackID: steps[index].TargetStackID,
	}
}

func publishEvent(appStore *store.Store, job store.Job, eventType, message, data string, step *store.JobEventStep) error {
	sequence, err := appStore.NextJobEventSequence(context.Background(), job.ID)
	if err != nil {
		return err
	}
	return appStore.CreateJobEvent(context.Background(), store.JobEvent{
		JobID:     job.ID,
		Sequence:  sequence,
		Event:     eventType,
		State:     job.State,
		Message:   message,
		Data:      data,
		Step:      step,
		Timestamp: time.Now().UTC(),
	})
}

func saveRuntimeState(appStore *store.Store, runtimeKey string, state runtimeState) error {
	payload, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return appStore.SetAppSetting(context.Background(), runtimeKey, string(payload), time.Now().UTC())
}

func emitResult(state runtimeState) {
	encoded, err := json.Marshal(state)
	if err != nil {
		fmt.Fprintln(os.Stdout, "{}")
		return
	}
	fmt.Fprintln(os.Stdout, string(encoded))
}

func failJSON(err error) {
	emitResult(runtimeState{
		Result:     "failed",
		Message:    err.Error(),
		FinishedAt: ptrTime(time.Now().UTC()),
	})
	os.Exit(1)
}

func ptrTime(value time.Time) *time.Time {
	return &value
}

func versionSuffix(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return ""
	}
	return " to " + version
}
