package servicemetrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

var httpDurationBounds = [...]float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}
var jobDurationBounds = [...]float64{0.1, 0.5, 1, 5, 10, 30, 60, 120, 300, 600, 1800, 3600}

type metricDefinition struct {
	desc  *prometheus.Desc
	kind  prometheus.ValueType
	value func(Snapshot) float64
}

type prometheusCollector struct {
	collector *Collector
	metrics   []metricDefinition
	build     *prometheus.Desc
	check     *prometheus.Desc
	httpTime  *prometheus.Desc
	jobTime   *prometheus.Desc
}

// NewPrometheusRegistry uses a private registry so application instances and tests
// never share counters. Only bounded service aggregates and runtime data are exposed.
func NewPrometheusRegistry(c *Collector, version, commit string) *prometheus.Registry {
	p := &prometheusCollector{
		collector: c,
		build:     prometheus.NewDesc("stacklab_build_info", "Stacklab build information.", nil, prometheus.Labels{"version": version, "commit": commit}),
		check:     prometheus.NewDesc("stacklab_readiness_check", "Whether a readiness component is healthy (1) or unavailable/unknown (0).", []string{"component"}, nil),
		httpTime:  prometheus.NewDesc("stacklab_http_request_duration_seconds", "Duration of completed HTTP handlers, including closed WebSocket handlers. Scrapes are excluded.", nil, nil),
		jobTime:   prometheus.NewDesc("stacklab_job_duration_seconds", "Duration of completed jobs, including failed and cancelled jobs.", nil, nil),
	}
	add := func(name, help string, kind prometheus.ValueType, value func(Snapshot) float64) {
		p.metrics = append(p.metrics, metricDefinition{prometheus.NewDesc("stacklab_"+name, help, nil, nil), kind, value})
	}
	add("uptime_seconds", "Stacklab process uptime in seconds.", prometheus.GaugeValue, func(s Snapshot) float64 { return float64(s.Process.UptimeSeconds) })
	add("http_requests_total", "Completed HTTP handlers excluding Prometheus scrapes.", prometheus.CounterValue, func(s Snapshot) float64 { return float64(s.HTTP.RequestsTotal) })
	add("http_requests_in_flight", "Active HTTP handlers including WebSockets, excluding Prometheus scrapes.", prometheus.GaugeValue, func(s Snapshot) float64 { return float64(s.HTTP.RequestsInFlight) })
	add("http_errors_total", "HTTP responses with a 5xx status, excluding Prometheus scrapes.", prometheus.CounterValue, func(s Snapshot) float64 { return float64(s.HTTP.ErrorsTotal) })
	add("jobs_started_total", "Jobs whose initial state was persisted.", prometheus.CounterValue, func(s Snapshot) float64 { return float64(s.Jobs.StartedTotal) })
	add("jobs_active", "Currently active jobs.", prometheus.GaugeValue, func(s Snapshot) float64 { return float64(s.Jobs.Active) })
	add("jobs_completed_total", "Jobs whose terminal state was persisted.", prometheus.CounterValue, func(s Snapshot) float64 { return float64(s.Jobs.CompletedTotal) })
	add("jobs_errors_total", "Jobs that failed or timed out.", prometheus.CounterValue, func(s Snapshot) float64 { return float64(s.Jobs.ErrorsTotal) })
	add("websocket_connections_total", "Accepted WebSocket connections.", prometheus.CounterValue, func(s Snapshot) float64 { return float64(s.WebSockets.ConnectionsTotal) })
	add("websocket_connections_active", "Active WebSocket connections.", prometheus.GaugeValue, func(s Snapshot) float64 { return float64(s.WebSockets.ConnectionsActive) })
	add("websocket_errors_total", "Failed WebSocket upgrades and unexpected connection I/O errors.", prometheus.CounterValue, func(s Snapshot) float64 { return float64(s.WebSockets.ErrorsTotal) })
	add("websocket_connection_duration_seconds_total", "Total duration of closed WebSocket connections in seconds.", prometheus.CounterValue, func(s Snapshot) float64 { return s.WebSockets.ConnectionDurationSecondsTotal })
	add("ready", "Whether all readiness checks are healthy (1) or unavailable/unknown (0).", prometheus.GaugeValue, func(s Snapshot) float64 { return healthy(s.Readiness.Status) })
	add("readiness_checked_timestamp_seconds", "Unix timestamp of the last readiness evaluation, or 0 before the first check.", prometheus.GaugeValue, func(s Snapshot) float64 {
		if s.Readiness.CheckedAt == nil {
			return 0
		}
		return float64(s.Readiness.CheckedAt.Unix())
	})
	registry := prometheus.NewRegistry()
	registry.MustRegister(p, collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	return registry
}

func (p *prometheusCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, metric := range p.metrics {
		ch <- metric.desc
	}
	for _, desc := range []*prometheus.Desc{p.build, p.check, p.httpTime, p.jobTime} {
		ch <- desc
	}
}

func (p *prometheusCollector) Collect(ch chan<- prometheus.Metric) {
	c := p.collector
	c.mu.Lock()
	snapshot := c.snapshotLocked(time.Now().UTC())
	httpBuckets := cumulativeBuckets(httpDurationBounds[:], c.httpDurationBuckets[:])
	jobBuckets := cumulativeBuckets(jobDurationBounds[:], c.jobDurationBuckets[:])
	c.mu.Unlock()
	for _, metric := range p.metrics {
		ch <- prometheus.MustNewConstMetric(metric.desc, metric.kind, metric.value(snapshot))
	}
	ch <- prometheus.MustNewConstMetric(p.build, prometheus.GaugeValue, 1)
	for _, component := range []string{"database", "frontend", "runtime"} {
		ch <- prometheus.MustNewConstMetric(p.check, prometheus.GaugeValue, healthy(snapshot.Readiness.Checks[component]), component)
	}
	ch <- prometheus.MustNewConstHistogram(p.httpTime, snapshot.HTTP.RequestsTotal, snapshot.HTTP.DurationSecondsTotal, httpBuckets)
	ch <- prometheus.MustNewConstHistogram(p.jobTime, snapshot.Jobs.CompletedTotal, snapshot.Jobs.DurationSecondsTotal, jobBuckets)
}

func cumulativeBuckets(bounds []float64, counts []uint64) map[float64]uint64 {
	result := make(map[float64]uint64, len(bounds))
	var count uint64
	for i, bound := range bounds {
		count += counts[i]
		result[bound] = count
	}
	return result
}

func healthy(status string) float64 {
	if status == "ok" {
		return 1
	}
	return 0
}
