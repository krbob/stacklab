package stacks

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	statsSampleInterval = time.Second
	statsCommandTimeout = 5 * time.Second
	statsMaxAge         = 30 * time.Second
)

// StatsCollector consumes one continuous Docker stats stream and refreshes
// Compose project membership every second. Resource requests only read memory.
type StatsCollector struct {
	logger   *slog.Logger
	interval time.Duration
	run      func(ctx context.Context, name string, args ...string) ([]byte, error)
	stream   func(context.Context, func([]byte)) error

	mu         sync.RWMutex
	projects   map[string]string
	containers map[string]StackStats
}

func NewStatsCollector(logger *slog.Logger) *StatsCollector {
	return &StatsCollector{
		logger:   logger,
		interval: statsSampleInterval,
		run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, name, args...).Output()
		},
		stream:     streamDockerStats,
		projects:   map[string]string{},
		containers: map[string]StackStats{},
	}
}

// Run executes the sampling loop until ctx is cancelled.
func (c *StatsCollector) Run(ctx context.Context) {
	var streams sync.WaitGroup
	streams.Go(func() { c.followStats(ctx) })
	defer streams.Wait()
	c.refreshProjects(ctx)
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.refreshProjects(ctx)
		}
	}
}

// Start launches the sampling loop; it stops when ctx is cancelled.
func (c *StatsCollector) Start(ctx context.Context) {
	go c.Run(ctx)
}

// Snapshot returns fresh per-project aggregates; stale entries are dropped.
func (c *StatsCollector) Snapshot() map[string]StackStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	now := time.Now()
	result := make(map[string]StackStats)
	for id, sample := range c.containers {
		project := c.projects[id]
		if project == "" || now.Sub(sample.SampledAt) > statsMaxAge {
			continue
		}
		total := result[project]
		total.CPUPercent += sample.CPUPercent
		total.MemoryBytes += sample.MemoryBytes
		if total.SampledAt.IsZero() || sample.SampledAt.Before(total.SampledAt) {
			total.SampledAt = sample.SampledAt
		}
		result[project] = total
	}
	return result
}

func (c *StatsCollector) refreshProjects(ctx context.Context) {
	sampleCtx, cancel := context.WithTimeout(ctx, statsCommandTimeout)
	defer cancel()

	projectsOut, err := c.run(sampleCtx, "docker", "ps", "--format", "{{.ID}}\t{{.Label \"com.docker.compose.project\"}}")
	if err != nil {
		c.logger.Debug("stats collector: docker ps failed", slog.String("err", err.Error()))
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.projects = parseContainerProjects(projectsOut)
	for id := range c.containers {
		if c.projects[id] == "" {
			delete(c.containers, id)
		}
	}
}

func (c *StatsCollector) followStats(ctx context.Context) {
	for ctx.Err() == nil {
		if err := c.stream(ctx, c.record); err != nil && ctx.Err() == nil {
			c.logger.Debug("stats collector: docker stats stream failed", slog.String("err", err.Error()))
		}
		timer := time.NewTimer(c.interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (c *StatsCollector) record(line []byte) {
	// Streaming CLI output includes terminal escape sequences around each
	// JSON object, even when stdout is a pipe. Decode only the object.
	start, end := bytes.IndexByte(line, '{'), bytes.LastIndexByte(line, '}')
	if start < 0 || end < start {
		return
	}
	var entry dockerStatsLine
	if json.Unmarshal(line[start:end+1], &entry) != nil || entry.ID == "" {
		return
	}
	c.mu.Lock()
	c.containers[shortContainerID(entry.ID)] = StackStats{
		CPUPercent:  parseCPUPercent(entry.CPUPerc),
		MemoryBytes: parseMemBytes(entry.MemUsage),
		SampledAt:   time.Now().UTC(),
	}
	c.mu.Unlock()
}

func streamDockerStats(ctx context.Context, consume func([]byte)) error {
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(streamCtx, "docker", "stats", "--format", "{{json .}}")
	cmd.WaitDelay = time.Second
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	err = scanDockerStats(stdout, consume)
	if err != nil {
		cancel()
	}
	waitErr := cmd.Wait()
	if err != nil {
		return err
	}
	return waitErr
}

func scanDockerStats(reader io.Reader, consume func([]byte)) error {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		consume(scanner.Bytes())
	}
	return scanner.Err()
}

func shortContainerID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func parseContainerProjects(output []byte) map[string]string {
	result := map[string]string{}
	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		parts := strings.SplitN(strings.TrimSpace(scanner.Text()), "\t", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			continue
		}
		result[shortContainerID(parts[0])] = parts[1]
	}
	return result
}

type dockerStatsLine struct {
	ID       string `json:"ID"`
	CPUPerc  string `json:"CPUPerc"`
	MemUsage string `json:"MemUsage"`
}

func parseCPUPercent(value string) float64 {
	value = strings.TrimSuffix(strings.TrimSpace(value), "%")
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0
	}
	return parsed
}

// parseMemBytes parses the usage half of docker stats MemUsage
// (e.g. "88.55MiB / 7.653GiB") into bytes.
func parseMemBytes(value string) int64 {
	usage, _, _ := strings.Cut(value, "/")
	usage = strings.TrimSpace(usage)
	if usage == "" {
		return 0
	}

	unitStart := len(usage)
	for i, r := range usage {
		if (r < '0' || r > '9') && r != '.' {
			unitStart = i
			break
		}
	}
	number, err := strconv.ParseFloat(usage[:unitStart], 64)
	if err != nil {
		return 0
	}

	multipliers := map[string]float64{
		"B":   1,
		"KIB": 1 << 10, "MIB": 1 << 20, "GIB": 1 << 30, "TIB": 1 << 40,
		"KB": 1e3, "MB": 1e6, "GB": 1e9, "TB": 1e12,
	}
	unit := strings.ToUpper(strings.TrimSpace(usage[unitStart:]))
	multiplier, ok := multipliers[unit]
	if !ok {
		return 0
	}
	return int64(number * multiplier)
}
