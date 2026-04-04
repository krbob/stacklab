package hostinfo

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"stacklab/internal/config"
)

func TestParseJournalEntries(t *testing.T) {
	t.Parallel()

	output := strings.Join([]string{
		`{"__REALTIME_TIMESTAMP":"1712336000000000","PRIORITY":"6","MESSAGE":"Started","__CURSOR":"s=cursor-1"}`,
		`{"__REALTIME_TIMESTAMP":"1712336010000000","PRIORITY":"3","MESSAGE":"Failed to bind","__CURSOR":"s=cursor-2"}`,
	}, "\n")

	entries, err := parseJournalEntries([]byte(output))
	if err != nil {
		t.Fatalf("parseJournalEntries() error = %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	if entries[0].Level != "info" || entries[0].Message != "Started" || entries[0].Cursor != "s=cursor-1" {
		t.Fatalf("unexpected first entry: %#v", entries[0])
	}
	if entries[1].Level != "error" || entries[1].Cursor != "s=cursor-2" {
		t.Fatalf("unexpected second entry: %#v", entries[1])
	}
}

func TestFilterLogEntries(t *testing.T) {
	t.Parallel()

	entries := []StacklabLogEntry{
		{Level: "info", Message: "Started HTTP server"},
		{Level: "error", Message: "Failed to bind port"},
		{Level: "warn", Message: "Slow response"},
	}

	filtered := filterLogEntries(entries, "error", "")
	if len(filtered) != 1 || filtered[0].Message != "Failed to bind port" {
		t.Fatalf("unexpected level filter result: %#v", filtered)
	}

	filtered = filterLogEntries(entries, "", "server")
	if len(filtered) != 1 || filtered[0].Message != "Started HTTP server" {
		t.Fatalf("unexpected search filter result: %#v", filtered)
	}
}

func TestStacklabLogsUsesRunnerAndFilters(t *testing.T) {
	t.Parallel()

	service := NewService(config.Config{RootDir: t.TempDir(), SystemdUnitName: "stacklab"}, time.Unix(1_712_598_000, 0).UTC())
	service.runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name != "journalctl" {
			t.Fatalf("unexpected command %q", name)
		}
		return []byte(strings.Join([]string{
			`{"__REALTIME_TIMESTAMP":"1712336000000000","PRIORITY":"6","MESSAGE":"Started","__CURSOR":"s=cursor-1"}`,
			`{"__REALTIME_TIMESTAMP":"1712336010000000","PRIORITY":"3","MESSAGE":"Failed to bind","__CURSOR":"s=cursor-2"}`,
		}, "\n")), nil
	}

	response, err := service.StacklabLogs(context.Background(), LogsQuery{
		Limit:  200,
		Level:  "error",
		Search: "bind",
	})
	if err != nil {
		t.Fatalf("StacklabLogs() error = %v", err)
	}
	if len(response.Items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(response.Items))
	}
	if response.Items[0].Cursor != "s=cursor-2" || response.NextCursor != "s=cursor-2" {
		t.Fatalf("unexpected response: %#v", response)
	}
}

func TestOverviewReadsProcAndDiskData(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	procDir := filepath.Join(tempDir, "proc")
	if err := os.MkdirAll(procDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(proc) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(procDir, "uptime"), []byte("12345.67 54321.00\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(uptime) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(procDir, "loadavg"), []byte("0.31 0.22 0.18 1/100 123\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(loadavg) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(procDir, "meminfo"), []byte("MemTotal:        1024000 kB\nMemAvailable:     512000 kB\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(meminfo) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(procDir, "stat"), []byte("cpu  100 20 30 400 50 0 0 0 0 0\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(stat) error = %v", err)
	}

	osReleasePath := filepath.Join(tempDir, "os-release")
	if err := os.WriteFile(osReleasePath, []byte("PRETTY_NAME=\"Test Linux\"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(os-release) error = %v", err)
	}

	rootDir := filepath.Join(tempDir, "root")
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(root) error = %v", err)
	}

	service := NewService(config.Config{RootDir: rootDir, SystemdUnitName: "stacklab"}, time.Unix(1_712_598_000, 0).UTC())
	service.procRoot = procDir
	service.osReleasePath = osReleasePath

	response, err := service.Overview(context.Background())
	if err != nil {
		t.Fatalf("Overview() error = %v", err)
	}
	if response.Host.OSName != "Test Linux" {
		t.Fatalf("response.Host.OSName = %q, want %q", response.Host.OSName, "Test Linux")
	}
	if response.Host.UptimeSeconds != 12345 {
		t.Fatalf("response.Host.UptimeSeconds = %d, want %d", response.Host.UptimeSeconds, 12345)
	}
	if len(response.Resources.CPU.LoadAverage) != 3 || response.Resources.CPU.LoadAverage[0] != 0.3 {
		t.Fatalf("unexpected CPU load average: %#v", response.Resources.CPU.LoadAverage)
	}
	if response.Resources.Memory.TotalBytes == 0 || response.Resources.Memory.AvailableBytes == 0 {
		t.Fatalf("unexpected memory usage: %#v", response.Resources.Memory)
	}
	if response.Resources.Disk.Path != rootDir {
		t.Fatalf("response.Resources.Disk.Path = %q, want %q", response.Resources.Disk.Path, rootDir)
	}
}
