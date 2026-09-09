package stacks

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestDockerStatsCommandStreamsUntilCancelled(t *testing.T) {
	binDir := t.TempDir()
	script := "#!/bin/sh\n" +
		"[ \"$1\" = stats ] && [ \"$2\" = --format ] && [ \"$3\" = '{{json .}}' ] && [ \"$#\" = 3 ] || exit 2\n" +
		"printf '%s\\n' '{\"ID\":\"abc123456789\",\"CPUPerc\":\"3%\",\"MemUsage\":\"32MiB / 1GiB\"}'\n" +
		"exec sleep 60\n"
	if err := os.WriteFile(filepath.Join(binDir, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c := NewStatsCollector(slog.Default())
	c.projects = map[string]string{"abc123456789": "demo"}
	err := streamDockerStats(ctx, func(line []byte) {
		c.record(line)
		cancel()
	})
	if err == nil || !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("stream did not stop on cancellation: %v, context: %v", err, ctx.Err())
	}
	if sample := c.Snapshot()["demo"]; sample.CPUPercent != 3 || sample.MemoryBytes != 32<<20 {
		t.Fatalf("stream did not publish before exit: %+v", sample)
	}
}

func TestStatsSnapshotDoesNotScanStackDefinitions(t *testing.T) {
	reader := &ServiceReader{}
	if result := reader.StatsSnapshot(); result.Items == nil || len(result.Items) != 0 {
		t.Fatalf("collector not attached: %+v", result)
	}
	collector := NewStatsCollector(slog.Default())
	collector.projects = map[string]string{"abc123456789": "demo"}
	collector.record([]byte("{\"ID\":\"abc123456789\",\"CPUPerc\":\"7%\",\"MemUsage\":\"64MiB / 1GiB\"}"))
	reader.AttachStatsCollector(collector)
	if got := reader.StatsSnapshot().Items["demo"]; got.CPUPercent != 7 || got.MemoryBytes != 64<<20 {
		t.Fatalf("cached stats = %+v", got)
	}
}

func TestCollectorRemovesStoppedContainersAndKeepsSamplesOnDiscoveryFailure(t *testing.T) {
	c := NewStatsCollector(slog.Default())
	c.projects = map[string]string{"abc123456789": "demo", "stopped": "demo"}
	c.record([]byte("{\"ID\":\"abc123456789\",\"CPUPerc\":\"2%\",\"MemUsage\":\"64MiB / 1GiB\"}"))
	c.record([]byte("{\"ID\":\"stopped\",\"CPUPerc\":\"90%\",\"MemUsage\":\"512MiB / 1GiB\"}"))
	c.run = func(context.Context, string, ...string) ([]byte, error) {
		return []byte("abc123456789\tdemo\n"), nil
	}
	c.refreshProjects(context.Background())
	demo := c.Snapshot()["demo"]
	if demo.CPUPercent != 2 || demo.MemoryBytes != 64<<20 {
		t.Fatalf("snapshot = %+v", demo)
	}
	c.run = func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("Docker unavailable")
	}
	c.refreshProjects(context.Background())
	if c.Snapshot()["demo"] != demo {
		t.Fatal("failed discovery discarded last sample")
	}
	c.run = func(context.Context, string, ...string) ([]byte, error) { return nil, nil }
	c.refreshProjects(context.Background())
	if len(c.Snapshot()) != 0 || len(c.containers) != 0 {
		t.Fatal("empty inventory retained samples")
	}
}

func TestCollectorStreamReplacesSamplesAndHandlesTerminalEscapes(t *testing.T) {
	c := NewStatsCollector(slog.Default())
	c.projects = map[string]string{"abc123456789": "demo"}
	output := "\x1b[H{\"ID\":\"abc123456789full\",\"CPUPerc\":\"2%\",\"MemUsage\":\"64MiB / 1GiB\"}\x1b[K\n" +
		"\x1b[J\x1b[H{\"ID\":\"abc123456789\",\"CPUPerc\":\"5%\",\"MemUsage\":\"128MiB / 1GiB\"}\x1b[K\n" +
		"\x1b[J\ninvalid\n{}\n{broken}\n"
	if err := scanDockerStats(strings.NewReader(output), c.record); err != nil {
		t.Fatal(err)
	}
	if got := c.Snapshot()["demo"]; got.CPUPercent != 5 || got.MemoryBytes != 128<<20 {
		t.Fatalf("latest snapshot = %+v", got)
	}
}

func TestCollectorRefreshesEverySecondAndReconnectsStream(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		c := NewStatsCollector(slog.Default())
		var discoveries, streams atomic.Int32
		c.run = func(context.Context, string, ...string) ([]byte, error) {
			discoveries.Add(1)
			return []byte("abc123456789\tdemo\n"), nil
		}
		c.stream = func(ctx context.Context, consume func([]byte)) error {
			if streams.Add(1) == 1 {
				return errors.New("connection lost")
			}
			consume([]byte("{\"ID\":\"abc123456789\",\"CPUPerc\":\"7%\",\"MemUsage\":\"64MiB / 1GiB\"}"))
			<-ctx.Done()
			return ctx.Err()
		}
		done := make(chan struct{})
		go func() { c.Run(ctx); close(done) }()
		synctest.Wait()
		if discoveries.Load() != 1 || streams.Load() != 1 {
			t.Fatalf("initial discoveries/streams = %d/%d", discoveries.Load(), streams.Load())
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if discoveries.Load() != 2 || streams.Load() != 2 || c.Snapshot()["demo"].CPUPercent != 7 {
			t.Fatalf("after 1s: discoveries/streams = %d/%d, snapshot = %+v", discoveries.Load(), streams.Load(), c.Snapshot())
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if discoveries.Load() != 3 || streams.Load() != 2 {
			t.Fatalf("healthy stream restarted: discoveries/streams = %d/%d", discoveries.Load(), streams.Load())
		}
		cancel()
		synctest.Wait()
		select {
		case <-done:
		default:
			t.Fatal("collector did not shut down")
		}
	})
}

func TestParseMemBytes(t *testing.T) {
	mib := float64(1 << 20)
	gib := float64(1 << 30)
	cases := map[string]int64{
		"88.55MiB / 7.653GiB": int64(88.55 * mib),
		"1.2GiB / 7.6GiB":     int64(1.2 * gib),
		"512KiB / 1GiB":       512 << 10,
		"100B / 1GiB":         100,
		"1.5MB / 2GB":         1500000,
		"garbage":             0,
		"":                    0,
	}
	for in, want := range cases {
		if got := parseMemBytes(in); got != want {
			t.Errorf("parseMemBytes(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestParseCPUPercent(t *testing.T) {
	if got := parseCPUPercent("0.07%"); got != 0.07 {
		t.Fatalf("parseCPUPercent = %v", got)
	}
	if got := parseCPUPercent("bogus"); got != 0 {
		t.Fatalf("parseCPUPercent bogus = %v", got)
	}
}

func TestAggregateStats(t *testing.T) {
	projects := parseContainerProjects([]byte("abc123456789\tjellyfin\ndef123456789\tfusznik\nghi123456789\tfusznik\n"))
	statsOut := []byte(`
{"ID":"abc123456789","CPUPerc":"1.50%","MemUsage":"100MiB / 8GiB"}
{"ID":"def123456789","CPUPerc":"0.25%","MemUsage":"200MiB / 8GiB"}
{"ID":"ghi123456789","CPUPerc":"0.25%","MemUsage":"50MiB / 8GiB"}
{"ID":"zzz","CPUPerc":"9.99%","MemUsage":"1GiB / 8GiB"}
`)
	c := NewStatsCollector(slog.Default())
	c.projects = projects
	if err := scanDockerStats(strings.NewReader(string(statsOut)), c.record); err != nil {
		t.Fatal(err)
	}
	result := c.Snapshot()

	if len(result) != 2 {
		t.Fatalf("len(result) = %d, want 2 (unknown container dropped)", len(result))
	}
	fusznik := result["fusznik"]
	if fusznik.CPUPercent != 0.5 {
		t.Fatalf("fusznik cpu = %v, want 0.5", fusznik.CPUPercent)
	}
	if fusznik.MemoryBytes != 250<<20 {
		t.Fatalf("fusznik mem = %d, want %d", fusznik.MemoryBytes, 250<<20)
	}
	if fusznik.SampledAt.IsZero() {
		t.Fatalf("sampledAt = %v", fusznik.SampledAt)
	}
}

func TestCollectorSnapshotDropsStale(t *testing.T) {
	c := NewStatsCollector(slog.Default())
	c.projects = map[string]string{"fresh": "fresh", "stale": "stale"}
	c.containers = map[string]StackStats{
		"fresh": {CPUPercent: 1, SampledAt: time.Now()},
		"stale": {CPUPercent: 2, SampledAt: time.Now().Add(-time.Minute)},
	}
	snap := c.Snapshot()
	if _, ok := snap["fresh"]; !ok {
		t.Fatal("fresh sample missing")
	}
	if _, ok := snap["stale"]; ok {
		t.Fatal("stale sample not dropped")
	}
}

func TestCollectorRefreshesProjectsWithoutStartingAnotherStatsCommand(t *testing.T) {
	calls := [][]string{}
	c := NewStatsCollector(slog.Default())
	c.run = func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, append([]string{name}, args...))
		if args[0] == "ps" {
			return []byte("abc123456789\tdemo\n"), nil
		}
		return []byte(`{"ID":"abc123456789","CPUPerc":"2.00%","MemUsage":"64MiB / 1GiB"}`), nil
	}

	c.refreshProjects(context.Background())
	c.record([]byte("{\"ID\":\"abc123456789\",\"CPUPerc\":\"2.00%\",\"MemUsage\":\"64MiB / 1GiB\"}"))

	if len(calls) != 1 || calls[0][1] != "ps" {
		t.Fatalf("runner calls = %v, want only docker ps", calls)
	}
	snap := c.Snapshot()
	demo, ok := snap["demo"]
	if !ok || demo.CPUPercent != 2 || demo.MemoryBytes != 64<<20 {
		t.Fatalf("snapshot = %+v", snap)
	}
}
