package servicemetrics

import (
	"sync"
	"time"
)

// Collector keeps a bounded, process-local aggregate of Stacklab service
// activity. It intentionally does not retain request paths, job IDs, stack IDs,
// or any other unbounded labels.
type Collector struct {
	mu        sync.Mutex
	startedAt time.Time

	http       HTTPMetrics
	jobs       JobMetrics
	webSockets WebSocketMetrics
	readiness  ReadinessMetrics
}

type Snapshot struct {
	CollectedAt time.Time        `json:"collected_at"`
	Process     ProcessMetrics   `json:"process"`
	HTTP        HTTPMetrics      `json:"http"`
	Jobs        JobMetrics       `json:"jobs"`
	WebSockets  WebSocketMetrics `json:"websockets"`
	Readiness   ReadinessMetrics `json:"readiness"`
}

type ProcessMetrics struct {
	StartedAt     time.Time `json:"started_at"`
	UptimeSeconds int64     `json:"uptime_seconds"`
}

type HTTPMetrics struct {
	RequestsTotal        uint64  `json:"requests_total"`
	RequestsInFlight     uint64  `json:"requests_in_flight"`
	ErrorsTotal          uint64  `json:"errors_total"`
	DurationSecondsTotal float64 `json:"duration_seconds_total"`
	DurationSecondsMax   float64 `json:"duration_seconds_max"`
}

type JobMetrics struct {
	StartedTotal         uint64  `json:"started_total"`
	Active               uint64  `json:"active"`
	CompletedTotal       uint64  `json:"completed_total"`
	ErrorsTotal          uint64  `json:"errors_total"`
	DurationSecondsTotal float64 `json:"duration_seconds_total"`
	DurationSecondsMax   float64 `json:"duration_seconds_max"`
}

type WebSocketMetrics struct {
	ConnectionsTotal               uint64  `json:"connections_total"`
	ConnectionsActive              uint64  `json:"connections_active"`
	ErrorsTotal                    uint64  `json:"errors_total"`
	ConnectionDurationSecondsTotal float64 `json:"connection_duration_seconds_total"`
	ConnectionDurationSecondsMax   float64 `json:"connection_duration_seconds_max"`
}

type ReadinessMetrics struct {
	Status    string            `json:"status"`
	CheckedAt *time.Time        `json:"checked_at"`
	Checks    map[string]string `json:"checks"`
}

func New(startedAt time.Time) *Collector {
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	return &Collector{
		startedAt: startedAt.UTC(),
		readiness: ReadinessMetrics{
			Status: "unknown",
			Checks: map[string]string{},
		},
	}
}

func (c *Collector) RequestStarted() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.http.RequestsInFlight++
	c.mu.Unlock()
}

func (c *Collector) RequestFinished(duration time.Duration, status int) {
	if c == nil {
		return
	}
	seconds := durationSeconds(duration)
	c.mu.Lock()
	if c.http.RequestsInFlight > 0 {
		c.http.RequestsInFlight--
	}
	c.http.RequestsTotal++
	if status >= 500 {
		c.http.ErrorsTotal++
	}
	c.http.DurationSecondsTotal += seconds
	c.http.DurationSecondsMax = max(c.http.DurationSecondsMax, seconds)
	c.mu.Unlock()
}

func (c *Collector) JobStarted(time.Time) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.jobs.StartedTotal++
	c.jobs.Active++
	c.mu.Unlock()
}

func (c *Collector) JobFinished(startedAt, finishedAt time.Time, state string) {
	if c == nil {
		return
	}
	seconds := durationSeconds(finishedAt.Sub(startedAt))
	c.mu.Lock()
	if c.jobs.Active > 0 {
		c.jobs.Active--
	}
	c.jobs.CompletedTotal++
	if state == "failed" || state == "timed_out" {
		c.jobs.ErrorsTotal++
	}
	c.jobs.DurationSecondsTotal += seconds
	c.jobs.DurationSecondsMax = max(c.jobs.DurationSecondsMax, seconds)
	c.mu.Unlock()
}

func (c *Collector) WebSocketOpened() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.webSockets.ConnectionsTotal++
	c.webSockets.ConnectionsActive++
	c.mu.Unlock()
}

func (c *Collector) WebSocketClosed(duration time.Duration) {
	if c == nil {
		return
	}
	seconds := durationSeconds(duration)
	c.mu.Lock()
	if c.webSockets.ConnectionsActive > 0 {
		c.webSockets.ConnectionsActive--
	}
	c.webSockets.ConnectionDurationSecondsTotal += seconds
	c.webSockets.ConnectionDurationSecondsMax = max(c.webSockets.ConnectionDurationSecondsMax, seconds)
	c.mu.Unlock()
}

func (c *Collector) WebSocketError() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.webSockets.ErrorsTotal++
	c.mu.Unlock()
}

func (c *Collector) ReadinessChecked(status string, checks map[string]string, checkedAt time.Time) {
	if c == nil {
		return
	}
	checkedAt = checkedAt.UTC()
	clonedChecks := make(map[string]string, len(checks))
	for name, checkStatus := range checks {
		clonedChecks[name] = checkStatus
	}
	c.mu.Lock()
	c.readiness = ReadinessMetrics{
		Status:    status,
		CheckedAt: &checkedAt,
		Checks:    clonedChecks,
	}
	c.mu.Unlock()
}

func (c *Collector) Snapshot(now time.Time) Snapshot {
	if c == nil {
		return Snapshot{}
	}
	now = now.UTC()
	c.mu.Lock()
	defer c.mu.Unlock()

	readiness := c.readiness
	if readiness.CheckedAt != nil {
		checkedAt := *readiness.CheckedAt
		readiness.CheckedAt = &checkedAt
	}
	readiness.Checks = make(map[string]string, len(c.readiness.Checks))
	for name, status := range c.readiness.Checks {
		readiness.Checks[name] = status
	}
	uptime := now.Sub(c.startedAt)
	if uptime < 0 {
		uptime = 0
	}
	return Snapshot{
		CollectedAt: now,
		Process: ProcessMetrics{
			StartedAt:     c.startedAt,
			UptimeSeconds: int64(uptime / time.Second),
		},
		HTTP:       c.http,
		Jobs:       c.jobs,
		WebSockets: c.webSockets,
		Readiness:  readiness,
	}
}

func durationSeconds(duration time.Duration) float64 {
	if duration < 0 {
		return 0
	}
	return duration.Seconds()
}
