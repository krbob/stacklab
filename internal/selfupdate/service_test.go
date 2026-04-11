package selfupdate

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"stacklab/internal/config"
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
