package hostinfo

import (
	"bufio"
	"bytes"
	"context"
	"os"
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
)

type MetricsCollector struct {
	procRoot           string
	rootDir            string
	activeInterval     time.Duration
	backgroundInterval time.Duration
	activeTTL          time.Duration
	historyWindow      time.Duration
	maxSamples         int
	now                func() time.Time
	statfs             func(string, *syscall.Statfs_t) error
	wakeCh             chan struct{}

	sampleMu      sync.Mutex
	mu            sync.RWMutex
	samples       []HostMetricSample
	activeUntil   time.Time
	lastCPU       cpuSample
	hasLastCPU    bool
	lastNetwork   map[string]networkCounter
	lastNetworkAt time.Time
}

type networkCounter struct {
	rxBytes uint64
	txBytes uint64
}

type mountInfo struct {
	mountPoint string
	device     string
	fsType     string
}

func newMetricsCollector(rootDir, procRoot string) *MetricsCollector {
	return &MetricsCollector{
		procRoot:           procRoot,
		rootDir:            rootDir,
		activeInterval:     metricsActiveSampleInterval,
		backgroundInterval: metricsBackgroundSampleInterval,
		activeTTL:          metricsActiveTTL,
		historyWindow:      metricsHistoryWindow,
		maxSamples:         metricsMaxSamples,
		now:                time.Now,
		statfs:             syscall.Statfs,
		wakeCh:             make(chan struct{}, 1),
		lastNetwork:        map[string]networkCounter{},
	}
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

func (c *MetricsCollector) Snapshot() MetricsResponse {
	c.markActive()
	if c.shouldSample(c.activeInterval) {
		c.sampleAndStore()
	}

	now := c.now().UTC()
	c.mu.RLock()
	history := append([]HostMetricSample(nil), c.samples...)
	interval := c.currentIntervalLocked(now)
	c.mu.RUnlock()

	var current *HostMetricSample
	if len(history) > 0 {
		currentSample := history[len(history)-1]
		current = &currentSample
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

	c.mu.Lock()
	c.samples = append(c.samples, sample)
	if len(c.samples) > c.maxSamples {
		c.samples = append([]HostMetricSample(nil), c.samples[len(c.samples)-c.maxSamples:]...)
	}
	c.mu.Unlock()
}

func (c *MetricsCollector) readSample() HostMetricSample {
	now := c.now().UTC()
	return HostMetricSample{
		SampledAt:   now,
		CPU:         c.readCPUUsage(),
		Memory:      c.readMemoryUsage(),
		Filesystems: c.readFilesystems(),
		Network:     c.readNetworkUsage(now),
	}
}

func (c *MetricsCollector) readCPUUsage() CPUUsage {
	return CPUUsage{
		CoreCount:    runtime.NumCPU(),
		LoadAverage:  readMetricsLoadAverage(c.procRoot),
		UsagePercent: c.readCPUPercent(),
	}
}

func (c *MetricsCollector) readCPUPercent() float64 {
	sample, ok := readMetricsCPUSample(c.procRoot)
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

func readMetricsLoadAverage(procRoot string) []float64 {
	data, err := os.ReadFile(filepath.Join(procRoot, "loadavg"))
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

func readMetricsCPUSample(procRoot string) (cpuSample, bool) {
	data, err := os.ReadFile(filepath.Join(procRoot, "stat"))
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

		values := make([]uint64, 0, len(fields)-1)
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

func (c *MetricsCollector) readMemoryUsage() MemoryUsage {
	data, err := os.ReadFile(filepath.Join(c.procRoot, "meminfo"))
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

func (c *MetricsCollector) readFilesystems() []FilesystemUsage {
	mounts := c.readMountInfo()
	filesystems := make([]FilesystemUsage, 0, len(mounts))
	seen := map[string]bool{}

	for _, mount := range mounts {
		if seen[mount.mountPoint] || isVirtualFilesystem(mount.fsType) || shouldSkipMountPoint(mount.mountPoint) {
			continue
		}
		seen[mount.mountPoint] = true

		usage, ok := c.filesystemUsage(mount.mountPoint, mount.device, mount.fsType)
		if !ok {
			continue
		}
		filesystems = append(filesystems, usage)
	}

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
	} else {
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
			mountPoint: unescapeMountInfo(preFields[4]),
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
	var stats syscall.Statfs_t
	if err := c.statfs(mountPoint, &stats); err != nil {
		return FilesystemUsage{}, false
	}

	blockSize := uint64(stats.Bsize)
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
		Interfaces:         interfaces,
	}
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
		if name == "" || name == "lo" {
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
