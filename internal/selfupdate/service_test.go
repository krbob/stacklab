package selfupdate

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"stacklab/internal/config"
	"stacklab/internal/jobs"
	"stacklab/internal/store"
)

func TestParseAPTChannel(t *testing.T) {
	t.Parallel()

	cases := []struct {
		line string
		want string
	}{
		{line: "deb https://krbob.github.io/stacklab/apt stable main", want: "stable"},
		{line: "deb [arch=arm64 signed-by=/usr/share/keyrings/stacklab.gpg] https://krbob.github.io/stacklab/apt nightly main", want: "nightly"},
		{line: "deb https://example.com/other stable main", want: ""},
		{line: "# deb https://krbob.github.io/stacklab/apt stable main", want: ""},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.line, func(t *testing.T) {
			t.Parallel()
			if got := parseAPTChannel(tc.line); got != tc.want {
				t.Fatalf("parseAPTChannel(%q) = %q, want %q", tc.line, got, tc.want)
			}
		})
	}
}

func TestOverviewReportsPackageUpdateAvailability(t *testing.T) {
	t.Parallel()

	service := newTestService(t, config.Config{
		SelfUpdatePackageName: "stacklab",
	})
	service.runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		switch {
		case name == "dpkg-query":
			return []byte("ii\t2026.04.0\n"), nil
		case name == "env":
			wantArgs := []string{"LC_ALL=C", "LANG=C", "apt-cache", "policy", "stacklab"}
			if !reflect.DeepEqual(args, wantArgs) {
				t.Fatalf("env args = %v, want %v", args, wantArgs)
			}
			return []byte("stacklab:\n  Installed: 2026.04.0\n  Candidate: 2026.04.1\n"), nil
		default:
			t.Fatalf("unexpected command %s %v", name, args)
			return nil, nil
		}
	}

	response, err := service.Overview(context.Background())
	if err != nil {
		t.Fatalf("Overview() error = %v", err)
	}
	if !response.Package.Supported {
		t.Fatalf("Package.Supported = false, want true")
	}
	if response.Package.InstalledVersion != "2026.04.0" {
		t.Fatalf("InstalledVersion = %q, want %q", response.Package.InstalledVersion, "2026.04.0")
	}
	if response.Package.CandidateVersion != "2026.04.1" {
		t.Fatalf("CandidateVersion = %q, want %q", response.Package.CandidateVersion, "2026.04.1")
	}
	if !response.Package.UpdateAvailable {
		t.Fatalf("UpdateAvailable = false, want true")
	}
}

func TestOverviewParsesCandidateVersionIndependentlyOfHostLocale(t *testing.T) {
	t.Parallel()

	service := newTestService(t, config.Config{
		SelfUpdatePackageName: "stacklab",
	})
	service.runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		switch {
		case name == "dpkg-query":
			return []byte("ii\t2026.05.0~rc1\n"), nil
		case name == "env":
			wantArgs := []string{"LC_ALL=C", "LANG=C", "apt-cache", "policy", "stacklab"}
			if !reflect.DeepEqual(args, wantArgs) {
				t.Fatalf("env args = %v, want %v", args, wantArgs)
			}
			return []byte("stacklab:\n  Installed: 2026.05.0~rc1\n  Candidate: 2026.05.0~rc2\n"), nil
		default:
			t.Fatalf("unexpected command %s %v", name, args)
			return nil, nil
		}
	}

	response, err := service.Overview(context.Background())
	if err != nil {
		t.Fatalf("Overview() error = %v", err)
	}
	if response.Package.CandidateVersion != "2026.05.0~rc2" {
		t.Fatalf("CandidateVersion = %q, want %q", response.Package.CandidateVersion, "2026.05.0~rc2")
	}
	if !response.Package.UpdateAvailable {
		t.Fatalf("UpdateAvailable = false, want true")
	}
}

func TestWriteCapabilityRequiresSudoOrRoot(t *testing.T) {
	t.Parallel()

	helperPath := filepath.Join(t.TempDir(), "stacklab-self-update-helper")
	if err := os.WriteFile(helperPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(helper) error = %v", err)
	}

	service := newTestService(t, config.Config{
		SelfUpdateHelperPath: helperPath,
		SelfUpdateUseSudo:    false,
	})

	response := service.writeCapability(PackageStatus{
		Supported: true,
		Name:      "stacklab",
	})
	if response.Supported {
		t.Fatalf("writeCapability().Supported = true, want false")
	}
	if !strings.Contains(response.Reason, "requires sudo or a root-owned Stacklab service") {
		t.Fatalf("writeCapability().Reason = %q, want sudo/root hint", response.Reason)
	}
}

func TestSystemdRunHelperCommandUsesTransientUnit(t *testing.T) {
	t.Parallel()

	helperPath := "/usr/lib/stacklab/bin/stacklab-self-update-helper"
	service := newTestService(t, config.Config{
		SelfUpdateHelperPath: helperPath,
		SelfUpdateUseSudo:    true,
	})

	name, args, err := service.systemdRunHelperCommand(systemdRunUnitName("job_bad/id"), false, false, "run", "--job-id", "job_bad/id")
	if err != nil {
		t.Fatalf("systemdRunHelperCommand() error = %v", err)
	}

	wantArgs := []string{
		"-n",
		systemdRunPath,
		"--quiet",
		"--collect",
		"--unit=stacklab-self-update-job_bad_id",
		"--service-type=exec",
		helperPath,
		"run",
		"--job-id",
		"job_bad/id",
	}
	if name != "sudo" {
		t.Fatalf("command name = %q, want sudo", name)
	}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("command args = %#v, want %#v", args, wantArgs)
	}
}

func TestSystemdRunHelperCommandNoSudoUsesTransientUnitDirectly(t *testing.T) {
	t.Parallel()

	helperPath := "/usr/lib/stacklab/bin/stacklab-self-update-helper"
	service := newTestService(t, config.Config{
		SelfUpdateHelperPath: helperPath,
	})

	name, args, err := service.systemdRunHelperCommandNoSudo(systemdRunUnitName("job_123"), false, false, "run", "--job-id", "job_123")
	if err != nil {
		t.Fatalf("systemdRunHelperCommandNoSudo() error = %v", err)
	}

	wantArgs := []string{
		"--quiet",
		"--collect",
		"--unit=stacklab-self-update-job_123",
		"--service-type=exec",
		helperPath,
		"run",
		"--job-id",
		"job_123",
	}
	if name != systemdRunPath {
		t.Fatalf("command name = %q, want %q", name, systemdRunPath)
	}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("command args = %#v, want %#v", args, wantArgs)
	}
}

func TestHelperProbeCommandUsesWaitAndPipe(t *testing.T) {
	t.Parallel()

	helperPath := "/usr/lib/stacklab/bin/stacklab-self-update-helper"
	service := newTestService(t, config.Config{
		SelfUpdateHelperPath: helperPath,
		SelfUpdateUseSudo:    true,
	})

	name, args, err := service.helperProbeCommand("probe")
	if err != nil {
		t.Fatalf("helperProbeCommand() error = %v", err)
	}
	if name != "sudo" {
		t.Fatalf("command name = %q, want sudo", name)
	}
	for _, want := range []string{"-n", systemdRunPath, "--quiet", "--wait", "--collect", "--pipe", "--service-type=exec", helperPath, "probe"} {
		if !hasArg(args, want) {
			t.Fatalf("command args = %#v, want arg %q", args, want)
		}
	}
	foundProbeUnit := false
	for _, arg := range args {
		if strings.HasPrefix(arg, "--unit=stacklab-self-update-probe-") {
			foundProbeUnit = true
			break
		}
	}
	if !foundProbeUnit {
		t.Fatalf("command args = %#v, want probe unit", args)
	}
}

func TestApplyRejectsWhenSelfUpdateLockHeld(t *testing.T) {
	t.Parallel()

	appStore, err := store.Open(filepath.Join(t.TempDir(), "stacklab.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() {
		_ = appStore.Close()
	})

	helperPath := filepath.Join(t.TempDir(), "stacklab-self-update-helper")
	if err := os.WriteFile(helperPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(helper) error = %v", err)
	}
	jobService := jobs.NewService(appStore)
	if _, err := jobService.StartWithResources(context.Background(), "", "other_self_update_owner", "local", jobs.SelfUpdateResource()); err != nil {
		t.Fatalf("StartWithResources(lock holder) error = %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service := NewService(config.Config{
		DatabasePath:         filepath.Join(t.TempDir(), "stacklab.db"),
		SelfUpdateHelperPath: helperPath,
		SelfUpdateUseSudo:    true,
	}, appStore, jobService, nil, nil, logger)
	service.runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		switch name {
		case "dpkg-query":
			return []byte("ii\t2026.07.08\n"), nil
		case "env":
			return []byte("stacklab:\n  Installed: 2026.07.08\n  Candidate: 2026.07.09\n"), nil
		case "sudo":
			return []byte(`{"result":"ok"}`), nil
		default:
			t.Fatalf("unexpected command %s %#v", name, args)
			return nil, nil
		}
	}

	overview, err := service.Overview(context.Background())
	if err != nil {
		t.Fatalf("Overview() error = %v", err)
	}
	if !overview.Package.Supported || !overview.WriteCapability.Supported || !overview.Package.UpdateAvailable {
		t.Fatalf("unexpected overview before Apply: %#v", overview)
	}

	_, err = service.Apply(context.Background(), ApplyRequest{ExpectedCandidateVersion: "2026.07.09"}, "local")
	if !errors.Is(err, ErrInvalidState) {
		t.Fatalf("Apply() error = %v, want %v", err, ErrInvalidState)
	}
}

func TestApplyHoldsMutationDrainUntilSelfUpdateFinalizes(t *testing.T) {
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
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service := NewService(config.Config{
		DatabasePath:         filepath.Join(directory, "stacklab.db"),
		SelfUpdateHelperPath: helperPath,
		SelfUpdateUseSudo:    true,
	}, appStore, jobService, nil, nil, logger)
	service.runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		switch name {
		case "dpkg-query":
			return []byte("ii\t2026.07.08\n"), nil
		case "env":
			return []byte("stacklab:\n  Installed: 2026.07.08\n  Candidate: 2026.07.09\n"), nil
		case "sudo":
			return []byte(`{"result":"ok"}`), nil
		default:
			t.Fatalf("unexpected command %s %#v", name, args)
			return nil, nil
		}
	}

	response, err := service.Apply(context.Background(), ApplyRequest{ExpectedCandidateVersion: "2026.07.09"}, "local")
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if !response.Started || response.Job.ID == "" || response.Job.State != "running" {
		t.Fatalf("unexpected Apply() response: %#v", response)
	}
	_, err = jobService.Start(context.Background(), "demo", "up", "local")
	var conflict *jobs.ResourceConflictError
	if !errors.As(err, &conflict) || conflict.Reason != jobs.ConflictReasonDrainActive || conflict.ConflictingJobID != response.Job.ID {
		t.Fatalf("Start(stack during self-update) error = %#v, want active drain owned by %s", err, response.Job.ID)
	}

	if _, err := jobService.FinishFailed(context.Background(), response.Job, "test_finalized", "test finalized"); err != nil {
		t.Fatalf("FinishFailed(self-update) error = %v", err)
	}
	if _, err := jobService.Start(context.Background(), "demo", "up", "local"); err != nil {
		t.Fatalf("Start(stack after self-update finalization) error = %v", err)
	}
}

func hasArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func newTestService(t *testing.T, cfg config.Config) *Service {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "stacklab.db")
	appStore, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() {
		_ = appStore.Close()
	})

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewService(cfg, appStore, nil, nil, nil, logger)
}
