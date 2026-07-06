package hostinfo

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
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

func TestMetricsCollectorSamplesFilesystemsAndNetworkRates(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	procDir := filepath.Join(tempDir, "proc")
	if err := os.MkdirAll(filepath.Join(procDir, "self"), 0o755); err != nil {
		t.Fatalf("MkdirAll(proc/self) error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(procDir, "net"), 0o755); err != nil {
		t.Fatalf("MkdirAll(proc/net) error = %v", err)
	}

	mountPoint := filepath.Join(tempDir, "stacklab")
	rootDir := filepath.Join(mountPoint, "data")
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(root) error = %v", err)
	}

	writeMetricsProcFixture(t, procDir, "cpu  100 0 0 100 0 0 0 0 0 0\n", 1000, 2000, 1000, 2000)
	mountInfo := strings.Join([]string{
		"26 23 8:2 / " + mountPoint + " rw,relatime - ext4 /dev/nvme0n1p2 rw",
		"27 23 0:42 / /mnt/offline-nas rw,relatime - nfs4 nas:/share rw",
		"27 23 0:23 / /run rw,nosuid,nodev - tmpfs tmpfs rw",
	}, "\n")
	if err := os.WriteFile(filepath.Join(procDir, "self", "mountinfo"), []byte(mountInfo+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(mountinfo) error = %v", err)
	}

	now := time.Unix(1_712_598_000, 0).UTC()
	collector := newMetricsCollector(rootDir, procDir)
	collector.now = func() time.Time {
		return now
	}

	collector.sampleAndStore()
	now = now.Add(10 * time.Second)
	writeMetricsProcFixture(t, procDir, "cpu  150 0 0 150 0 0 0 0 0 0\n", 2500, 2600, 1200, 2100)
	collector.sampleAndStore()

	response := collector.Snapshot(MetricsQuery{})
	if response.Current == nil {
		t.Fatalf("response.Current is nil")
	}
	if len(response.History) != 2 {
		t.Fatalf("len(response.History) = %d, want 2", len(response.History))
	}
	if response.Current.CPU.UsagePercent != 50 {
		t.Fatalf("response.Current.CPU.UsagePercent = %v, want 50", response.Current.CPU.UsagePercent)
	}
	if response.Current.Memory.TotalBytes != 1024000*1024 || response.Current.Memory.UsagePercent != 50 {
		t.Fatalf("unexpected memory usage: %#v", response.Current.Memory)
	}
	if response.Current.Swap.TotalBytes != 2048000*1024 || response.Current.Swap.UsedBytes != 512000*1024 || response.Current.Swap.UsagePercent != 25 {
		t.Fatalf("unexpected swap usage: %#v", response.Current.Swap)
	}
	if len(response.Current.Filesystems) != 1 {
		t.Fatalf("len(filesystems) = %d, want 1: %#v", len(response.Current.Filesystems), response.Current.Filesystems)
	}
	if filesystem := response.Current.Filesystems[0]; filesystem.MountPoint != mountPoint || filesystem.Device != "/dev/nvme0n1p2" || filesystem.FSType != "ext4" || !filesystem.Primary {
		t.Fatalf("unexpected filesystem: %#v", filesystem)
	}
	if response.Current.DiskIO.TotalReadBytesPerSec != 10240 || response.Current.DiskIO.TotalWriteBytesPerSec != 5120 {
		t.Fatalf("unexpected disk IO totals: %#v", response.Current.DiskIO)
	}
	if len(response.Current.DiskIO.Devices) != 1 || response.Current.DiskIO.Devices[0].Name != "vda" {
		t.Fatalf("unexpected disk IO devices: %#v", response.Current.DiskIO.Devices)
	}
	if response.Current.Network.TotalRXBytesPerSec != 150 || response.Current.Network.TotalTXBytesPerSec != 60 {
		t.Fatalf("unexpected network totals: %#v", response.Current.Network)
	}
	if len(response.Current.Network.Interfaces) != 1 || response.Current.Network.Interfaces[0].Name != "eth0" {
		t.Fatalf("unexpected interfaces: %#v", response.Current.Network.Interfaces)
	}
}

func TestMetricsCollectorPrunesHistoryByTime(t *testing.T) {
	t.Parallel()

	collector := newMetricsCollector(t.TempDir(), t.TempDir())
	collector.historyWindow = time.Minute
	collector.maxSamples = 100

	now := time.Unix(1_712_598_000, 0).UTC()
	collector.samples = []HostMetricSample{
		{SampledAt: now.Add(-2 * time.Minute)},
		{SampledAt: now.Add(-30 * time.Second)},
	}
	collector.now = func() time.Time {
		return now
	}

	collector.sampleAndStore()

	response := collector.Snapshot(MetricsQuery{})
	if len(response.History) != 2 {
		t.Fatalf("len(response.History) = %d, want 2: %#v", len(response.History), response.History)
	}
	if response.History[0].SampledAt.Before(now.Add(-time.Minute)) {
		t.Fatalf("old sample was not pruned: %#v", response.History)
	}
}

func TestMetricsCollectorSnapshotFiltersHistorySince(t *testing.T) {
	t.Parallel()

	collector := newMetricsCollector(t.TempDir(), t.TempDir())
	base := time.Unix(1_712_598_000, 0).UTC()
	collector.samples = []HostMetricSample{
		{SampledAt: base.Add(-2 * time.Second)},
		{SampledAt: base.Add(-time.Second)},
		{SampledAt: base},
	}
	collector.now = func() time.Time {
		return base
	}
	since := base.Add(-time.Second)

	response := collector.Snapshot(MetricsQuery{Since: &since})
	if response.Current == nil || !response.Current.SampledAt.Equal(base) {
		t.Fatalf("unexpected current sample: %#v", response.Current)
	}
	if len(response.History) != 1 || !response.History[0].SampledAt.Equal(base) {
		t.Fatalf("unexpected filtered history: %#v", response.History)
	}
}

func TestMetricsCollectorSwitchesToActiveIntervalAfterSnapshot(t *testing.T) {
	t.Parallel()

	collector := newMetricsCollector(t.TempDir(), t.TempDir())
	now := time.Unix(1_712_598_000, 0).UTC()
	collector.now = func() time.Time {
		return now
	}

	if got := collector.currentInterval(); got != metricsBackgroundSampleInterval {
		t.Fatalf("currentInterval() = %v, want %v", got, metricsBackgroundSampleInterval)
	}

	response := collector.Snapshot(MetricsQuery{})
	if response.SampleIntervalSeconds != int(metricsActiveSampleInterval.Seconds()) {
		t.Fatalf("response.SampleIntervalSeconds = %d, want %d", response.SampleIntervalSeconds, int(metricsActiveSampleInterval.Seconds()))
	}
	if got := collector.currentInterval(); got != metricsActiveSampleInterval {
		t.Fatalf("currentInterval(active) = %v, want %v", got, metricsActiveSampleInterval)
	}

	now = now.Add(metricsActiveTTL + time.Second)
	if got := collector.currentInterval(); got != metricsBackgroundSampleInterval {
		t.Fatalf("currentInterval(expired) = %v, want %v", got, metricsBackgroundSampleInterval)
	}
}

func writeMetricsProcFixture(t *testing.T, procDir, statLine string, eth0RXBytes, eth0TXBytes, diskReadSectors, diskWriteSectors uint64) {
	t.Helper()

	files := map[string]string{
		"loadavg": "0.31 0.22 0.18 1/100 123\n",
		"meminfo": "MemTotal:        1024000 kB\nMemAvailable:     512000 kB\nSwapTotal:       2048000 kB\nSwapFree:        1536000 kB\n",
		"stat":    statLine,
		"diskstats": strings.Join([]string{
			"   7       0 loop0 1 0 100 0 1 0 100 0 0 0 0 0 0 0 0",
			" 252       0 vda 10 0 " + strconv.FormatUint(diskReadSectors, 10) + " 0 20 0 " + strconv.FormatUint(diskWriteSectors, 10) + " 0 0 0 0 0 0 0 0",
			" 252       1 vda1 99 0 999999 0 99 0 999999 0 0 0 0 0 0 0 0",
		}, "\n") + "\n",
		filepath.Join("net", "dev"): strings.Join([]string{
			"Inter-|   Receive                                                |  Transmit",
			" face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed",
			"  lo: 1 0 0 0 0 0 0 0 1 0 0 0 0 0 0 0",
			"eth0: " + strconv.FormatUint(eth0RXBytes, 10) + " 0 0 0 0 0 0 0 " + strconv.FormatUint(eth0TXBytes, 10) + " 0 0 0 0 0 0 0",
			"br-123456: 999999 0 0 0 0 0 0 0 999999 0 0 0 0 0 0 0",
			"vethabc: 999999 0 0 0 0 0 0 0 999999 0 0 0 0 0 0 0",
		}, "\n") + "\n",
	}

	for name, content := range files {
		if err := os.WriteFile(filepath.Join(procDir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", name, err)
		}
	}
}
