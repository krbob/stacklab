package stacks

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

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
	now := time.Now().UTC()
	result := aggregateStats(statsOut, projects, now)

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
	if !fusznik.SampledAt.Equal(now) {
		t.Fatalf("sampledAt = %v", fusznik.SampledAt)
	}
}

func TestCollectorSnapshotDropsStale(t *testing.T) {
	c := NewStatsCollector(slog.Default())
	c.store(map[string]StackStats{
		"fresh": {CPUPercent: 1, SampledAt: time.Now()},
		"stale": {CPUPercent: 2, SampledAt: time.Now().Add(-time.Minute)},
	})
	snap := c.Snapshot()
	if _, ok := snap["fresh"]; !ok {
		t.Fatal("fresh sample missing")
	}
	if _, ok := snap["stale"]; ok {
		t.Fatal("stale sample not dropped")
	}
}

func TestCollectorSampleUsesRunner(t *testing.T) {
	calls := [][]string{}
	c := NewStatsCollector(slog.Default())
	c.run = func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, append([]string{name}, args...))
		if args[0] == "ps" {
			return []byte("abc123456789\tdemo\n"), nil
		}
		return []byte(`{"ID":"abc123456789","CPUPerc":"2.00%","MemUsage":"64MiB / 1GiB"}`), nil
	}

	c.sample(context.Background())

	if len(calls) != 2 {
		t.Fatalf("runner calls = %d, want 2", len(calls))
	}
	snap := c.Snapshot()
	demo, ok := snap["demo"]
	if !ok || demo.CPUPercent != 2 || demo.MemoryBytes != 64<<20 {
		t.Fatalf("snapshot = %+v", snap)
	}
}
