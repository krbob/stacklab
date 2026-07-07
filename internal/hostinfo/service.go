package hostinfo

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"stacklab/internal/config"
	"stacklab/internal/stacks"
)

const (
	settingsKey      = "host_observability_v1"
	settingsCacheTTL = 5 * time.Second
)

var ErrLogsUnavailable = errors.New("stacklab logs unavailable")

type commandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

type settingsStore interface {
	AppSetting(ctx context.Context, key string) (string, bool, error)
	SetAppSetting(ctx context.Context, key, valueJSON string, updatedAt time.Time) error
}

type Service struct {
	cfg              config.Config
	startedAt        time.Time
	procRoot         string
	osReleasePath    string
	stacklabUnitName string
	runCommand       commandRunner
	metrics          *MetricsCollector
	settingsStore    settingsStore
	settingsMu       sync.RWMutex
	settingsCache    SettingsResponse
	settingsCachedAt time.Time
	mu               sync.Mutex
	lastCPUSample    cpuSample
	hasCPUSample     bool
}

type cpuSample struct {
	total uint64
	idle  uint64
}

func NewService(cfg config.Config, startedAt time.Time) *Service {
	return NewServiceWithStore(cfg, startedAt, nil)
}

func NewServiceWithStore(cfg config.Config, startedAt time.Time, appStore settingsStore) *Service {
	metrics := newMetricsCollector(cfg.RootDir, "/proc")
	service := &Service{
		cfg:              cfg,
		startedAt:        startedAt.UTC(),
		procRoot:         "/proc",
		osReleasePath:    "/etc/os-release",
		stacklabUnitName: cfg.SystemdUnitName,
		runCommand:       defaultCommandRunner,
		metrics:          metrics,
		settingsStore:    appStore,
	}
	metrics.publicIPLookupEnabled = service.publicIPLookupEnabled
	return service
}

func defaultCommandRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func (s *Service) Overview(ctx context.Context) (OverviewResponse, error) {
	hostname, _ := os.Hostname()
	osName := s.readOSName()
	kernelVersion := readKernelVersion()
	uptimeSeconds := s.readUptimeSeconds()
	cpuUsage := s.readCPUUsage()
	memoryUsage := s.readMemoryUsage()
	diskUsage := s.readDiskUsage()

	return OverviewResponse{
		Host: HostMeta{
			Hostname:      hostname,
			OSName:        osName,
			KernelVersion: kernelVersion,
			Architecture:  runtime.GOOS + "-" + runtime.GOARCH,
			UptimeSeconds: uptimeSeconds,
		},
		Stacklab: StacklabMeta{
			Version:   stacks.AppVersion,
			Commit:    stacks.AppCommit,
			StartedAt: s.startedAt,
		},
		Docker: DockerMeta{
			EngineVersion:  detectDockerEngineVersion(ctx),
			ComposeVersion: detectComposeVersion(ctx),
		},
		Resources: ResourceUsage{
			CPU:    cpuUsage,
			Memory: memoryUsage,
			Disk:   diskUsage,
		},
	}, nil
}

func (s *Service) StartMetrics(ctx context.Context) {
	go s.metrics.Start(ctx)
}

func (s *Service) Metrics(ctx context.Context, query MetricsQuery) (MetricsResponse, error) {
	return s.metrics.Snapshot(query), nil
}

func (s *Service) GetSettings(ctx context.Context) (SettingsResponse, error) {
	return s.loadSettings(ctx)
}

func (s *Service) UpdateSettings(ctx context.Context, request UpdateSettingsRequest) (SettingsResponse, error) {
	settings := SettingsResponse(request)
	if err := s.saveSettings(ctx, settings); err != nil {
		return SettingsResponse{}, err
	}
	s.setSettingsCache(settings)
	if !settings.PublicIPLookupEnabled {
		s.metrics.clearPublicIP()
	}
	return settings, nil
}

func (s *Service) publicIPLookupEnabled() bool {
	settings, err := s.cachedSettings(context.Background())
	if err != nil {
		return s.defaultSettings().PublicIPLookupEnabled
	}
	return settings.PublicIPLookupEnabled
}

func (s *Service) cachedSettings(ctx context.Context) (SettingsResponse, error) {
	now := time.Now().UTC()
	s.settingsMu.RLock()
	if !s.settingsCachedAt.IsZero() && now.Sub(s.settingsCachedAt) < settingsCacheTTL {
		settings := s.settingsCache
		s.settingsMu.RUnlock()
		return settings, nil
	}
	s.settingsMu.RUnlock()

	settings, err := s.loadSettings(ctx)
	if err != nil {
		return SettingsResponse{}, err
	}
	s.setSettingsCache(settings)
	return settings, nil
}

func (s *Service) setSettingsCache(settings SettingsResponse) {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	s.settingsCache = settings
	s.settingsCachedAt = time.Now().UTC()
}

func (s *Service) loadSettings(ctx context.Context) (SettingsResponse, error) {
	settings := s.defaultSettings()
	if s.settingsStore == nil {
		return settings, nil
	}

	raw, ok, err := s.settingsStore.AppSetting(ctx, settingsKey)
	if err != nil {
		return SettingsResponse{}, err
	}
	if !ok {
		return settings, nil
	}
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		return SettingsResponse{}, fmt.Errorf("parse host observability settings: %w", err)
	}
	return settings, nil
}

func (s *Service) saveSettings(ctx context.Context, settings SettingsResponse) error {
	if s.settingsStore == nil {
		return nil
	}
	payload, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("marshal host observability settings: %w", err)
	}
	return s.settingsStore.SetAppSetting(ctx, settingsKey, string(payload), time.Now().UTC())
}

func (s *Service) defaultSettings() SettingsResponse {
	return SettingsResponse{
		PublicIPLookupEnabled: s.cfg.HostPublicIPLookupEnabled,
	}
}

func (s *Service) StacklabLogs(ctx context.Context, query LogsQuery) (StacklabLogsResponse, error) {
	limit := query.Limit
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}

	args := []string{
		"-u", s.stacklabUnitName,
		"--no-pager",
		"--output=json",
	}
	if query.Cursor != "" {
		args = append(args, "--after-cursor", query.Cursor)
	}
	args = append(args, "-n", strconv.Itoa(limit))

	output, err := s.runCommand(ctx, "journalctl", args...)
	if err != nil {
		return StacklabLogsResponse{}, fmt.Errorf("%w: %v", ErrLogsUnavailable, err)
	}

	entries, err := parseJournalEntries(output)
	if err != nil {
		return StacklabLogsResponse{}, fmt.Errorf("parse journal output: %w", err)
	}

	filtered := filterLogEntries(entries, strings.TrimSpace(strings.ToLower(query.Level)), strings.TrimSpace(strings.ToLower(query.Search)), query.IncludeHTTPAccess)
	response := StacklabLogsResponse{
		Items:   filtered,
		HasMore: len(entries) >= limit,
	}
	if len(filtered) > 0 {
		response.NextCursor = filtered[len(filtered)-1].Cursor
	}

	return response, nil
}

func parseJournalEntries(output []byte) ([]StacklabLogEntry, error) {
	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)

	entries := make([]StacklabLogEntry, 0)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 || bytes.Equal(line, []byte("-- No entries --")) {
			continue
		}

		var raw map[string]any
		if err := json.Unmarshal(line, &raw); err != nil {
			return nil, err
		}

		entry := StacklabLogEntry{
			Timestamp: parseJournalTimestamp(raw["__REALTIME_TIMESTAMP"]),
			Level:     normalizeJournalLevel(raw["PRIORITY"]),
			Message:   journalValueToString(raw["MESSAGE"]),
			Cursor:    journalValueToString(raw["__CURSOR"]),
		}
		if entry.Message == "" {
			continue
		}
		entries = append(entries, entry)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return entries, nil
}

func filterLogEntries(entries []StacklabLogEntry, level, search string, includeHTTPAccess bool) []StacklabLogEntry {
	filtered := make([]StacklabLogEntry, 0, len(entries))
	for _, entry := range entries {
		if level != "" && entry.Level != level {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(entry.Message), search) {
			continue
		}
		if !includeHTTPAccess && entry.Level == "info" && isHTTPAccessLogEntry(entry.Message) {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func isHTTPAccessLogEntry(message string) bool {
	normalized := strings.ToLower(strings.TrimSpace(message))
	return normalized == "http request" ||
		strings.Contains(normalized, `msg="http request"`) ||
		strings.Contains(normalized, `"msg":"http request"`) ||
		strings.Contains(normalized, "msg=http request")
}

func parseJournalTimestamp(value any) time.Time {
	raw := journalValueToString(value)
	if raw == "" {
		return time.Time{}
	}

	micros, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return time.Time{}
	}

	return time.UnixMicro(micros).UTC()
}

func normalizeJournalLevel(value any) string {
	raw := journalValueToString(value)
	switch raw {
	case "0", "1", "2", "3":
		return "error"
	case "4":
		return "warn"
	case "5", "6":
		return "info"
	case "7":
		return "debug"
	default:
		return "info"
	}
}

func journalValueToString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatInt(int64(typed), 10)
	case []any:
		var builder strings.Builder
		for _, item := range typed {
			builder.WriteString(journalValueToString(item))
		}
		return builder.String()
	default:
		return ""
	}
}

func (s *Service) readOSName() string {
	data, err := os.ReadFile(s.osReleasePath)
	if err != nil {
		return runtime.GOOS
	}

	lines := strings.Split(string(data), "\n")
	values := make(map[string]string, len(lines))
	for _, line := range lines {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		values[parts[0]] = strings.Trim(parts[1], "\"")
	}
	if pretty := values["PRETTY_NAME"]; pretty != "" {
		return pretty
	}
	if name := values["NAME"]; name != "" {
		return name
	}
	return runtime.GOOS
}

func readKernelVersion() string {
	output, err := exec.Command("uname", "-r").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func (s *Service) readUptimeSeconds() int64 {
	data, err := os.ReadFile(filepath.Join(s.procRoot, "uptime"))
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0
	}
	value, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0
	}
	return int64(value)
}

func (s *Service) readCPUUsage() CPUUsage {
	loadAverage := s.readLoadAverage()
	usagePercent := s.readCPUPercent()
	return CPUUsage{
		CoreCount:    runtime.NumCPU(),
		LoadAverage:  loadAverage,
		UsagePercent: usagePercent,
	}
}

func (s *Service) readLoadAverage() []float64 {
	return readProcLoadAverage(s.procRoot)
}

func (s *Service) readCPUPercent() float64 {
	sample, ok := s.readCPUSample()
	if !ok {
		return 0
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.hasCPUSample {
		s.lastCPUSample = sample
		s.hasCPUSample = true
		return 0
	}

	last := s.lastCPUSample
	s.lastCPUSample = sample

	totalDelta := sample.total - last.total
	idleDelta := sample.idle - last.idle
	if totalDelta == 0 || idleDelta > totalDelta {
		return 0
	}

	usage := (float64(totalDelta-idleDelta) / float64(totalDelta)) * 100
	return roundFloat(usage)
}

func (s *Service) readCPUSample() (cpuSample, bool) {
	return readProcCPUSample(s.procRoot)
}

func (s *Service) readMemoryUsage() MemoryUsage {
	return memoryUsageFromMemInfo(readProcMemInfoValues(s.procRoot))
}

func (s *Service) readDiskUsage() DiskUsage {
	path := s.cfg.RootDir
	var stats syscall.Statfs_t
	if err := syscall.Statfs(path, &stats); err != nil {
		return DiskUsage{Path: path}
	}

	blockSize := statfsBlockSize(stats)
	total := stats.Blocks * blockSize
	available := stats.Bavail * blockSize
	free := stats.Bfree * blockSize
	used := uint64(0)
	if total >= free {
		used = total - free
	}

	usagePercent := 0.0
	if total > 0 {
		usagePercent = roundFloat((float64(used) / float64(total)) * 100)
	}

	return DiskUsage{
		Path:           path,
		TotalBytes:     total,
		UsedBytes:      used,
		AvailableBytes: available,
		UsagePercent:   usagePercent,
	}
}

func roundFloat(value float64) float64 {
	return mathRound(value*10) / 10
}

func mathRound(value float64) float64 {
	if value < 0 {
		return float64(int64(value - 0.5))
	}
	return float64(int64(value + 0.5))
}

func detectDockerEngineVersion(ctx context.Context) string {
	cmd := exec.CommandContext(ctx, "docker", "version", "--format", "{{.Server.Version}}")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(output))
}

func detectComposeVersion(ctx context.Context) string {
	command, args := composeVersionCommand(ctx)
	if command == "" {
		return ""
	}

	output, err := exec.CommandContext(ctx, command, args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func composeVersionCommand(ctx context.Context) (string, []string) {
	candidates := []struct {
		command string
		args    []string
	}{
		{command: "docker", args: []string{"compose", "version", "--short"}},
		{command: "docker-compose", args: []string{"version", "--short"}},
		{command: "docker-compose", args: []string{"version"}},
	}
	for _, candidate := range candidates {
		cmd := exec.CommandContext(ctx, candidate.command, candidate.args...)
		output, err := cmd.Output()
		if err != nil {
			continue
		}
		version := strings.TrimSpace(string(output))
		if version != "" {
			return candidate.command, candidate.args
		}
	}
	return "", nil
}
