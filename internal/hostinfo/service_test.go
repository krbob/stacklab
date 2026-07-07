package hostinfo

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
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

	filtered := filterLogEntries(entries, "error", "", false)
	if len(filtered) != 1 || filtered[0].Message != "Failed to bind port" {
		t.Fatalf("unexpected level filter result: %#v", filtered)
	}

	filtered = filterLogEntries(entries, "", "server", false)
	if len(filtered) != 1 || filtered[0].Message != "Started HTTP server" {
		t.Fatalf("unexpected search filter result: %#v", filtered)
	}
}

func TestFilterLogEntriesHidesHTTPAccessLogsByDefault(t *testing.T) {
	t.Parallel()

	entries := []StacklabLogEntry{
		{Level: "info", Message: `time=2026-07-07T10:42:53+02:00 level=INFO msg="http request" method=GET path=/api/host/metrics status=200 duration=2ms`},
		{Level: "info", Message: `{"time":"2026-07-07T10:42:53+02:00","level":"INFO","msg":"http request","method":"GET"}`},
		{Level: "info", Message: "Started HTTP server"},
		{Level: "warn", Message: `time=2026-07-07T10:42:53+02:00 level=WARN msg="http request" method=GET path=/api/failing status=500 duration=2ms`},
	}

	filtered := filterLogEntries(entries, "", "", false)
	if len(filtered) != 2 {
		t.Fatalf("len(filtered) = %d, want 2: %#v", len(filtered), filtered)
	}
	if filtered[0].Message != "Started HTTP server" || filtered[1].Level != "warn" {
		t.Fatalf("unexpected filtered entries: %#v", filtered)
	}

	filtered = filterLogEntries(entries, "", "", true)
	if len(filtered) != len(entries) {
		t.Fatalf("len(filtered with HTTP access) = %d, want %d", len(filtered), len(entries))
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

func TestStacklabLogsIncludesHTTPAccessLogsWhenRequested(t *testing.T) {
	t.Parallel()

	service := NewService(config.Config{RootDir: t.TempDir(), SystemdUnitName: "stacklab"}, time.Unix(1_712_598_000, 0).UTC())
	service.runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte(strings.Join([]string{
			`{"__REALTIME_TIMESTAMP":"1712336000000000","PRIORITY":"6","MESSAGE":"time=2026-07-07T10:42:53+02:00 level=INFO msg=\"http request\" method=GET path=/api/host/metrics status=200 duration=2ms","__CURSOR":"s=cursor-1"}`,
			`{"__REALTIME_TIMESTAMP":"1712336010000000","PRIORITY":"6","MESSAGE":"startup complete","__CURSOR":"s=cursor-2"}`,
		}, "\n")), nil
	}

	response, err := service.StacklabLogs(context.Background(), LogsQuery{Limit: 200})
	if err != nil {
		t.Fatalf("StacklabLogs() error = %v", err)
	}
	if len(response.Items) != 1 || response.Items[0].Cursor != "s=cursor-2" {
		t.Fatalf("unexpected default response: %#v", response)
	}

	response, err = service.StacklabLogs(context.Background(), LogsQuery{Limit: 200, IncludeHTTPAccess: true})
	if err != nil {
		t.Fatalf("StacklabLogs(include HTTP) error = %v", err)
	}
	if len(response.Items) != 2 || response.Items[0].Cursor != "s=cursor-1" {
		t.Fatalf("unexpected include HTTP response: %#v", response)
	}
}

func TestStacklabLogsScansBeyondDisplayLimitBeforeFiltering(t *testing.T) {
	t.Parallel()

	service := NewService(config.Config{RootDir: t.TempDir(), SystemdUnitName: "stacklab"}, time.Unix(1_712_598_000, 0).UTC())
	service.runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name != "journalctl" {
			t.Fatalf("unexpected command %q", name)
		}
		if !hasJournalLimit(args, "20") {
			t.Fatalf("journalctl args = %#v, want raw -n 20 for filtered limit 2", args)
		}
		return []byte(strings.Join([]string{
			`{"__REALTIME_TIMESTAMP":"1712336000000000","PRIORITY":"6","MESSAGE":"startup complete","__CURSOR":"s=cursor-1"}`,
			`{"__REALTIME_TIMESTAMP":"1712336010000000","PRIORITY":"6","MESSAGE":"permission probe complete","__CURSOR":"s=cursor-2"}`,
			`{"__REALTIME_TIMESTAMP":"1712336020000000","PRIORITY":"6","MESSAGE":"workspace repair complete","__CURSOR":"s=cursor-3"}`,
			`{"__REALTIME_TIMESTAMP":"1712336030000000","PRIORITY":"6","MESSAGE":"time=2026-07-07T10:42:53+02:00 level=INFO msg=\"http request\" method=GET path=/api/host/metrics status=200 duration=2ms","__CURSOR":"s=cursor-4"}`,
			`{"__REALTIME_TIMESTAMP":"1712336040000000","PRIORITY":"6","MESSAGE":"time=2026-07-07T10:42:54+02:00 level=INFO msg=\"http request\" method=GET path=/api/host/metrics status=200 duration=2ms","__CURSOR":"s=cursor-5"}`,
		}, "\n")), nil
	}

	response, err := service.StacklabLogs(context.Background(), LogsQuery{Limit: 2})
	if err != nil {
		t.Fatalf("StacklabLogs() error = %v", err)
	}
	if len(response.Items) != 2 {
		t.Fatalf("len(items) = %d, want 2: %#v", len(response.Items), response.Items)
	}
	if response.Items[0].Cursor != "s=cursor-2" || response.Items[1].Cursor != "s=cursor-3" {
		t.Fatalf("unexpected capped filtered items: %#v", response.Items)
	}
	if response.NextCursor != "s=cursor-3" {
		t.Fatalf("NextCursor = %q, want s=cursor-3", response.NextCursor)
	}
}

func TestStacklabLogsAdvancesCursorWhenOnlyFilteredEntriesAreReturned(t *testing.T) {
	t.Parallel()

	service := NewService(config.Config{RootDir: t.TempDir(), SystemdUnitName: "stacklab"}, time.Unix(1_712_598_000, 0).UTC())
	service.runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte(strings.Join([]string{
			`{"__REALTIME_TIMESTAMP":"1712336030000000","PRIORITY":"6","MESSAGE":"time=2026-07-07T10:42:53+02:00 level=INFO msg=\"http request\" method=GET path=/api/host/metrics status=200 duration=2ms","__CURSOR":"s=cursor-1"}`,
			`{"__REALTIME_TIMESTAMP":"1712336040000000","PRIORITY":"6","MESSAGE":"time=2026-07-07T10:42:54+02:00 level=INFO msg=\"http request\" method=GET path=/api/host/metrics status=200 duration=2ms","__CURSOR":"s=cursor-2"}`,
		}, "\n")), nil
	}

	response, err := service.StacklabLogs(context.Background(), LogsQuery{Limit: 2})
	if err != nil {
		t.Fatalf("StacklabLogs() error = %v", err)
	}
	if len(response.Items) != 0 {
		t.Fatalf("len(items) = %d, want 0: %#v", len(response.Items), response.Items)
	}
	if response.NextCursor != "s=cursor-2" {
		t.Fatalf("NextCursor = %q, want last raw cursor", response.NextCursor)
	}
}

func hasJournalLimit(args []string, value string) bool {
	for i, arg := range args {
		if arg == "-n" && i+1 < len(args) && args[i+1] == value {
			return true
		}
	}
	return false
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
	sysDir := filepath.Join(tempDir, "sys")
	hwmonDir := filepath.Join(sysDir, "class", "hwmon", "hwmon0")
	if err := os.MkdirAll(hwmonDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(hwmon) error = %v", err)
	}
	temperatureFiles := map[string]string{
		"name":        "coretemp\n",
		"temp1_label": "Package id 0\n",
		"temp1_input": "42000\n",
		"temp2_label": "Core 0\n",
		"temp2_input": "41000\n",
	}
	for name, content := range temperatureFiles {
		if err := os.WriteFile(filepath.Join(hwmonDir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", name, err)
		}
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
	collector.sysRoot = sysDir
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
	if response.Current.Processes == nil {
		t.Fatal("response.Current.Processes is nil")
	}
	if response.History[0].Processes != nil {
		t.Fatalf("history process payload should be omitted: %#v", response.History[0].Processes)
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
	if response.Current.Temperatures.CPUCelsius == nil || *response.Current.Temperatures.CPUCelsius != 42 {
		t.Fatalf("unexpected CPU temperature: %#v", response.Current.Temperatures)
	}
	if len(response.Current.Temperatures.Sensors) != 2 || response.Current.Temperatures.Sensors[0].Name != "coretemp" {
		t.Fatalf("unexpected temperature sensors: %#v", response.Current.Temperatures.Sensors)
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

func TestParseProcessStat(t *testing.T) {
	t.Parallel()

	sample, ok := parseProcessStat(123, processStatLine(123, "worker name", "R", 10, 7, 3), 4096)
	if !ok {
		t.Fatal("parseProcessStat() returned false")
	}
	if sample.pid != 123 || sample.command != "worker name" || sample.state != "R" {
		t.Fatalf("unexpected process identity: %#v", sample)
	}
	if sample.ticks != 17 {
		t.Fatalf("sample.ticks = %d, want 17", sample.ticks)
	}
	if sample.rssBytes != 12_288 {
		t.Fatalf("sample.rssBytes = %d, want 12288", sample.rssBytes)
	}
}

func TestMetricsCollectorReadsTopProcesses(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	procDir := filepath.Join(tempDir, "proc")
	if err := os.MkdirAll(procDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(proc) error = %v", err)
	}

	memTotal := uint64(os.Getpagesize() * 4000)
	collector := newMetricsCollector(t.TempDir(), procDir)

	if err := os.WriteFile(filepath.Join(procDir, "stat"), []byte("cpu  100 0 0 100 0 0 0 0 0 0\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(stat) error = %v", err)
	}
	writeProcessFixture(t, procDir, 101, "stacklab", "S", 10, 5, 1000)
	writeProcessFixture(t, procDir, 202, "busy worker", "R", 20, 10, 2000)

	first := collector.readProcessUsage(map[string]uint64{"MemTotal": memTotal})
	if first.Total != 2 {
		t.Fatalf("first.Total = %d, want 2", first.Total)
	}
	if len(first.Items) != 2 {
		t.Fatalf("len(first.Items) = %d, want 2: %#v", len(first.Items), first.Items)
	}
	if first.Items[0].CPUPercent != 0 {
		t.Fatalf("first CPUPercent = %v, want 0", first.Items[0].CPUPercent)
	}

	if err := os.WriteFile(filepath.Join(procDir, "stat"), []byte("cpu  200 0 0 200 0 0 0 0 0 0\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(stat) error = %v", err)
	}
	writeProcessFixture(t, procDir, 101, "stacklab", "S", 20, 15, 1000)
	writeProcessFixture(t, procDir, 202, "busy worker", "R", 80, 60, 2000)

	second := collector.readProcessUsage(map[string]uint64{"MemTotal": memTotal})
	if len(second.Items) != 2 {
		t.Fatalf("len(second.Items) = %d, want 2: %#v", len(second.Items), second.Items)
	}
	top := second.Items[0]
	if top.PID != 202 || top.Command != "busy worker" || top.State != "R" {
		t.Fatalf("unexpected top process: %#v", top)
	}
	wantCPU := roundFloat((float64(110) / float64(200)) * float64(runtime.NumCPU()) * 100)
	if top.CPUPercent != wantCPU {
		t.Fatalf("top.CPUPercent = %v, want %v", top.CPUPercent, wantCPU)
	}
	if top.MemoryBytes != uint64(os.Getpagesize()*2000) || top.MemoryPercent != 50 {
		t.Fatalf("unexpected top memory: %#v", top)
	}
	if top.User == "" {
		t.Fatal("top.User is empty")
	}
}

func TestMetricsCollectorReadsProcessDisplayCommandFromCmdline(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	procDir := filepath.Join(tempDir, "proc")
	if err := os.MkdirAll(procDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(proc) error = %v", err)
	}

	collector := newMetricsCollector(t.TempDir(), procDir)
	memTotal := uint64(os.Getpagesize() * 4000)
	if err := os.WriteFile(filepath.Join(procDir, "stat"), []byte("cpu  100 0 0 100 0 0 0 0 0 0\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(stat) error = %v", err)
	}
	writeProcessFixture(t, procDir, 303, "java", "S", 10, 5, 1000)
	writeProcessCmdlineFixture(t, procDir, 303, []string{
		"/usr/bin/java",
		"-Xmx2G",
		"-Ddb.password=swordfish",
		"-jar",
		"/srv/minecraft/server.jar",
		"nogui",
	})

	usage := collector.readProcessUsage(map[string]uint64{"MemTotal": memTotal})
	if len(usage.Items) != 1 {
		t.Fatalf("len(usage.Items) = %d, want 1: %#v", len(usage.Items), usage.Items)
	}
	process := usage.Items[0]
	if process.Command != "java" {
		t.Fatalf("process.Command = %q, want java", process.Command)
	}
	if process.DisplayCommand != "java -jar server.jar nogui" {
		t.Fatalf("process.DisplayCommand = %q, want compact java jar label", process.DisplayCommand)
	}
	if strings.Contains(process.DisplayCommand, "swordfish") {
		t.Fatalf("process.DisplayCommand leaked secret: %q", process.DisplayCommand)
	}
}

func TestRedactProcessArgs(t *testing.T) {
	t.Parallel()

	got := strings.Join(redactProcessArgs([]string{"worker", "--token", "abc123", "--password=secret", "plain"}), " ")
	want := "worker --token [redacted] --password=[redacted] plain"
	if got != want {
		t.Fatalf("redactProcessArgs() = %q, want %q", got, want)
	}
}

func TestMetricsCollectorThrottlesProcessSampling(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	procDir := filepath.Join(tempDir, "proc")
	if err := os.MkdirAll(procDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(proc) error = %v", err)
	}

	collector := newMetricsCollector(t.TempDir(), procDir)
	collector.processSampleInterval = 5 * time.Second
	memInfo := map[string]uint64{"MemTotal": uint64(os.Getpagesize() * 4000)}
	base := time.Unix(1_712_598_000, 0).UTC()

	if err := os.WriteFile(filepath.Join(procDir, "stat"), []byte("cpu  100 0 0 100 0 0 0 0 0 0\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(stat) error = %v", err)
	}
	writeProcessFixture(t, procDir, 101, "stacklab", "S", 10, 5, 1000)

	first := collector.readProcessUsageForSample(base, memInfo)
	if first.Total != 1 {
		t.Fatalf("first.Total = %d, want 1", first.Total)
	}

	writeProcessFixture(t, procDir, 202, "new worker", "R", 20, 10, 2000)
	cached := collector.readProcessUsageForSample(base.Add(time.Second), memInfo)
	if cached.Total != 1 {
		t.Fatalf("cached.Total = %d, want 1 while process cache is fresh", cached.Total)
	}

	refreshed := collector.readProcessUsageForSample(base.Add(6*time.Second), memInfo)
	if refreshed.Total != 2 {
		t.Fatalf("refreshed.Total = %d, want 2 after process cache expires", refreshed.Total)
	}
}

func TestMetricsCollectorDedupesSystemdBindMountFilesystems(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	procDir := filepath.Join(tempDir, "proc")
	if err := os.MkdirAll(filepath.Join(procDir, "self"), 0o755); err != nil {
		t.Fatalf("MkdirAll(proc/self) error = %v", err)
	}

	hostRoot := filepath.Join(tempDir, "host")
	stacklabRoot := filepath.Join(hostRoot, "srv", "stacklab")
	etcDir := filepath.Join(tempDir, "namespace", "etc")
	usrDir := filepath.Join(tempDir, "namespace", "usr")
	efiDir := filepath.Join(tempDir, "efi")
	storageDir := filepath.Join(tempDir, "storage")
	for _, dir := range []string{hostRoot, stacklabRoot, etcDir, usrDir, efiDir, storageDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) error = %v", dir, err)
		}
	}

	mountInfo := strings.Join([]string{
		"26 23 8:2 / " + hostRoot + " rw,relatime - ext4 /dev/nvme0n1p2 rw",
		"27 23 8:2 /srv/stacklab " + stacklabRoot + " rw,relatime - ext4 /dev/nvme0n1p2 rw",
		"28 23 8:2 /etc " + etcDir + " rw,relatime - ext4 /dev/nvme0n1p2 rw",
		"29 23 8:2 /usr " + usrDir + " rw,relatime - ext4 /dev/nvme0n1p2 rw",
		"30 23 259:1 / " + efiDir + " rw,relatime - vfat /dev/nvme0n1p1 rw",
		"31 23 259:4 / " + storageDir + " rw,relatime - btrfs /dev/nvme0n1p4 rw",
	}, "\n")
	if err := os.WriteFile(filepath.Join(procDir, "self", "mountinfo"), []byte(mountInfo+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(mountinfo) error = %v", err)
	}

	collector := newMetricsCollector(stacklabRoot, procDir)
	filesystems := collector.readFilesystems()
	if len(filesystems) != 3 {
		t.Fatalf("len(filesystems) = %d, want 3: %#v", len(filesystems), filesystems)
	}
	if filesystems[0].MountPoint != hostRoot || filesystems[0].Device != "/dev/nvme0n1p2" || !filesystems[0].Primary {
		t.Fatalf("unexpected primary filesystem: %#v", filesystems[0])
	}
	if filesystems[1].MountPoint != efiDir || filesystems[1].Device != "/dev/nvme0n1p1" || filesystems[1].Primary {
		t.Fatalf("unexpected EFI filesystem: %#v", filesystems[1])
	}
	if filesystems[2].MountPoint != storageDir || filesystems[2].Device != "/dev/nvme0n1p4" || filesystems[2].Primary {
		t.Fatalf("unexpected storage filesystem: %#v", filesystems[2])
	}
}

func TestMetricsCollectorReadsThermalZoneTemperatureSensors(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	thermalZoneDir := filepath.Join(tempDir, "sys", "class", "thermal", "thermal_zone0")
	if err := os.MkdirAll(thermalZoneDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(thermal_zone0) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(thermalZoneDir, "type"), []byte("cpu-thermal\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(type) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(thermalZoneDir, "temp"), []byte("56500\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(temp) error = %v", err)
	}

	collector := newMetricsCollector(t.TempDir(), t.TempDir())
	collector.sysRoot = filepath.Join(tempDir, "sys")

	usage := collector.readTemperatureUsage()
	if usage.CPUCelsius == nil || *usage.CPUCelsius != 56.5 {
		t.Fatalf("unexpected CPU temperature: %#v", usage)
	}
	if len(usage.Sensors) != 1 || usage.Sensors[0].Name != "cpu-thermal" || usage.Sensors[0].Label != "thermal_zone0" {
		t.Fatalf("unexpected sensors: %#v", usage.Sensors)
	}
}

func TestMetricsCollectorSelectsStableCPUTemperatureSensor(t *testing.T) {
	t.Parallel()

	usage := TemperatureUsage{
		Sensors: []TemperatureSensor{
			{Name: "coretemp", Label: "Core 3", TemperatureCelsius: 63},
			{Name: "x86_pkg_temp", Label: "thermal_zone2", TemperatureCelsius: 62},
			{Name: "coretemp", Label: "Package id 0", TemperatureCelsius: 58},
		},
	}
	cpuSensor := selectCPUTemperatureSensor(usage.Sensors)
	if cpuSensor == nil {
		t.Fatal("selectCPUTemperatureSensor() returned nil")
	}
	if cpuSensor.Name != "coretemp" || cpuSensor.Label != "Package id 0" || cpuSensor.TemperatureCelsius != 58 {
		t.Fatalf("unexpected CPU sensor: %#v", cpuSensor)
	}
}

func TestMetricsCollectorDoesNotSelectNonCPUSensorAsCPUTemperature(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	hwmonDir := filepath.Join(tempDir, "sys", "class", "hwmon", "hwmon0")
	if err := os.MkdirAll(hwmonDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(hwmon0) error = %v", err)
	}
	files := map[string]string{
		"name":        "nvme\n",
		"temp1_label": "Composite\n",
		"temp1_input": "48000\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(hwmonDir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", name, err)
		}
	}

	collector := newMetricsCollector(t.TempDir(), t.TempDir())
	collector.sysRoot = filepath.Join(tempDir, "sys")

	usage := collector.readTemperatureUsage()
	if usage.CPUCelsius != nil {
		t.Fatalf("CPUCelsius = %v, want nil for non-CPU sensor", *usage.CPUCelsius)
	}
	if len(usage.Sensors) != 1 || usage.Sensors[0].Name != "nvme" {
		t.Fatalf("unexpected sensors: %#v", usage.Sensors)
	}
}

func TestNormalizePublicIP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
		ok   bool
	}{
		{name: "ipv4", raw: "8.8.8.8\n", want: "8.8.8.8", ok: true},
		{name: "ipv6", raw: "2001:4860:4860::8888\n", want: "2001:4860:4860::8888", ok: true},
		{name: "private", raw: "192.168.1.10", ok: false},
		{name: "loopback", raw: "127.0.0.1", ok: false},
		{name: "invalid", raw: "not-an-ip", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := normalizePublicIP(tt.raw)
			if ok != tt.ok {
				t.Fatalf("normalizePublicIP(%q) ok = %v, want %v", tt.raw, ok, tt.ok)
			}
			if got != tt.want {
				t.Fatalf("normalizePublicIP(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestMetricsCollectorRefreshesPublicIPAsynchronously(t *testing.T) {
	collector := newMetricsCollector(t.TempDir(), t.TempDir())
	collector.publicIPLookupEnabled = func() bool { return true }
	now := time.Unix(1_712_598_000, 0).UTC()
	collector.now = func() time.Time {
		return now
	}
	called := make(chan struct{})
	collector.publicIPResolver = func(ctx context.Context) (string, error) {
		close(called)
		return "8.8.8.8", nil
	}

	if got := collector.cachedPublicIP(now, true); got != "" {
		t.Fatalf("initial cachedPublicIP() = %q, want empty while refresh is in flight", got)
	}

	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for public IP resolver")
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if got := collector.cachedPublicIP(now.Add(time.Second), true); got == "8.8.8.8" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("cachedPublicIP() did not return refreshed IP")
}

func TestMetricsCollectorDoesNotRefreshPublicIPWhenDisabled(t *testing.T) {
	t.Parallel()

	collector := newMetricsCollector(t.TempDir(), t.TempDir())
	called := false
	collector.publicIPResolver = func(ctx context.Context) (string, error) {
		called = true
		return "8.8.8.8", nil
	}

	if got := collector.cachedPublicIP(time.Now().UTC(), true); got != "" {
		t.Fatalf("cachedPublicIP(disabled) = %q, want empty", got)
	}
	if called {
		t.Fatal("public IP resolver was called while lookup was disabled")
	}
}

func TestHostSettingsPersistAndDrivePublicIPLookup(t *testing.T) {
	t.Parallel()

	settingStore := &fakeHostSettingsStore{values: map[string]string{}}
	service := NewServiceWithStore(config.Config{
		RootDir:                   t.TempDir(),
		SystemdUnitName:           "stacklab",
		HostPublicIPLookupEnabled: true,
	}, time.Unix(1_712_598_000, 0).UTC(), settingStore)

	initial, err := service.GetSettings(context.Background())
	if err != nil {
		t.Fatalf("GetSettings() error = %v", err)
	}
	if !initial.PublicIPLookupEnabled || !service.publicIPLookupEnabled() {
		t.Fatalf("initial public IP lookup should inherit env default: %#v", initial)
	}

	updated, err := service.UpdateSettings(context.Background(), UpdateSettingsRequest{PublicIPLookupEnabled: false})
	if err != nil {
		t.Fatalf("UpdateSettings() error = %v", err)
	}
	if updated.PublicIPLookupEnabled || service.publicIPLookupEnabled() {
		t.Fatalf("public IP lookup should be disabled after persisted update: %#v", updated)
	}
	if settingStore.values[settingsKey] == "" {
		t.Fatal("host settings were not persisted")
	}
}

func TestMetricsCollectorDoesNotRefreshPublicIPWhenInactive(t *testing.T) {
	t.Parallel()

	collector := newMetricsCollector(t.TempDir(), t.TempDir())
	collector.publicIPLookupEnabled = func() bool { return true }
	called := false
	collector.publicIPResolver = func(ctx context.Context) (string, error) {
		called = true
		return "8.8.8.8", nil
	}

	if got := collector.cachedPublicIP(time.Now().UTC(), false); got != "" {
		t.Fatalf("cachedPublicIP(inactive) = %q, want empty", got)
	}
	if called {
		t.Fatal("public IP resolver was called while refresh was disabled")
	}
}

func TestMetricsCollectorPublicIPRefreshRecoversFromPanic(t *testing.T) {
	t.Parallel()

	collector := newMetricsCollector(t.TempDir(), t.TempDir())
	collector.publicIPLookupEnabled = func() bool { return true }
	collector.publicIPRefreshInFlight = true
	collector.publicIPResolver = func(ctx context.Context) (string, error) {
		panic("resolver panic")
	}

	collector.refreshPublicIP()

	if collector.publicIPRefreshInFlight {
		t.Fatal("publicIPRefreshInFlight = true after panic")
	}
	if collector.publicIPCheckedAt.IsZero() {
		t.Fatal("publicIPCheckedAt was not updated after panic")
	}
	if collector.publicIP != "" {
		t.Fatalf("publicIP = %q, want empty after panic", collector.publicIP)
	}
}

func TestParseTemperatureCelsiusUsesSysfsMillidegrees(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want float64
		ok   bool
	}{
		{name: "standard", raw: "42000", want: 42, ok: true},
		{name: "sub degree", raw: "900", want: 0.9, ok: true},
		{name: "zero sentinel", raw: "0", ok: false},
		{name: "negative sentinel", raw: "-5000", ok: false},
		{name: "implausible high", raw: "151000", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := parseTemperatureCelsius(tt.raw)
			if ok != tt.ok {
				t.Fatalf("parseTemperatureCelsius(%q) ok = %v, want %v", tt.raw, ok, tt.ok)
			}
			if got != tt.want {
				t.Fatalf("parseTemperatureCelsius(%q) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestShouldSkipBlockDeviceFiltersPartitions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want bool
	}{
		{name: "vda", want: false},
		{name: "vda1", want: true},
		{name: "vda15", want: true},
		{name: "sda10", want: true},
		{name: "xvda14", want: true},
		{name: "nvme0n1", want: false},
		{name: "nvme0n1p15", want: true},
		{name: "mmcblk0", want: false},
		{name: "mmcblk0p2", want: true},
		{name: "md0", want: false},
		{name: "dm-0", want: false},
		{name: "dm-0p1", want: true},
		{name: "loop0", want: true},
		{name: "zram0", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := shouldSkipBlockDevice(tt.name); got != tt.want {
				t.Fatalf("shouldSkipBlockDevice(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestMetricsCollectorPrunesHistoryByTime(t *testing.T) {
	t.Parallel()

	collector := newMetricsCollector(t.TempDir(), t.TempDir())
	collector.sysRoot = t.TempDir()
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
	collector.sysRoot = t.TempDir()
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
	collector.sysRoot = t.TempDir()
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
			" 252      15 vda15 99 0 999999 0 99 0 999999 0 0 0 0 0 0 0 0",
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

func writeProcessFixture(t *testing.T, procDir string, pid int, command, state string, utime, stime uint64, rssPages int64) {
	t.Helper()

	processDir := filepath.Join(procDir, strconv.Itoa(pid))
	if err := os.MkdirAll(processDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(process %d) error = %v", pid, err)
	}
	if err := os.WriteFile(filepath.Join(processDir, "stat"), []byte(processStatLine(pid, command, state, utime, stime, rssPages)), 0o644); err != nil {
		t.Fatalf("WriteFile(process stat %d) error = %v", pid, err)
	}
	if err := os.WriteFile(filepath.Join(processDir, "comm"), []byte(command+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(process comm %d) error = %v", pid, err)
	}
}

func writeProcessCmdlineFixture(t *testing.T, procDir string, pid int, args []string) {
	t.Helper()

	processDir := filepath.Join(procDir, strconv.Itoa(pid))
	if err := os.WriteFile(filepath.Join(processDir, "cmdline"), []byte(strings.Join(args, "\x00")+"\x00"), 0o644); err != nil {
		t.Fatalf("WriteFile(process cmdline %d) error = %v", pid, err)
	}
}

func processStatLine(pid int, command, state string, utime, stime uint64, rssPages int64) string {
	fields := []string{
		state,
		"0", "0", "0", "0", "0", "0", "0", "0", "0", "0",
		strconv.FormatUint(utime, 10),
		strconv.FormatUint(stime, 10),
		"0", "0", "20", "0", "1", "0", "0", "0",
		strconv.FormatInt(rssPages, 10),
	}
	return strconv.Itoa(pid) + " (" + command + ") " + strings.Join(fields, " ") + "\n"
}

type fakeHostSettingsStore struct {
	values map[string]string
}

func (f *fakeHostSettingsStore) AppSetting(ctx context.Context, key string) (string, bool, error) {
	value, ok := f.values[key]
	return value, ok, nil
}

func (f *fakeHostSettingsStore) SetAppSetting(ctx context.Context, key, valueJSON string, updatedAt time.Time) error {
	f.values[key] = valueJSON
	return nil
}
