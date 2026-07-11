package selfupdate

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"stacklab/internal/config"
	"stacklab/internal/jobs"
	"stacklab/internal/store"
)

func TestReconcilePendingResultFinalizesRuntimeState(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	directory := t.TempDir()
	appStore, err := store.Open(filepath.Join(directory, "stacklab.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = appStore.Close() })
	jobService := jobs.NewService(appStore)
	job, err := jobService.Start(ctx, "alpha", "up", "operator")
	if err != nil {
		t.Fatalf("jobs.Start() error = %v", err)
	}
	job, err = jobService.FinishSucceeded(ctx, job)
	if err != nil {
		t.Fatalf("jobs.FinishSucceeded() error = %v", err)
	}

	service := NewService(config.Config{}, appStore, jobService, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	startedAt := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	finishedAt := startedAt.Add(time.Minute)
	state := runtimeState{
		JobID: job.ID, RequestedVersion: "2026.07.12", InstalledVersion: "2026.07.12",
		Result: "succeeded", Message: "Update installed.", StartedAt: &startedAt, FinishedAt: &finishedAt,
		PendingFinalize: true,
	}
	if err := service.saveRuntimeState(ctx, state); err != nil {
		t.Fatalf("saveRuntimeState() error = %v", err)
	}

	service.reconcilePendingResult(ctx)
	reconciled, err := service.loadRuntimeState(ctx)
	if err != nil {
		t.Fatalf("loadRuntimeState() error = %v", err)
	}
	if reconciled.PendingFinalize || reconciled.JobID != job.ID || reconciled.Result != "succeeded" {
		t.Fatalf("reconciled runtime state = %#v", reconciled)
	}
	view := runtimeStatusView(reconciled)
	if view == nil || view.PendingFinalize || view.JobID != job.ID || view.InstalledVersion != "2026.07.12" || view.FinishedAt == nil {
		t.Fatalf("runtimeStatusView() = %#v", view)
	}
	if runtimeStatusView(runtimeState{}) != nil {
		t.Fatal("runtimeStatusView(empty) returned non-nil status")
	}

	// Missing jobs and non-pending states remain untouched and are safe to
	// retry on the next process poll.
	missing := runtimeState{JobID: "missing", PendingFinalize: true, Result: "failed"}
	if err := service.saveRuntimeState(ctx, missing); err != nil {
		t.Fatalf("saveRuntimeState(missing) error = %v", err)
	}
	service.reconcilePendingResult(ctx)
	unchanged, err := service.loadRuntimeState(ctx)
	if err != nil || !unchanged.PendingFinalize {
		t.Fatalf("missing-job runtime state = %#v, %v", unchanged, err)
	}
	unchanged.PendingFinalize = false
	if err := service.saveRuntimeState(ctx, unchanged); err != nil {
		t.Fatalf("saveRuntimeState(non-pending) error = %v", err)
	}
	service.reconcilePendingResult(ctx)
}

func TestBackgroundReconciliationRunsOnIntervalAndStops(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	directory := t.TempDir()
	appStore, err := store.Open(filepath.Join(directory, "stacklab.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = appStore.Close() })
	jobService := jobs.NewService(appStore)
	job, err := jobService.Start(ctx, "alpha", "up", "operator")
	if err != nil {
		t.Fatalf("jobs.Start() error = %v", err)
	}
	job, err = jobService.FinishSucceeded(ctx, job)
	if err != nil {
		t.Fatalf("jobs.FinishSucceeded() error = %v", err)
	}
	service := NewService(config.Config{}, appStore, jobService, nil, nil, nil)
	service.pollInterval = 5 * time.Millisecond

	backgroundCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		service.RunBackground(backgroundCtx)
		close(done)
	}()
	// Let the immediate pass and at least one empty interval pass before
	// introducing pending work. Its completion must therefore come from a
	// subsequent ticker iteration.
	time.Sleep(15 * time.Millisecond)
	if err := service.saveRuntimeState(ctx, runtimeState{JobID: job.ID, Result: "succeeded", PendingFinalize: true}); err != nil {
		t.Fatalf("saveRuntimeState() error = %v", err)
	}
	waitForRuntimeState(t, service, func(state runtimeState) bool { return !state.PendingFinalize })
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("RunBackground() did not stop after cancellation")
	}

	cancelledCtx, cancelImmediately := context.WithCancel(context.Background())
	cancelImmediately()
	service.StartBackground(cancelledCtx)
}

func TestPackageInspectionAndHelperCommandFailures(t *testing.T) {
	t.Parallel()

	service := newTestService(t, config.Config{})
	genericErr := errors.New("package manager unavailable")
	service.runCommand = func(context.Context, string, ...string) ([]byte, error) { return nil, genericErr }
	if version, installed, err := service.installedVersion(context.Background(), "stacklab"); version != "" || installed || !errors.Is(err, genericErr) {
		t.Fatalf("installedVersion(generic error) = %q, %t, %v", version, installed, err)
	}

	outputs := []struct {
		name      string
		output    string
		err       error
		version   string
		installed bool
	}{
		{name: "not installed", err: &exec.ExitError{}},
		{name: "empty", output: ""},
		{name: "malformed", output: "ii"},
		{name: "wrong status", output: "rc 2026.07.1"},
		{name: "installed", output: "ii 2026.07.1", version: "2026.07.1", installed: true},
	}
	for _, test := range outputs {
		t.Run(test.name, func(t *testing.T) {
			service.runCommand = func(context.Context, string, ...string) ([]byte, error) {
				return []byte(test.output), test.err
			}
			version, installed, err := service.installedVersion(context.Background(), "stacklab")
			if err != nil || version != test.version || installed != test.installed {
				t.Fatalf("installedVersion() = %q, %t, %v", version, installed, err)
			}
		})
	}

	service.runCommand = func(context.Context, string, ...string) ([]byte, error) {
		return []byte("stacklab:\n  Installed: 1\n"), nil
	}
	if candidate, err := service.candidateVersion(context.Background(), "stacklab"); err != nil || candidate != "" {
		t.Fatalf("candidateVersion(no candidate) = %q, %v", candidate, err)
	}
	service.runCommand = func(context.Context, string, ...string) ([]byte, error) { return nil, genericErr }
	if _, err := service.candidateVersion(context.Background(), "stacklab"); !errors.Is(err, genericErr) {
		t.Fatalf("candidateVersion(error) = %v", err)
	}

	status := service.inspectPackageStatus(context.Background())
	if status.Supported || status.Message != packageManagerErrorMessage || status.Name != defaultPackageName {
		t.Fatalf("inspectPackageStatus(error) = %#v", status)
	}

	if _, _, err := service.helperCommand("probe"); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("helperCommand(unconfigured) error = %v", err)
	}
	if _, _, err := service.systemdRunHelperCommandNoSudo("unit", false, false, "probe"); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("systemdRunHelperCommandNoSudo(unconfigured) error = %v", err)
	}
}

func TestWriteCapabilityClassifiesHelperProbeFailures(t *testing.T) {
	t.Parallel()

	helperPath := filepath.Join(t.TempDir(), "stacklab-self-update-helper")
	if err := os.WriteFile(helperPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(helper) error = %v", err)
	}
	service := newTestService(t, config.Config{SelfUpdateHelperPath: helperPath, SelfUpdateUseSudo: true})
	pkg := PackageStatus{Supported: true, Name: "stacklab"}

	tests := []struct {
		name       string
		output     string
		wantReason string
	}{
		{name: "no new privileges", output: "No New Privileges prevents execution", wantReason: "NoNewPrivileges=false"},
		{name: "password required", output: "sudo: a password is required", wantReason: "sudoers"},
		{name: "not allowed", output: "user is not allowed to execute", wantReason: "sudoers"},
		{name: "empty", output: "", wantReason: "could not be executed"},
		{name: "custom", output: "transient unit failed", wantReason: "transient unit failed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service.runCommand = func(context.Context, string, ...string) ([]byte, error) {
				return []byte(test.output), errors.New("probe failed")
			}
			capability := service.writeCapability(pkg)
			if capability.Supported || !strings.Contains(capability.Reason, test.wantReason) {
				t.Fatalf("writeCapability() = %#v, want reason containing %q", capability, test.wantReason)
			}
		})
	}

	service.runCommand = func(context.Context, string, ...string) ([]byte, error) { return []byte(`{"result":"ok"}`), nil }
	if capability := service.writeCapability(pkg); !capability.Supported || capability.Reason != "" {
		t.Fatalf("writeCapability(success) = %#v", capability)
	}
	if capability := service.writeCapability(PackageStatus{Supported: false, Message: "custom unsupported"}); capability.Supported || capability.Reason != "custom unsupported" {
		t.Fatalf("writeCapability(package message) = %#v", capability)
	}
	if capability := service.writeCapability(PackageStatus{}); capability.Supported || capability.Reason != unsupportedInstallMessage {
		t.Fatalf("writeCapability(default unsupported) = %#v", capability)
	}

	directoryService := newTestService(t, config.Config{SelfUpdateHelperPath: t.TempDir(), SelfUpdateUseSudo: true})
	if capability := directoryService.writeCapability(pkg); capability.Supported || !strings.Contains(capability.Reason, "unavailable") {
		t.Fatalf("writeCapability(directory) = %#v", capability)
	}
}

func TestApplyReportsHelperStartupFailureAndClearsRuntime(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	appStore, err := store.Open(filepath.Join(directory, "stacklab.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = appStore.Close() })
	helperPath := filepath.Join(directory, "stacklab-self-update-helper")
	if err := os.WriteFile(helperPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(helper) error = %v", err)
	}
	jobService := jobs.NewService(appStore)
	var terminal store.Job
	jobService.SetTerminalHook(func(job store.Job) { terminal = job })
	service := NewService(config.Config{
		DatabasePath: filepath.Join(directory, "stacklab.db"), SelfUpdatePackageName: "stacklab",
		SelfUpdateHelperPath: helperPath, SelfUpdateUseSudo: true,
	}, appStore, jobService, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	sudoCalls := 0
	service.runCommand = func(_ context.Context, name string, args ...string) ([]byte, error) {
		switch name {
		case "dpkg-query":
			return []byte("ii 2026.07.1\n"), nil
		case "env":
			return []byte("Candidate: 2026.07.2\n"), nil
		case "sudo":
			sudoCalls++
			if sudoCalls == 1 {
				return []byte(`{"result":"ok"}`), nil
			}
			return []byte("systemd rejected transient unit"), errors.New("exit status 1")
		default:
			t.Fatalf("unexpected command %s %#v", name, args)
			return nil, nil
		}
	}

	_, err = service.Apply(context.Background(), ApplyRequest{ExpectedCandidateVersion: "2026.07.2"}, "operator")
	if err == nil || !strings.Contains(err.Error(), "systemd rejected transient unit") {
		t.Fatalf("Apply() error = %v", err)
	}
	if terminal.State != "failed" || terminal.ErrorCode != "self_update_start_failed" {
		t.Fatalf("terminal self-update job = %#v", terminal)
	}
	state, err := service.loadRuntimeState(context.Background())
	if err != nil {
		t.Fatalf("loadRuntimeState() error = %v", err)
	}
	if !reflect.DeepEqual(state, runtimeState{}) {
		t.Fatalf("runtime state after helper failure = %#v", state)
	}
}

func TestRuntimeStateLoadErrorsAreReported(t *testing.T) {
	t.Parallel()

	service := newTestService(t, config.Config{})
	service.runCommand = func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("package manager unavailable")
	}
	if state, err := service.loadRuntimeState(context.Background()); err != nil || !reflect.DeepEqual(state, runtimeState{}) {
		t.Fatalf("loadRuntimeState(missing) = %#v, %v", state, err)
	}
	if err := service.store.SetAppSetting(context.Background(), runtimeStateKey, "{invalid", time.Now()); err != nil {
		t.Fatalf("SetAppSetting(invalid runtime) error = %v", err)
	}
	if _, err := service.Overview(context.Background()); err == nil || !strings.Contains(err.Error(), "parse self-update runtime state") {
		t.Fatalf("Overview(invalid runtime) error = %v", err)
	}
}

func waitForRuntimeState(t *testing.T, service *Service, predicate func(runtimeState) bool) runtimeState {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		state, err := service.loadRuntimeState(context.Background())
		if err != nil {
			t.Fatalf("loadRuntimeState() error = %v", err)
		}
		if predicate(state) {
			return state
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for runtime state")
	return runtimeState{}
}
