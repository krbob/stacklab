package stacks

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	statsSampleInterval = 10 * time.Second
	statsMaxAge         = 30 * time.Second
)

// StatsCollector samples Docker container resource usage on a fixed interval
// and aggregates it per Compose project. List requests read the cached
// snapshot and never touch Docker directly (Slice A1 of the dashboard
// read-model contract).
type StatsCollector struct {
	logger   *slog.Logger
	interval time.Duration
	run      func(ctx context.Context, name string, args ...string) ([]byte, error)

	mu      sync.RWMutex
	samples map[string]StackStats
}

func NewStatsCollector(logger *slog.Logger) *StatsCollector {
	return &StatsCollector{
		logger:   logger,
		interval: statsSampleInterval,
		run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, name, args...).Output()
		},
		samples: map[string]StackStats{},
	}
}

// Run executes the sampling loop until ctx is cancelled.
func (c *StatsCollector) Run(ctx context.Context) {
	c.sample(ctx)
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.sample(ctx)
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
	result := make(map[string]StackStats, len(c.samples))
	for project, sample := range c.samples {
		if now.Sub(sample.SampledAt) > statsMaxAge {
			continue
		}
		result[project] = sample
	}
	return result
}

func (c *StatsCollector) sample(ctx context.Context) {
	sampleCtx, cancel := context.WithTimeout(ctx, c.interval)
	defer cancel()

	projectsOut, err := c.run(sampleCtx, "docker", "ps", "--format", "{{.ID}}\t{{.Label \"com.docker.compose.project\"}}")
	if err != nil {
		c.logger.Debug("stats collector: docker ps failed", slog.String("err", err.Error()))
		return
	}
	projectByID := parseContainerProjects(projectsOut)
	if len(projectByID) == 0 {
		c.store(map[string]StackStats{})
		return
	}

	statsOut, err := c.run(sampleCtx, "docker", "stats", "--no-stream", "--format", "{{json .}}")
	if err != nil {
		c.logger.Debug("stats collector: docker stats failed", slog.String("err", err.Error()))
		return
	}

	c.store(aggregateStats(statsOut, projectByID, time.Now().UTC()))
}

func (c *StatsCollector) store(samples map[string]StackStats) {
	c.mu.Lock()
	c.samples = samples
	c.mu.Unlock()
}

func parseContainerProjects(output []byte) map[string]string {
	result := map[string]string{}
	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		parts := strings.SplitN(strings.TrimSpace(scanner.Text()), "\t", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			continue
		}
		result[parts[0]] = parts[1]
	}
	return result
}

type dockerStatsLine struct {
	ID       string `json:"ID"`
	CPUPerc  string `json:"CPUPerc"`
	MemUsage string `json:"MemUsage"`
}

func aggregateStats(output []byte, projectByID map[string]string, sampledAt time.Time) map[string]StackStats {
	result := map[string]StackStats{}
	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry dockerStatsLine
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		// docker stats truncates IDs to 12 chars; docker ps does the same by
		// default, but match on prefix to be safe against full IDs.
		project := ""
		for id, p := range projectByID {
			if strings.HasPrefix(id, entry.ID) || strings.HasPrefix(entry.ID, id) {
				project = p
				break
			}
		}
		if project == "" {
			continue
		}

		sample := result[project]
		sample.CPUPercent += parseCPUPercent(entry.CPUPerc)
		sample.MemoryBytes += parseMemBytes(entry.MemUsage)
		sample.SampledAt = sampledAt
		result[project] = sample
	}
	return result
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
