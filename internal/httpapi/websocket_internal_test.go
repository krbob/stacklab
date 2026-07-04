package httpapi

import (
	"testing"
	"time"

	"stacklab/internal/stacks"
)

func TestParseDockerStatsLine(t *testing.T) {
	t.Parallel()

	record, err := parseDockerStatsLine(`{"ID":"abc123","Container":"abc123def456","Name":"demo-app-1","CPUPerc":"12.50%","MemUsage":"256.5MiB / 1.0GiB","NetIO":"12.3kB / 4.5MB"}`)
	if err != nil {
		t.Fatalf("parseDockerStatsLine error = %v", err)
	}
	if record.ID != "abc123def456" {
		t.Fatalf("record.ID = %q, want %q", record.ID, "abc123def456")
	}
	if record.CPU != 12.5 {
		t.Fatalf("record.CPU = %v, want %v", record.CPU, 12.5)
	}
	if record.Memory == 0 || record.MemLimit == 0 || record.NetRX == 0 || record.NetTX == 0 {
		t.Fatalf("expected non-zero parsed usage values, got %#v", record)
	}
}

func TestCalculateNetworkRates(t *testing.T) {
	t.Parallel()

	previous := statsSample{
		rxBytes:   1000,
		txBytes:   2000,
		timestamp: time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC),
	}
	current := dockerStatsRecord{
		NetRX: 4000,
		NetTX: 5000,
	}

	rxRate, txRate := calculateNetworkRates(previous, current, previous.timestamp.Add(2*time.Second))
	if rxRate != 1500 {
		t.Fatalf("rxRate = %v, want %v", rxRate, 1500.0)
	}
	if txRate != 1500 {
		t.Fatalf("txRate = %v, want %v", txRate, 1500.0)
	}
}

func TestAddDockerStatsTotalsUsesMaxMemoryLimit(t *testing.T) {
	t.Parallel()

	totals := map[string]float64{
		"cpu_percent":              0,
		"memory_bytes":             0,
		"memory_limit_bytes":       0,
		"network_rx_bytes_per_sec": 0,
		"network_tx_bytes_per_sec": 0,
	}
	addDockerStatsTotals(totals, dockerStatsRecord{CPU: 1.5, Memory: 100, MemLimit: 4 << 30}, 10, 20)
	addDockerStatsTotals(totals, dockerStatsRecord{CPU: 0.5, Memory: 200, MemLimit: 4 << 30}, 30, 40)

	if totals["cpu_percent"] != 2 {
		t.Fatalf("cpu total = %v, want 2", totals["cpu_percent"])
	}
	if totals["memory_bytes"] != 300 {
		t.Fatalf("memory total = %v, want 300", totals["memory_bytes"])
	}
	if totals["memory_limit_bytes"] != float64(4<<30) {
		t.Fatalf("memory limit = %v, want %v", totals["memory_limit_bytes"], float64(4<<30))
	}
	if totals["network_rx_bytes_per_sec"] != 40 || totals["network_tx_bytes_per_sec"] != 60 {
		t.Fatalf("network totals = rx %v tx %v, want rx 40 tx 60", totals["network_rx_bytes_per_sec"], totals["network_tx_bytes_per_sec"])
	}
}

func TestParseTimestampedLogLine(t *testing.T) {
	t.Parallel()

	timestamp, line := parseTimestampedLogLine("2026-04-03T18:42:01.123456789Z container is ready")
	if line != "container is ready" {
		t.Fatalf("line = %q, want %q", line, "container is ready")
	}
	if timestamp.Format(time.RFC3339Nano) != "2026-04-03T18:42:01.123456789Z" {
		t.Fatalf("timestamp = %s", timestamp.Format(time.RFC3339Nano))
	}
}

func TestFilterContainersByService(t *testing.T) {
	t.Parallel()

	containers := []struct {
		id      string
		service string
	}{
		{id: "a", service: "app"},
		{id: "b", service: "db"},
		{id: "c", service: "app"},
	}

	filtered := filterContainersByService([]stacks.Container{
		{ID: containers[0].id, ServiceName: containers[0].service},
		{ID: containers[1].id, ServiceName: containers[1].service},
		{ID: containers[2].id, ServiceName: containers[2].service},
	}, []string{"app"})
	if len(filtered) != 2 {
		t.Fatalf("len(filtered) = %d, want %d", len(filtered), 2)
	}
	for _, container := range filtered {
		if container.ServiceName != "app" {
			t.Fatalf("unexpected filtered container %#v", container)
		}
	}
}

func TestResolveLogTail(t *testing.T) {
	t.Parallel()

	if got := resolveLogTail(nil); got != 200 {
		t.Fatalf("resolveLogTail(nil) = %d, want %d", got, 200)
	}

	zero := 0
	if got := resolveLogTail(&zero); got != 0 {
		t.Fatalf("resolveLogTail(&0) = %d, want %d", got, 0)
	}

	negative := -5
	if got := resolveLogTail(&negative); got != 0 {
		t.Fatalf("resolveLogTail(&-5) = %d, want %d", got, 0)
	}

	five := 5
	if got := resolveLogTail(&five); got != 5 {
		t.Fatalf("resolveLogTail(&5) = %d, want %d", got, 5)
	}
}
