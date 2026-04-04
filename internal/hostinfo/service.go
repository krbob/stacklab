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

var ErrLogsUnavailable = errors.New("stacklab logs unavailable")

type commandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

type Service struct {
	cfg              config.Config
	startedAt        time.Time
	procRoot         string
	osReleasePath    string
	stacklabUnitName string
	runCommand       commandRunner
	mu               sync.Mutex
	lastCPUSample    cpuSample
	hasCPUSample     bool
}

type cpuSample struct {
	total uint64
	idle  uint64
}

func NewService(cfg config.Config, startedAt time.Time) *Service {
	return &Service{
		cfg:              cfg,
		startedAt:        startedAt.UTC(),
		procRoot:         "/proc",
		osReleasePath:    "/etc/os-release",
		stacklabUnitName: cfg.SystemdUnitName,
		runCommand:       defaultCommandRunner,
	}
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

	filtered := filterLogEntries(entries, strings.TrimSpace(strings.ToLower(query.Level)), strings.TrimSpace(strings.ToLower(query.Search)))
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

func filterLogEntries(entries []StacklabLogEntry, level, search string) []StacklabLogEntry {
	filtered := make([]StacklabLogEntry, 0, len(entries))
	for _, entry := range entries {
		if level != "" && entry.Level != level {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(entry.Message), search) {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
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
	data, err := os.ReadFile(filepath.Join(s.procRoot, "loadavg"))
	if err != nil {
		return []float64{0, 0, 0}
	}
	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return []float64{0, 0, 0}
	}
	result := make([]float64, 0, 3)
	for i := 0; i < 3; i++ {
		value, err := strconv.ParseFloat(fields[i], 64)
		if err != nil {
			result = append(result, 0)
			continue
		}
		result = append(result, roundFloat(value))
	}
	return result
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
	data, err := os.ReadFile(filepath.Join(s.procRoot, "stat"))
	if err != nil {
		return cpuSample{}, false
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			return cpuSample{}, false
		}

		var values []uint64
		for _, field := range fields[1:] {
			value, err := strconv.ParseUint(field, 10, 64)
			if err != nil {
				return cpuSample{}, false
			}
			values = append(values, value)
		}
		var total uint64
		for _, value := range values {
			total += value
		}

		idle := values[3]
		if len(values) > 4 {
			idle += values[4]
		}
		return cpuSample{total: total, idle: idle}, true
	}
	return cpuSample{}, false
}

func (s *Service) readMemoryUsage() MemoryUsage {
	data, err := os.ReadFile(filepath.Join(s.procRoot, "meminfo"))
	if err != nil {
		return MemoryUsage{}
	}

	values := map[string]uint64{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		fields := strings.Fields(strings.TrimSpace(parts[1]))
		if len(fields) == 0 {
			continue
		}
		value, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			continue
		}
		values[parts[0]] = value * 1024
	}

	total := values["MemTotal"]
	available := values["MemAvailable"]
	if available == 0 {
		available = values["MemFree"]
	}
	used := uint64(0)
	if total >= available {
		used = total - available
	}
	usagePercent := 0.0
	if total > 0 {
		usagePercent = roundFloat((float64(used) / float64(total)) * 100)
	}

	return MemoryUsage{
		TotalBytes:     total,
		UsedBytes:      used,
		AvailableBytes: available,
		UsagePercent:   usagePercent,
	}
}

func (s *Service) readDiskUsage() DiskUsage {
	path := s.cfg.RootDir
	var stats syscall.Statfs_t
	if err := syscall.Statfs(path, &stats); err != nil {
		return DiskUsage{Path: path}
	}

	total := stats.Blocks * uint64(stats.Bsize)
	available := stats.Bavail * uint64(stats.Bsize)
	used := total - (stats.Bfree * uint64(stats.Bsize))

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
