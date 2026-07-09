package hostinfo

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	metricsActiveSampleInterval     = time.Second
	metricsBackgroundSampleInterval = 30 * time.Second
	metricsActiveTTL                = 15 * time.Second
	metricsHistoryWindow            = 30 * time.Minute
	metricsMaxSamples               = int(metricsHistoryWindow / metricsActiveSampleInterval)
	processesMaxItems               = 30
	processesPerMetricLimit         = 15
	processesSampleInterval         = 5 * time.Second
	processCommandMaxLength         = 80
	processDisplayCommandMaxLength  = 180
	processCmdlineReadLimit         = 4096
	processContainerMetadataTTL     = 30 * time.Second
	processContainerInspectTimeout  = 2 * time.Second
	filesystemStatfsTimeout         = 500 * time.Millisecond
	publicIPCacheTTL                = 10 * time.Minute
	publicIPFailureRetryInterval    = time.Minute
	publicIPFetchTimeout            = 2 * time.Second
	publicIPLookupURL               = "https://api64.ipify.org"
)

var (
	errPublicIPLookupPanic = errors.New("public IP lookup panic")
	errStatfsTimedOut      = errors.New("statfs timed out")
)

type MetricsCollector struct {
	procRoot           string
	sysRoot            string
	rootDir            string
	activeInterval     time.Duration
	backgroundInterval time.Duration
	activeTTL          time.Duration
	historyWindow      time.Duration
	maxSamples         int
	now                func() time.Time
	statfs             func(string, *syscall.Statfs_t) error
	statfsTimeout      time.Duration
	wakeCh             chan struct{}

	sampleMu              sync.Mutex
	mu                    sync.RWMutex
	samples               []HostMetricSample
	latestProcesses       *ProcessUsage
	activeUntil           time.Time
	lastCPU               cpuSample
	hasLastCPU            bool
	lastDiskIO            map[string]diskIOCounter
	lastDiskIOAt          time.Time
	lastNetwork           map[string]networkCounter
	lastNetworkAt         time.Time
	lastProcesses         map[int]processCPUCounter
	lastProcessSystemCPU  uint64
	lastProcessUsage      *ProcessUsage
	lastProcessSampledAt  time.Time
	statfsCache           map[string]syscall.Statfs_t
	processSampleInterval time.Duration
	usernamesByUID        map[uint32]string
	usernamesLoaded       bool
	containerMetadataByID map[string]ProcessContainerInfo
	containerMetadataAt   time.Time
	containerMetadataTTL  time.Duration
	containerResolver     func() map[string]ProcessContainerInfo

	publicIPMu              sync.Mutex
	publicIP                string
	publicIPCheckedAt       time.Time
	publicIPRefreshInFlight bool
	publicIPLookupEnabled   func() bool
	publicIPResolver        func(context.Context) (string, error)
}

type diskIOCounter struct {
	readBytes  uint64
	writeBytes uint64
}

type networkCounter struct {
	rxBytes uint64
	txBytes uint64
}

type processCPUCounter struct {
	ticks uint64
}

type processStatSample struct {
	pid      int
	command  string
	state    string
	ticks    uint64
	rssBytes uint64
}

type dockerProcessContainerInspect struct {
	ID     string `json:"Id"`
	Name   string `json:"Name"`
	Config struct {
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
}

type mountInfo struct {
	root       string
	mountPoint string
	majorMinor string
	device     string
	fsType     string
}

type filesystemCandidate struct {
	mount           mountInfo
	usage           FilesystemUsage
	coversRootDir   bool
	wholeFilesystem bool
}

func newMetricsCollector(rootDir, procRoot string) *MetricsCollector {
	collector := &MetricsCollector{
		procRoot:              procRoot,
		sysRoot:               "/sys",
		rootDir:               rootDir,
		activeInterval:        metricsActiveSampleInterval,
		backgroundInterval:    metricsBackgroundSampleInterval,
		activeTTL:             metricsActiveTTL,
		historyWindow:         metricsHistoryWindow,
		maxSamples:            metricsMaxSamples,
		processSampleInterval: processesSampleInterval,
		now:                   time.Now,
		statfs:                syscall.Statfs,
		statfsTimeout:         filesystemStatfsTimeout,
		wakeCh:                make(chan struct{}, 1),
		containerMetadataTTL:  processContainerMetadataTTL,
		lastDiskIO:            map[string]diskIOCounter{},
		lastNetwork:           map[string]networkCounter{},
		lastProcesses:         map[int]processCPUCounter{},
		statfsCache:           map[string]syscall.Statfs_t{},
		publicIPLookupEnabled: func() bool { return false },
		publicIPResolver:      defaultPublicIPResolver,
	}
	if procRoot == "/proc" {
		collector.containerResolver = collector.readDockerProcessContainers
	}
	return collector
}

func (c *MetricsCollector) Start(ctx context.Context) {
	c.sampleAndStore()

	for {
		timer := time.NewTimer(c.currentInterval())
		select {
		case <-ctx.Done():
			stopTimer(timer)
			return
		case <-timer.C:
			c.sampleAndStore()
		case <-c.wakeCh:
			stopTimer(timer)
		}
	}
}

func (c *MetricsCollector) Snapshot(query MetricsQuery) MetricsResponse {
	c.markActive()
	if c.shouldSample(c.activeInterval) {
		c.sampleAndStore()
	}

	now := c.now().UTC()
	c.mu.RLock()
	allHistory := append([]HostMetricSample(nil), c.samples...)
	processes := cloneProcessUsage(c.latestProcesses)
	interval := c.currentIntervalLocked(now)
	c.mu.RUnlock()

	var current *HostMetricSample
	if len(allHistory) > 0 {
		currentSample := allHistory[len(allHistory)-1]
		currentSample.Processes = processes
		current = &currentSample
	}
	history := allHistory
	if query.Since != nil {
		history = filterMetricHistorySince(allHistory, *query.Since)
	}

	return MetricsResponse{
		SampleIntervalSeconds:           int(interval.Seconds()),
		BackgroundSampleIntervalSeconds: int(c.backgroundInterval.Seconds()),
		ActiveSampleIntervalSeconds:     int(c.activeInterval.Seconds()),
		HistoryWindowSeconds:            int(c.historyWindow.Seconds()),
		Current:                         current,
		History:                         history,
	}
}

func filterMetricHistorySince(history []HostMetricSample, since time.Time) []HostMetricSample {
	filtered := make([]HostMetricSample, 0, len(history))
	for _, sample := range history {
		if sample.SampledAt.After(since) {
			filtered = append(filtered, sample)
		}
	}
	return filtered
}

func (c *MetricsCollector) markActive() {
	now := c.now().UTC()
	activeUntil := now.Add(c.activeTTL)

	c.mu.Lock()
	if activeUntil.After(c.activeUntil) {
		c.activeUntil = activeUntil
	}
	c.mu.Unlock()

	select {
	case c.wakeCh <- struct{}{}:
	default:
	}
}

func (c *MetricsCollector) currentInterval() time.Duration {
	now := c.now().UTC()
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.currentIntervalLocked(now)
}

func (c *MetricsCollector) currentIntervalLocked(now time.Time) time.Duration {
	if now.Before(c.activeUntil) {
		return c.activeInterval
	}
	return c.backgroundInterval
}

func (c *MetricsCollector) shouldSample(maxAge time.Duration) bool {
	now := c.now().UTC()

	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.samples) == 0 {
		return true
	}
	return now.Sub(c.samples[len(c.samples)-1].SampledAt) >= maxAge
}

func (c *MetricsCollector) sampleAndStore() {
	c.sampleMu.Lock()
	defer c.sampleMu.Unlock()

	sample := c.readSample()
	processes := sample.Processes
	sample.Processes = nil

	c.mu.Lock()
	c.latestProcesses = processes
	c.samples = append(c.samples, sample)
	c.pruneSamplesLocked(sample.SampledAt)
	if len(c.samples) > c.maxSamples {
		c.samples = append([]HostMetricSample(nil), c.samples[len(c.samples)-c.maxSamples:]...)
	}
	c.mu.Unlock()
}

func (c *MetricsCollector) pruneSamplesLocked(now time.Time) {
	cutoff := now.Add(-c.historyWindow)
	first := 0
	for first < len(c.samples) && c.samples[first].SampledAt.Before(cutoff) {
		first++
	}
	if first > 0 {
		c.samples = append([]HostMetricSample(nil), c.samples[first:]...)
	}
}

func (c *MetricsCollector) readSample() HostMetricSample {
	now := c.now().UTC()
	memInfo := readProcMemInfoValues(c.procRoot)
	return HostMetricSample{
		SampledAt:    now,
		CPU:          c.readCPUUsage(),
		Memory:       memoryUsageFromMemInfo(memInfo),
		Swap:         swapUsageFromMemInfo(memInfo),
		Temperatures: c.readTemperatureUsage(),
		Filesystems:  c.readFilesystems(),
		DiskIO:       c.readDiskIOUsage(now),
		Network:      c.readNetworkUsage(now),
		Processes:    c.readProcessUsageForSample(now, memInfo),
	}
}

func (c *MetricsCollector) readCPUUsage() CPUUsage {
	return CPUUsage{
		CoreCount:    runtime.NumCPU(),
		LoadAverage:  readProcLoadAverage(c.procRoot),
		UsagePercent: c.readCPUPercent(),
	}
}

func (c *MetricsCollector) readCPUPercent() float64 {
	sample, ok := readProcCPUSample(c.procRoot)
	if !ok {
		return 0
	}

	if !c.hasLastCPU {
		c.lastCPU = sample
		c.hasLastCPU = true
		return 0
	}

	last := c.lastCPU
	c.lastCPU = sample
	if sample.total < last.total || sample.idle < last.idle {
		return 0
	}

	totalDelta := sample.total - last.total
	idleDelta := sample.idle - last.idle
	if totalDelta == 0 || idleDelta > totalDelta {
		return 0
	}

	return roundFloat((float64(totalDelta-idleDelta) / float64(totalDelta)) * 100)
}

func cloneProcessUsage(usage *ProcessUsage) *ProcessUsage {
	if usage == nil {
		return nil
	}
	cloned := ProcessUsage{
		Total: usage.Total,
		Items: append([]ProcessInfo(nil), usage.Items...),
	}
	for index := range cloned.Items {
		if cloned.Items[index].Container != nil {
			container := *cloned.Items[index].Container
			cloned.Items[index].Container = &container
		}
	}
	if cloned.Items == nil {
		cloned.Items = []ProcessInfo{}
	}
	return &cloned
}

func (c *MetricsCollector) readProcessUsageForSample(now time.Time, memInfo map[string]uint64) *ProcessUsage {
	if c.lastProcessUsage != nil && now.Sub(c.lastProcessSampledAt) < c.processSampleInterval {
		return cloneProcessUsage(c.lastProcessUsage)
	}
	usage := c.readProcessUsage(memInfo)
	c.lastProcessUsage = cloneProcessUsage(usage)
	c.lastProcessSampledAt = now
	return usage
}

func (c *MetricsCollector) readProcessUsage(memInfo map[string]uint64) *ProcessUsage {
	entries, err := os.ReadDir(c.procRoot)
	if err != nil {
		return &ProcessUsage{Items: []ProcessInfo{}}
	}

	systemSample, hasSystemSample := readProcCPUSample(c.procRoot)
	systemDelta := uint64(0)
	if hasSystemSample && c.lastProcessSystemCPU > 0 && systemSample.total >= c.lastProcessSystemCPU {
		systemDelta = systemSample.total - c.lastProcessSystemCPU
	}

	memTotal := memInfo["MemTotal"]
	pageSize := os.Getpagesize()
	currentCounters := map[int]processCPUCounter{}
	userCache := map[uint32]string{}
	containerMetadata := c.processContainerMetadata()
	processes := []ProcessInfo{}

	for _, entry := range entries {
		if !entry.IsDir() || !isNumericProcessDir(entry.Name()) {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}

		processDir := filepath.Join(c.procRoot, entry.Name())
		sample, ok := c.readProcessStat(pid, pageSize)
		if !ok {
			continue
		}
		currentCounters[pid] = processCPUCounter{ticks: sample.ticks}

		cpuPercent := 0.0
		if systemDelta > 0 {
			if previous, ok := c.lastProcesses[pid]; ok && sample.ticks >= previous.ticks {
				cpuPercent = roundFloat((float64(sample.ticks-previous.ticks) / float64(systemDelta)) * float64(runtime.NumCPU()) * 100)
			}
		}

		memoryPercent := 0.0
		if memTotal > 0 {
			memoryPercent = roundFloat((float64(sample.rssBytes) / float64(memTotal)) * 100)
		}

		command := c.readProcessCommand(pid, sample.command)
		processes = append(processes, ProcessInfo{
			PID:            pid,
			User:           c.processOwner(processDir, userCache),
			State:          sample.state,
			CPUPercent:     cpuPercent,
			MemoryBytes:    sample.rssBytes,
			MemoryPercent:  memoryPercent,
			Command:        command,
			DisplayCommand: c.readProcessDisplayCommand(pid, command),
			Container:      c.processContainer(processDir, containerMetadata),
		})
	}

	if hasSystemSample {
		c.lastProcessSystemCPU = systemSample.total
	}
	c.lastProcesses = currentCounters

	return &ProcessUsage{
		Total: len(processes),
		Items: selectTopProcesses(processes),
	}
}

func (c *MetricsCollector) readProcessStat(pid int, pageSize int) (processStatSample, bool) {
	data, err := os.ReadFile(filepath.Join(c.procRoot, strconv.Itoa(pid), "stat"))
	if err != nil {
		return processStatSample{}, false
	}
	return parseProcessStat(pid, string(data), pageSize)
}

func (c *MetricsCollector) processContainerMetadata() map[string]ProcessContainerInfo {
	now := c.now().UTC()
	if c.containerMetadataByID != nil && c.containerMetadataTTL > 0 && now.Sub(c.containerMetadataAt) < c.containerMetadataTTL {
		return c.containerMetadataByID
	}
	if c.containerResolver == nil {
		c.containerMetadataByID = map[string]ProcessContainerInfo{}
		c.containerMetadataAt = now
		return c.containerMetadataByID
	}
	metadata := c.containerResolver()
	if metadata == nil {
		metadata = map[string]ProcessContainerInfo{}
	}
	c.containerMetadataByID = metadata
	c.containerMetadataAt = now
	return metadata
}

func (c *MetricsCollector) readDockerProcessContainers() map[string]ProcessContainerInfo {
	ctx, cancel := context.WithTimeout(context.Background(), processContainerInspectTimeout)
	defer cancel()

	output, err := exec.CommandContext(ctx, "docker", "ps", "-q").Output()
	if err != nil {
		return map[string]ProcessContainerInfo{}
	}
	ids := strings.Fields(strings.TrimSpace(string(output)))
	if len(ids) == 0 {
		return map[string]ProcessContainerInfo{}
	}

	args := append([]string{"inspect"}, ids...)
	inspectOutput, err := exec.CommandContext(ctx, "docker", args...).Output()
	if err != nil {
		return map[string]ProcessContainerInfo{}
	}

	var inspected []dockerProcessContainerInspect
	if err := json.Unmarshal(inspectOutput, &inspected); err != nil {
		return map[string]ProcessContainerInfo{}
	}

	containers := make(map[string]ProcessContainerInfo, len(inspected))
	for _, container := range inspected {
		id := normalizeContainerID(container.ID)
		if id == "" {
			continue
		}
		labels := container.Config.Labels
		containers[id] = ProcessContainerInfo{
			ID:          id,
			Name:        strings.TrimPrefix(container.Name, "/"),
			StackID:     labels["com.docker.compose.project"],
			ServiceName: labels["com.docker.compose.service"],
		}
	}
	return containers
}

func (c *MetricsCollector) processContainer(processDir string, metadata map[string]ProcessContainerInfo) *ProcessContainerInfo {
	containerID := c.readProcessContainerID(processDir)
	if containerID == "" {
		return nil
	}

	if info, ok := metadata[containerID]; ok {
		return cloneProcessContainerInfo(info)
	}
	for id, info := range metadata {
		if containerIDsMatch(containerID, id) {
			return cloneProcessContainerInfo(info)
		}
	}

	return &ProcessContainerInfo{
		ID:   containerID,
		Name: shortContainerID(containerID),
	}
}

func cloneProcessContainerInfo(info ProcessContainerInfo) *ProcessContainerInfo {
	cloned := info
	return &cloned
}

func (c *MetricsCollector) readProcessContainerID(processDir string) string {
	data, err := os.ReadFile(filepath.Join(processDir, "cgroup"))
	if err != nil {
		return ""
	}
	return parseDockerContainerIDFromCgroup(string(data))
}

func parseDockerContainerIDFromCgroup(data string) string {
	for _, line := range strings.Split(data, "\n") {
		if id := dockerContainerIDAfter(line, "docker-"); id != "" {
			return id
		}
		if id := dockerContainerIDAfter(line, "docker/"); id != "" {
			return id
		}
	}
	return ""
}

func dockerContainerIDAfter(value, marker string) string {
	searchFrom := 0
	for {
		index := strings.Index(value[searchFrom:], marker)
		if index < 0 {
			return ""
		}
		start := searchFrom + index + len(marker)
		id := normalizeContainerID(readHexPrefix(value[start:]))
		if len(id) == 64 {
			return id
		}
		searchFrom = start
	}
}

func readHexPrefix(value string) string {
	var builder strings.Builder
	for _, char := range value {
		if !isHexChar(char) {
			break
		}
		builder.WriteRune(char)
	}
	return builder.String()
}

func normalizeContainerID(value string) string {
	value = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(value, "sha256:")))
	if len(value) < 12 {
		return ""
	}
	for _, char := range value {
		if !isHexChar(char) {
			return ""
		}
	}
	return value
}

func isHexChar(char rune) bool {
	return (char >= '0' && char <= '9') || (char >= 'a' && char <= 'f') || (char >= 'A' && char <= 'F')
}

func containerIDsMatch(left, right string) bool {
	left = normalizeContainerID(left)
	right = normalizeContainerID(right)
	if len(left) < 12 || len(right) < 12 {
		return false
	}
	return left == right || strings.HasPrefix(left, right) || strings.HasPrefix(right, left)
}

func shortContainerID(id string) string {
	id = normalizeContainerID(id)
	if len(id) <= 12 {
		return id
	}
	return id[:12]
}

func parseProcessStat(pid int, statLine string, pageSize int) (processStatSample, bool) {
	left := strings.Index(statLine, "(")
	right := strings.LastIndex(statLine, ")")
	if left < 0 || right <= left {
		return processStatSample{}, false
	}

	fields := strings.Fields(statLine[right+1:])
	if len(fields) < 22 {
		return processStatSample{}, false
	}

	utime, err := strconv.ParseUint(fields[11], 10, 64)
	if err != nil {
		return processStatSample{}, false
	}
	stime, err := strconv.ParseUint(fields[12], 10, 64)
	if err != nil {
		return processStatSample{}, false
	}
	rssPages, err := strconv.ParseInt(fields[21], 10, 64)
	if err != nil {
		return processStatSample{}, false
	}

	rssBytes := uint64(0)
	if rssPages > 0 && pageSize > 0 {
		rssBytes = uint64(rssPages) * uint64(pageSize)
	}

	return processStatSample{
		pid:      pid,
		command:  sanitizeProcessCommand(statLine[left+1:right], pid),
		state:    fields[0],
		ticks:    utime + stime,
		rssBytes: rssBytes,
	}, true
}

func (c *MetricsCollector) readProcessCommand(pid int, fallback string) string {
	raw := readTrimmedFile(filepath.Join(c.procRoot, strconv.Itoa(pid), "comm"))
	if strings.TrimSpace(raw) != "" {
		return sanitizeProcessCommand(raw, pid)
	}
	return fallback
}

func sanitizeProcessCommand(command string, pid int) string {
	command = strings.TrimSpace(strings.ReplaceAll(command, "\x00", " "))
	if command == "" {
		return "pid " + strconv.Itoa(pid)
	}
	if len(command) > processCommandMaxLength {
		return strings.TrimSpace(command[:processCommandMaxLength])
	}
	return command
}

func (c *MetricsCollector) readProcessDisplayCommand(pid int, fallback string) string {
	args := c.readProcessArgs(pid)
	if len(args) == 0 {
		return fallback
	}
	args = redactProcessArgs(args)
	args = compactProcessArgs(args)
	display := sanitizeProcessDisplayCommand(strings.Join(args, " "), pid)
	if display == "" {
		return fallback
	}
	return display
}

func (c *MetricsCollector) readProcessArgs(pid int) []string {
	file, err := os.Open(filepath.Join(c.procRoot, strconv.Itoa(pid), "cmdline"))
	if err != nil {
		return nil
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, processCmdlineReadLimit+1))
	if err != nil || len(data) == 0 {
		return nil
	}
	if len(data) > processCmdlineReadLimit {
		data = data[:processCmdlineReadLimit]
	}

	parts := bytes.Split(data, []byte{0})
	args := make([]string, 0, len(parts))
	for _, part := range parts {
		arg := sanitizeProcessArg(string(part))
		if arg != "" {
			args = append(args, arg)
		}
	}
	return args
}

func sanitizeProcessArg(arg string) string {
	arg = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return ' '
		}
		return r
	}, arg)
	return strings.Join(strings.Fields(arg), " ")
}

func sanitizeProcessDisplayCommand(command string, pid int) string {
	command = strings.Join(strings.Fields(command), " ")
	if command == "" {
		return "pid " + strconv.Itoa(pid)
	}
	if len(command) > processDisplayCommandMaxLength {
		return strings.TrimSpace(command[:processDisplayCommandMaxLength])
	}
	return command
}

func redactProcessArgs(args []string) []string {
	redacted := make([]string, 0, len(args))
	redactNext := false
	for _, arg := range args {
		if arg == "" {
			continue
		}
		if redactNext {
			redacted = append(redacted, "[redacted]")
			redactNext = false
			continue
		}

		if processArgLooksSensitive(arg) {
			if key, _, ok := strings.Cut(arg, "="); ok {
				redacted = append(redacted, key+"=[redacted]")
			} else {
				redacted = append(redacted, arg)
				redactNext = true
			}
			continue
		}

		redacted = append(redacted, arg)
	}
	return redacted
}

func processArgLooksSensitive(arg string) bool {
	lower := strings.ToLower(arg)
	sensitiveTerms := []string{
		"password",
		"passwd",
		"secret",
		"token",
		"apikey",
		"api-key",
		"access-key",
		"private-key",
		"credential",
		"auth",
		"bearer",
	}
	for _, term := range sensitiveTerms {
		if strings.Contains(lower, term) {
			return true
		}
	}
	return false
}

func compactProcessArgs(args []string) []string {
	if len(args) == 0 {
		return args
	}
	executable := filepath.Base(args[0])
	switch strings.ToLower(executable) {
	case "java":
		return compactJavaProcessArgs(executable, args[1:])
	default:
		return args
	}
}

func compactJavaProcessArgs(executable string, args []string) []string {
	for i := 0; i < len(args); i++ {
		if args[i] == "-jar" && i+1 < len(args) {
			return append([]string{executable, "-jar", filepath.Base(args[i+1])}, args[i+2:]...)
		}
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if javaOptionConsumesNext(arg) {
			i++
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		return append([]string{executable, arg}, args[i+1:]...)
	}

	return append([]string{executable}, args...)
}

func javaOptionConsumesNext(arg string) bool {
	switch arg {
	case "-cp", "-classpath", "--class-path", "-p", "--module-path", "--upgrade-module-path", "--add-modules", "-m", "--module":
		return true
	default:
		return false
	}
}

func isNumericProcessDir(name string) bool {
	if name == "" {
		return false
	}
	for _, char := range name {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func (c *MetricsCollector) processOwner(processDir string, cache map[uint32]string) string {
	info, err := os.Stat(processDir)
	if err != nil {
		return ""
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return ""
	}
	uid := stat.Uid
	if cached, ok := cache[uid]; ok {
		return cached
	}

	uidString := strconv.FormatUint(uint64(uid), 10)
	if !c.usernamesLoaded {
		c.usernamesByUID = readPasswdUsernames("/etc/passwd")
		c.usernamesLoaded = true
	}
	username := c.usernamesByUID[uid]
	if username == "" {
		username = uidString
	}
	cache[uid] = username
	return username
}

func readPasswdUsernames(path string) map[uint32]string {
	data, err := os.ReadFile(path)
	if err != nil {
		return map[uint32]string{}
	}
	usernames := map[uint32]string{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, ":")
		if len(fields) < 3 || fields[0] == "" {
			continue
		}
		uid64, err := strconv.ParseUint(fields[2], 10, 32)
		if err != nil {
			continue
		}
		usernames[uint32(uid64)] = fields[0]
	}
	return usernames
}

func selectTopProcesses(processes []ProcessInfo) []ProcessInfo {
	if len(processes) == 0 {
		return []ProcessInfo{}
	}

	selected := map[int]ProcessInfo{}
	byCPU := append([]ProcessInfo(nil), processes...)
	sortProcessInfos(byCPU, "cpu")
	addTopProcesses(selected, byCPU, processesPerMetricLimit)

	byMemory := append([]ProcessInfo(nil), processes...)
	sortProcessInfos(byMemory, "memory")
	addTopProcesses(selected, byMemory, processesPerMetricLimit)

	result := make([]ProcessInfo, 0, len(selected))
	for _, process := range selected {
		result = append(result, process)
	}
	sortProcessInfos(result, "cpu")
	if len(result) > processesMaxItems {
		return append([]ProcessInfo(nil), result[:processesMaxItems]...)
	}
	return result
}

func addTopProcesses(selected map[int]ProcessInfo, processes []ProcessInfo, limit int) {
	if limit > len(processes) {
		limit = len(processes)
	}
	for i := 0; i < limit; i++ {
		selected[processes[i].PID] = processes[i]
	}
}

func sortProcessInfos(processes []ProcessInfo, by string) {
	sort.SliceStable(processes, func(i, j int) bool {
		left := processes[i]
		right := processes[j]
		switch by {
		case "memory":
			if left.MemoryBytes != right.MemoryBytes {
				return left.MemoryBytes > right.MemoryBytes
			}
			if left.CPUPercent != right.CPUPercent {
				return left.CPUPercent > right.CPUPercent
			}
		default:
			if left.CPUPercent != right.CPUPercent {
				return left.CPUPercent > right.CPUPercent
			}
			if left.MemoryBytes != right.MemoryBytes {
				return left.MemoryBytes > right.MemoryBytes
			}
		}
		if left.Command != right.Command {
			return left.Command < right.Command
		}
		return left.PID < right.PID
	})
}

func (c *MetricsCollector) readTemperatureUsage() TemperatureUsage {
	sensors := c.readHwmonTemperatureSensors()
	sensors = append(sensors, c.readThermalZoneTemperatureSensors()...)
	sensors = sortAndLimitTemperatureSensors(dedupeTemperatureSensors(sensors))
	if sensors == nil {
		sensors = []TemperatureSensor{}
	}

	usage := TemperatureUsage{
		Sensors: sensors,
	}
	if cpuSensor := selectCPUTemperatureSensor(sensors); cpuSensor != nil {
		usage.CPUSensor = cpuSensor
		value := roundFloat(cpuSensor.TemperatureCelsius)
		usage.CPUCelsius = &value
	}
	return usage
}

func (c *MetricsCollector) readHwmonTemperatureSensors() []TemperatureSensor {
	baseDir := filepath.Join(c.sysRoot, "class", "hwmon")
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return nil
	}

	sensors := []TemperatureSensor{}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "hwmon") {
			continue
		}
		dir := filepath.Join(baseDir, entry.Name())
		deviceName := readTrimmedFile(filepath.Join(dir, "name"))
		if deviceName == "" {
			deviceName = entry.Name()
		}

		tempEntries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, tempEntry := range tempEntries {
			filename := tempEntry.Name()
			if !strings.HasPrefix(filename, "temp") || !strings.HasSuffix(filename, "_input") {
				continue
			}
			index := strings.TrimSuffix(strings.TrimPrefix(filename, "temp"), "_input")
			temperature, ok := parseTemperatureCelsius(readTrimmedFile(filepath.Join(dir, filename)))
			if !ok {
				continue
			}

			sensors = append(sensors, TemperatureSensor{
				Name:               deviceName,
				Label:              readTrimmedFile(filepath.Join(dir, "temp"+index+"_label")),
				TemperatureCelsius: temperature,
			})
		}
	}

	return sensors
}

func (c *MetricsCollector) readThermalZoneTemperatureSensors() []TemperatureSensor {
	baseDir := filepath.Join(c.sysRoot, "class", "thermal")
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return nil
	}

	sensors := []TemperatureSensor{}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "thermal_zone") {
			continue
		}
		dir := filepath.Join(baseDir, entry.Name())
		temperature, ok := parseTemperatureCelsius(readTrimmedFile(filepath.Join(dir, "temp")))
		if !ok {
			continue
		}
		zoneType := readTrimmedFile(filepath.Join(dir, "type"))
		if zoneType == "" {
			zoneType = entry.Name()
		}
		sensors = append(sensors, TemperatureSensor{
			Name:               zoneType,
			Label:              entry.Name(),
			TemperatureCelsius: temperature,
		})
	}

	return sensors
}

func readTrimmedFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func parseTemperatureCelsius(raw string) (float64, bool) {
	if raw == "" {
		return 0, false
	}
	milliCelsius, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0, false
	}
	value := milliCelsius / 1000
	if value <= 0 || value > 150 {
		return 0, false
	}
	return roundFloat(value), true
}

func dedupeTemperatureSensors(sensors []TemperatureSensor) []TemperatureSensor {
	deduped := make([]TemperatureSensor, 0, len(sensors))
	seen := map[string]bool{}
	for _, sensor := range sensors {
		key := strings.ToLower(sensor.Name + "\x00" + sensor.Label + "\x00" + strconv.FormatFloat(sensor.TemperatureCelsius, 'f', 1, 64))
		if seen[key] {
			continue
		}
		seen[key] = true
		deduped = append(deduped, sensor)
	}
	return deduped
}

func sortAndLimitTemperatureSensors(sensors []TemperatureSensor) []TemperatureSensor {
	sort.SliceStable(sensors, func(i, j int) bool {
		leftCPULike := isCPUTemperatureSensor(sensors[i])
		rightCPULike := isCPUTemperatureSensor(sensors[j])
		if leftCPULike != rightCPULike {
			return leftCPULike
		}
		if sensors[i].TemperatureCelsius != sensors[j].TemperatureCelsius {
			return sensors[i].TemperatureCelsius > sensors[j].TemperatureCelsius
		}
		if sensors[i].Name != sensors[j].Name {
			return sensors[i].Name < sensors[j].Name
		}
		return sensors[i].Label < sensors[j].Label
	})

	const maxTemperatureSensors = 16
	if len(sensors) > maxTemperatureSensors {
		return append([]TemperatureSensor(nil), sensors[:maxTemperatureSensors]...)
	}
	return sensors
}

func selectCPUTemperatureSensor(sensors []TemperatureSensor) *TemperatureSensor {
	var selected TemperatureSensor
	selectedScore := 0
	found := false
	for _, sensor := range sensors {
		if !isCPUTemperatureSensor(sensor) {
			continue
		}
		score := cpuTemperatureSensorScore(sensor)
		if !found ||
			score < selectedScore ||
			(score == selectedScore && sensor.TemperatureCelsius > selected.TemperatureCelsius) ||
			(score == selectedScore && sensor.TemperatureCelsius == selected.TemperatureCelsius && temperatureSensorSortKey(sensor) < temperatureSensorSortKey(selected)) {
			selected = sensor
			selectedScore = score
			found = true
		}
	}
	if !found {
		return nil
	}
	return &selected
}

func isCPUTemperatureSensor(sensor TemperatureSensor) bool {
	haystack := strings.ToLower(sensor.Name + " " + sensor.Label)
	for _, hint := range []string{"cpu", "package", "core", "tctl", "tdie", "k10temp", "coretemp", "x86_pkg_temp", "soc"} {
		if strings.Contains(haystack, hint) {
			return true
		}
	}
	return false
}

func cpuTemperatureSensorScore(sensor TemperatureSensor) int {
	name := strings.ToLower(sensor.Name)
	label := strings.ToLower(sensor.Label)
	haystack := name + " " + label

	switch {
	case strings.Contains(label, "tctl"), strings.Contains(label, "tdie"):
		return 0
	case strings.Contains(label, "package"):
		return 1
	case strings.Contains(name, "x86_pkg_temp"):
		return 2
	case strings.Contains(label, "cpu"), strings.Contains(name, "cpu"):
		return 3
	case strings.Contains(label, "core"):
		return 4
	case strings.Contains(haystack, "soc"):
		return 5
	default:
		return 6
	}
}

func temperatureSensorSortKey(sensor TemperatureSensor) string {
	return strings.ToLower(sensor.Name + "\x00" + sensor.Label)
}

func (c *MetricsCollector) readFilesystems() []FilesystemUsage {
	mounts := c.readMountInfo()
	candidates := make([]filesystemCandidate, 0, len(mounts))
	seenMountPoints := map[string]bool{}
	rootCoveredByNetworkMount := false

	for _, mount := range mounts {
		if seenMountPoints[mount.mountPoint] || isVirtualFilesystem(mount.fsType) || shouldSkipMountPoint(mount.mountPoint) {
			continue
		}
		if isNetworkFilesystem(mount.fsType) {
			if pathHasPrefix(c.rootDir, mount.mountPoint) {
				rootCoveredByNetworkMount = true
			}
			continue
		}
		seenMountPoints[mount.mountPoint] = true

		usage, ok := c.filesystemUsage(mount.mountPoint, mount.device, mount.fsType)
		if !ok {
			continue
		}
		candidates = append(candidates, filesystemCandidate{
			mount:           mount,
			usage:           usage,
			coversRootDir:   pathHasPrefix(c.rootDir, mount.mountPoint),
			wholeFilesystem: filepath.Clean(mount.root) == string(os.PathSeparator),
		})
	}

	filesystems := dedupeFilesystemCandidates(candidates)

	primaryIndex := -1
	primaryLength := -1
	for index, filesystem := range filesystems {
		if pathHasPrefix(c.rootDir, filesystem.MountPoint) && len(filesystem.MountPoint) > primaryLength {
			primaryIndex = index
			primaryLength = len(filesystem.MountPoint)
		}
	}
	if primaryIndex >= 0 {
		filesystems[primaryIndex].Primary = true
	} else if !rootCoveredByNetworkMount {
		filesystems = append(filesystems, c.rootFilesystemUsage())
	}

	sort.SliceStable(filesystems, func(i, j int) bool {
		if filesystems[i].Primary != filesystems[j].Primary {
			return filesystems[i].Primary
		}
		return filesystems[i].MountPoint < filesystems[j].MountPoint
	})

	return filesystems
}

func dedupeFilesystemCandidates(candidates []filesystemCandidate) []FilesystemUsage {
	selected := []filesystemCandidate{}
	selectedByKey := map[string]int{}
	for _, candidate := range candidates {
		key := filesystemIdentityKey(candidate.mount)
		if index, ok := selectedByKey[key]; ok {
			if betterFilesystemCandidate(candidate, selected[index]) {
				selected[index] = candidate
			}
			continue
		}
		selectedByKey[key] = len(selected)
		selected = append(selected, candidate)
	}

	filesystems := make([]FilesystemUsage, 0, len(selected))
	for _, candidate := range selected {
		filesystems = append(filesystems, candidate.usage)
	}
	return filesystems
}

func filesystemIdentityKey(mount mountInfo) string {
	if mount.majorMinor == "" || mount.device == "" {
		return mount.fsType + "\x00" + mount.mountPoint
	}
	return mount.fsType + "\x00" + mount.majorMinor + "\x00" + mount.device
}

func betterFilesystemCandidate(candidate, current filesystemCandidate) bool {
	if candidate.coversRootDir != current.coversRootDir {
		return candidate.coversRootDir
	}
	if candidate.wholeFilesystem != current.wholeFilesystem {
		return candidate.wholeFilesystem
	}

	candidateMountPoint := filepath.Clean(candidate.mount.mountPoint)
	currentMountPoint := filepath.Clean(current.mount.mountPoint)
	if len(candidateMountPoint) != len(currentMountPoint) {
		return len(candidateMountPoint) < len(currentMountPoint)
	}
	return candidateMountPoint < currentMountPoint
}

func (c *MetricsCollector) readMountInfo() []mountInfo {
	data, err := os.ReadFile(filepath.Join(c.procRoot, "self", "mountinfo"))
	if err != nil {
		return nil
	}

	mounts := []mountInfo{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, " - ")
		if len(parts) != 2 {
			continue
		}
		preFields := strings.Fields(parts[0])
		postFields := strings.Fields(parts[1])
		if len(preFields) < 5 || len(postFields) < 2 {
			continue
		}

		mounts = append(mounts, mountInfo{
			root:       unescapeMountInfo(preFields[3]),
			mountPoint: unescapeMountInfo(preFields[4]),
			majorMinor: preFields[2],
			fsType:     postFields[0],
			device:     unescapeMountInfo(postFields[1]),
		})
	}

	return mounts
}

func (c *MetricsCollector) rootFilesystemUsage() FilesystemUsage {
	usage, ok := c.filesystemUsage(c.rootDir, "", "")
	if !ok {
		return FilesystemUsage{MountPoint: c.rootDir, Primary: true}
	}
	usage.Primary = true
	return usage
}

func (c *MetricsCollector) filesystemUsage(mountPoint, device, fsType string) (FilesystemUsage, bool) {
	stats, err := c.statfsWithTimeout(mountPoint)
	if err != nil {
		if errors.Is(err, errStatfsTimedOut) {
			if cached, ok := c.statfsCache[mountPoint]; ok {
				stats = cached
			} else {
				return FilesystemUsage{}, false
			}
		} else {
			return FilesystemUsage{}, false
		}
	} else {
		c.statfsCache[mountPoint] = stats
	}

	blockSize := statfsBlockSize(stats)
	total := stats.Blocks * blockSize
	free := stats.Bfree * blockSize
	available := stats.Bavail * blockSize
	used := uint64(0)
	if total >= free {
		used = total - free
	}

	usagePercent := 0.0
	if total > 0 {
		usagePercent = roundFloat((float64(used) / float64(total)) * 100)
	}

	return FilesystemUsage{
		MountPoint:     mountPoint,
		Device:         device,
		FSType:         fsType,
		TotalBytes:     total,
		UsedBytes:      used,
		AvailableBytes: available,
		UsagePercent:   usagePercent,
	}, true
}

func (c *MetricsCollector) statfsWithTimeout(mountPoint string) (syscall.Statfs_t, error) {
	if c.statfsTimeout <= 0 {
		var stats syscall.Statfs_t
		err := c.statfs(mountPoint, &stats)
		return stats, err
	}

	type statfsResult struct {
		stats syscall.Statfs_t
		err   error
	}
	results := make(chan statfsResult, 1)
	go func() {
		var stats syscall.Statfs_t
		err := c.statfs(mountPoint, &stats)
		results <- statfsResult{stats: stats, err: err}
	}()

	timer := time.NewTimer(c.statfsTimeout)
	defer stopTimer(timer)

	select {
	case result := <-results:
		return result.stats, result.err
	case <-timer.C:
		return syscall.Statfs_t{}, errStatfsTimedOut
	}
}

func (c *MetricsCollector) readDiskIOUsage(sampledAt time.Time) DiskIOUsage {
	counters := c.readDiskIOCounters()
	names := make([]string, 0, len(counters))
	for name := range counters {
		names = append(names, name)
	}
	sort.Strings(names)

	elapsedSeconds := 0.0
	if !c.lastDiskIOAt.IsZero() {
		elapsedSeconds = sampledAt.Sub(c.lastDiskIOAt).Seconds()
	}

	devices := make([]DiskIODeviceUsage, 0, len(names))
	var totalReadRate float64
	var totalWriteRate float64
	for _, name := range names {
		counter := counters[name]
		readRate := 0.0
		writeRate := 0.0
		if elapsedSeconds > 0 {
			if previous, ok := c.lastDiskIO[name]; ok {
				if counter.readBytes >= previous.readBytes {
					readRate = float64(counter.readBytes-previous.readBytes) / elapsedSeconds
				}
				if counter.writeBytes >= previous.writeBytes {
					writeRate = float64(counter.writeBytes-previous.writeBytes) / elapsedSeconds
				}
			}
		}
		readRate = roundFloat(readRate)
		writeRate = roundFloat(writeRate)
		totalReadRate += readRate
		totalWriteRate += writeRate
		devices = append(devices, DiskIODeviceUsage{
			Name:             name,
			ReadBytes:        counter.readBytes,
			WriteBytes:       counter.writeBytes,
			ReadBytesPerSec:  readRate,
			WriteBytesPerSec: writeRate,
		})
	}

	sort.SliceStable(devices, func(i, j int) bool {
		leftRate := devices[i].ReadBytesPerSec + devices[i].WriteBytesPerSec
		rightRate := devices[j].ReadBytesPerSec + devices[j].WriteBytesPerSec
		if leftRate != rightRate {
			return leftRate > rightRate
		}
		return devices[i].Name < devices[j].Name
	})

	c.lastDiskIO = counters
	c.lastDiskIOAt = sampledAt

	return DiskIOUsage{
		TotalReadBytesPerSec:  roundFloat(totalReadRate),
		TotalWriteBytesPerSec: roundFloat(totalWriteRate),
		Devices:               devices,
	}
}

func (c *MetricsCollector) readDiskIOCounters() map[string]diskIOCounter {
	data, err := os.ReadFile(filepath.Join(c.procRoot, "diskstats"))
	if err != nil {
		return map[string]diskIOCounter{}
	}

	counters := map[string]diskIOCounter{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 10 {
			continue
		}

		name := fields[2]
		if shouldSkipBlockDevice(name) {
			continue
		}

		readSectors, readErr := strconv.ParseUint(fields[5], 10, 64)
		writeSectors, writeErr := strconv.ParseUint(fields[9], 10, 64)
		if readErr != nil || writeErr != nil {
			continue
		}

		counters[name] = diskIOCounter{
			readBytes:  readSectors * 512,
			writeBytes: writeSectors * 512,
		}
	}

	return counters
}

func (c *MetricsCollector) readNetworkUsage(sampledAt time.Time) NetworkUsage {
	counters := c.readNetworkCounters()
	names := make([]string, 0, len(counters))
	for name := range counters {
		names = append(names, name)
	}
	sort.Strings(names)

	elapsedSeconds := 0.0
	if !c.lastNetworkAt.IsZero() {
		elapsedSeconds = sampledAt.Sub(c.lastNetworkAt).Seconds()
	}

	interfaces := make([]NetworkInterfaceUsage, 0, len(names))
	var totalRXRate float64
	var totalTXRate float64
	for _, name := range names {
		counter := counters[name]
		rxRate := 0.0
		txRate := 0.0
		if elapsedSeconds > 0 {
			if previous, ok := c.lastNetwork[name]; ok {
				if counter.rxBytes >= previous.rxBytes {
					rxRate = float64(counter.rxBytes-previous.rxBytes) / elapsedSeconds
				}
				if counter.txBytes >= previous.txBytes {
					txRate = float64(counter.txBytes-previous.txBytes) / elapsedSeconds
				}
			}
		}
		rxRate = roundFloat(rxRate)
		txRate = roundFloat(txRate)
		totalRXRate += rxRate
		totalTXRate += txRate
		interfaces = append(interfaces, NetworkInterfaceUsage{
			Name:          name,
			RXBytes:       counter.rxBytes,
			TXBytes:       counter.txBytes,
			RXBytesPerSec: rxRate,
			TXBytesPerSec: txRate,
		})
	}

	sort.SliceStable(interfaces, func(i, j int) bool {
		leftRate := interfaces[i].RXBytesPerSec + interfaces[i].TXBytesPerSec
		rightRate := interfaces[j].RXBytesPerSec + interfaces[j].TXBytesPerSec
		if leftRate != rightRate {
			return leftRate > rightRate
		}
		return interfaces[i].Name < interfaces[j].Name
	})

	c.lastNetwork = counters
	c.lastNetworkAt = sampledAt

	return NetworkUsage{
		TotalRXBytesPerSec: roundFloat(totalRXRate),
		TotalTXBytesPerSec: roundFloat(totalTXRate),
		PublicIP:           c.cachedPublicIP(sampledAt, c.isActiveAt(sampledAt)),
		Interfaces:         interfaces,
	}
}

func (c *MetricsCollector) isActiveAt(now time.Time) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return now.Before(c.activeUntil)
}

func (c *MetricsCollector) cachedPublicIP(now time.Time, allowRefresh bool) string {
	if c.publicIPLookupEnabled == nil || !c.publicIPLookupEnabled() {
		return ""
	}

	c.publicIPMu.Lock()
	defer c.publicIPMu.Unlock()

	checkedAt := c.publicIPCheckedAt
	if c.publicIP != "" && (!allowRefresh || now.Sub(checkedAt) < publicIPCacheTTL) {
		return c.publicIP
	}
	if !allowRefresh {
		return ""
	}
	if c.publicIP == "" && !checkedAt.IsZero() && now.Sub(checkedAt) < publicIPFailureRetryInterval {
		return ""
	}
	if !c.publicIPRefreshInFlight {
		c.publicIPRefreshInFlight = true
		go c.refreshPublicIP()
	}
	return c.publicIP
}

func (c *MetricsCollector) clearPublicIP() {
	c.publicIPMu.Lock()
	defer c.publicIPMu.Unlock()
	c.publicIP = ""
	c.publicIPCheckedAt = time.Time{}
	c.publicIPRefreshInFlight = false
}

func (c *MetricsCollector) refreshPublicIP() {
	var ip string
	var err error
	defer func() {
		if recovered := recover(); recovered != nil {
			ip = ""
			err = errPublicIPLookupPanic
		}
		now := c.now().UTC()
		c.publicIPMu.Lock()
		defer c.publicIPMu.Unlock()
		if err == nil {
			if normalized, ok := normalizePublicIP(ip); ok {
				c.publicIP = normalized
			}
		}
		c.publicIPCheckedAt = now
		c.publicIPRefreshInFlight = false
	}()

	ctx, cancel := context.WithTimeout(context.Background(), publicIPFetchTimeout)
	defer cancel()

	ip, err = c.publicIPResolver(ctx)
}

func defaultPublicIPResolver(ctx context.Context) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, publicIPLookupURL, nil)
	if err != nil {
		return "", err
	}

	client := &http.Client{
		Timeout: publicIPFetchTimeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", nil
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 256))
	if err != nil {
		return "", err
	}
	if ip, ok := normalizePublicIP(string(body)); ok {
		return ip, nil
	}
	return "", nil
}

func normalizePublicIP(raw string) (string, bool) {
	candidate := strings.TrimSpace(raw)
	fields := strings.Fields(candidate)
	if len(fields) > 0 {
		candidate = fields[0]
	}
	ip := net.ParseIP(candidate)
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return "", false
	}
	return ip.String(), true
}

func (c *MetricsCollector) readNetworkCounters() map[string]networkCounter {
	data, err := os.ReadFile(filepath.Join(c.procRoot, "net", "dev"))
	if err != nil {
		return map[string]networkCounter{}
	}

	counters := map[string]networkCounter{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}

		name := strings.TrimSpace(parts[0])
		if shouldSkipNetworkInterface(name) {
			continue
		}

		fields := strings.Fields(parts[1])
		if len(fields) < 16 {
			continue
		}
		rxBytes, rxErr := strconv.ParseUint(fields[0], 10, 64)
		txBytes, txErr := strconv.ParseUint(fields[8], 10, 64)
		if rxErr != nil || txErr != nil {
			continue
		}

		counters[name] = networkCounter{rxBytes: rxBytes, txBytes: txBytes}
	}

	return counters
}

func isVirtualFilesystem(fsType string) bool {
	switch fsType {
	case "autofs", "bpf", "binfmt_misc", "cgroup", "cgroup2", "configfs", "debugfs", "devpts",
		"devtmpfs", "fusectl", "hugetlbfs", "mqueue", "nsfs", "overlay", "proc", "pstore",
		"ramfs", "rpc_pipefs", "securityfs", "squashfs", "sysfs", "tmpfs", "tracefs":
		return true
	default:
		return false
	}
}

func isNetworkFilesystem(fsType string) bool {
	if strings.HasPrefix(fsType, "fuse.") {
		return true
	}

	switch fsType {
	case "9p", "afs", "cifs", "davfs", "gfs", "gfs2", "glusterfs", "ncpfs", "nfs", "nfs4", "smb3", "smbfs", "sshfs":
		return true
	default:
		return false
	}
}

func shouldSkipMountPoint(mountPoint string) bool {
	clean := filepath.Clean(mountPoint)
	if clean == string(os.PathSeparator) {
		return false
	}
	for _, prefix := range []string{"/proc", "/sys", "/dev", "/run", "/var/lib/docker", "/var/lib/containerd"} {
		if clean == prefix || strings.HasPrefix(clean, prefix+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

func shouldSkipNetworkInterface(name string) bool {
	if name == "" || name == "lo" {
		return true
	}
	for _, prefix := range []string{"br-", "cni", "docker", "flannel", "veth", "virbr"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func shouldSkipBlockDevice(name string) bool {
	if name == "" || strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") {
		return true
	}
	if strings.HasPrefix(name, "zram") {
		return true
	}
	if strings.Contains(name, "/") {
		return true
	}
	return isLikelyBlockPartition(name)
}

func isLikelyBlockPartition(name string) bool {
	if len(name) < 2 {
		return false
	}

	digitStart := len(name)
	for digitStart > 0 && isASCIIDigit(name[digitStart-1]) {
		digitStart--
	}
	if digitStart == len(name) || digitStart == 0 {
		return false
	}

	if strings.Contains(name, "nvme") || strings.Contains(name, "mmcblk") {
		return strings.HasSuffix(name[:digitStart], "p")
	}
	if strings.HasPrefix(name, "md") && allASCIIDigits(name[2:]) {
		return false
	}
	if strings.HasPrefix(name, "dm-") {
		return strings.HasSuffix(name[:digitStart], "p")
	}

	previous := name[digitStart-1]
	return isASCIILetter(previous)
}

func isASCIIDigit(value byte) bool {
	return value >= '0' && value <= '9'
}

func isASCIILetter(value byte) bool {
	return (value >= 'a' && value <= 'z') || (value >= 'A' && value <= 'Z')
}

func allASCIIDigits(value string) bool {
	if value == "" {
		return false
	}
	for index := 0; index < len(value); index++ {
		if !isASCIIDigit(value[index]) {
			return false
		}
	}
	return true
}

func pathHasPrefix(pathValue, prefix string) bool {
	cleanPath := filepath.Clean(pathValue)
	cleanPrefix := filepath.Clean(prefix)
	if cleanPath == cleanPrefix || cleanPrefix == string(os.PathSeparator) {
		return true
	}
	return strings.HasPrefix(cleanPath, cleanPrefix+string(os.PathSeparator))
}

func unescapeMountInfo(value string) string {
	replacer := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	return replacer.Replace(value)
}

func stopTimer(timer *time.Timer) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}
