package servicemetrics

import (
	"testing"
	"time"
)

func TestCollectorTracksBoundedProcessMetrics(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, time.July, 11, 10, 0, 0, 0, time.FixedZone("local", 2*60*60))
	collector := New(startedAt)

	collector.RequestStarted()
	collector.RequestFinished(125*time.Millisecond, 503)
	collector.RequestStarted()
	collector.RequestFinished(25*time.Millisecond, 404)

	jobStartedAt := startedAt.Add(time.Second)
	collector.JobStarted(jobStartedAt)
	collector.JobFinished(jobStartedAt, jobStartedAt.Add(3*time.Second), "failed")

	collector.WebSocketOpened()
	collector.WebSocketError()
	collector.WebSocketClosed(5 * time.Second)

	collector.ReadinessChecked("unavailable", map[string]string{
		"database": "error",
		"frontend": "ok",
	}, startedAt.Add(9*time.Second))

	snapshot := collector.Snapshot(startedAt.Add(10 * time.Second))
	if snapshot.Process.StartedAt.Location() != time.UTC || snapshot.Process.UptimeSeconds != 10 {
		t.Fatalf("process metrics = %#v", snapshot.Process)
	}
	if snapshot.HTTP.RequestsTotal != 2 || snapshot.HTTP.RequestsInFlight != 0 || snapshot.HTTP.ErrorsTotal != 1 {
		t.Fatalf("HTTP metrics = %#v", snapshot.HTTP)
	}
	if snapshot.HTTP.DurationSecondsTotal != 0.15 || snapshot.HTTP.DurationSecondsMax != 0.125 {
		t.Fatalf("HTTP durations = %#v", snapshot.HTTP)
	}
	if snapshot.Jobs.StartedTotal != 1 || snapshot.Jobs.Active != 0 || snapshot.Jobs.CompletedTotal != 1 || snapshot.Jobs.ErrorsTotal != 1 || snapshot.Jobs.DurationSecondsTotal != 3 {
		t.Fatalf("job metrics = %#v", snapshot.Jobs)
	}
	if snapshot.WebSockets.ConnectionsTotal != 1 || snapshot.WebSockets.ConnectionsActive != 0 || snapshot.WebSockets.ErrorsTotal != 1 || snapshot.WebSockets.ConnectionDurationSecondsTotal != 5 {
		t.Fatalf("WebSocket metrics = %#v", snapshot.WebSockets)
	}
	if snapshot.Readiness.Status != "unavailable" || snapshot.Readiness.CheckedAt == nil || snapshot.Readiness.Checks["database"] != "error" {
		t.Fatalf("readiness metrics = %#v", snapshot.Readiness)
	}

	// Snapshots must not expose the collector's internal readiness map.
	snapshot.Readiness.Checks["database"] = "ok"
	if got := collector.Snapshot(startedAt.Add(10 * time.Second)).Readiness.Checks["database"]; got != "error" {
		t.Fatalf("mutating snapshot changed collector state: %q", got)
	}
}

func TestCollectorDoesNotUnderflowActiveGauges(t *testing.T) {
	t.Parallel()

	collector := New(time.Now())
	collector.RequestFinished(time.Second, 200)
	collector.JobFinished(time.Now(), time.Now().Add(time.Second), "cancelled")
	collector.WebSocketClosed(time.Second)

	snapshot := collector.Snapshot(time.Now())
	if snapshot.HTTP.RequestsInFlight != 0 || snapshot.Jobs.Active != 0 || snapshot.WebSockets.ConnectionsActive != 0 {
		t.Fatalf("active gauge underflowed: %#v", snapshot)
	}
}
