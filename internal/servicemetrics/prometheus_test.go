package servicemetrics

import (
	"sync"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
)

func TestPrometheusExportsExistingActivityAndBoundedHistograms(t *testing.T) {
	c := New(time.Now().Add(-time.Minute))
	c.RequestStarted()
	c.RequestFinished(10*time.Millisecond, 200)
	c.RequestStarted()
	c.RequestFinished(2*time.Second, 503)
	c.RequestStarted()
	c.JobStarted(time.Now())
	c.JobFinished(time.Unix(0, 0), time.Unix(30, 0), "failed")
	c.WebSocketOpened()
	c.WebSocketError()
	c.ReadinessChecked("unavailable", map[string]string{"database": "ok", "frontend": "ok", "runtime": "error", "unbounded-secret-path": "error"}, time.Now())
	registry := NewPrometheusRegistry(c, "test-version", "test-commit")
	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	byName := make(map[string]*dto.MetricFamily)
	for _, family := range families {
		byName[family.GetName()] = family
		for _, metric := range family.Metric {
			for _, label := range metric.Label {
				if label.GetValue() == "unbounded-secret-path" {
					t.Fatal("unbounded readiness label escaped into metrics")
				}
			}
		}
	}
	for name, want := range map[string]float64{
		"stacklab_http_requests_total": 2, "stacklab_http_errors_total": 1,
		"stacklab_jobs_started_total": 1, "stacklab_jobs_completed_total": 1,
		"stacklab_jobs_errors_total": 1, "stacklab_websocket_connections_total": 1,
		"stacklab_websocket_errors_total": 1,
	} {
		if family := byName[name]; family == nil || len(family.Metric) != 1 || family.Metric[0].GetCounter().GetValue() != want {
			t.Errorf("%s = %v, want %v", name, family, want)
		}
	}
	for name, want := range map[string]float64{"stacklab_ready": 0, "stacklab_http_requests_in_flight": 1, "stacklab_jobs_active": 0, "stacklab_websocket_connections_active": 1} {
		if family := byName[name]; family == nil || len(family.Metric) != 1 || family.Metric[0].GetGauge().GetValue() != want {
			t.Errorf("%s = %v, want %v", name, family, want)
		}
	}
	checks := byName["stacklab_readiness_check"].Metric
	if len(checks) != 3 {
		t.Fatalf("readiness check count = %d, want 3", len(checks))
	}
	histogram := byName["stacklab_http_request_duration_seconds"].Metric[0].GetHistogram()
	if histogram.GetSampleCount() != 2 || histogram.GetSampleSum() != 2.01 {
		t.Fatalf("unexpected HTTP histogram: %v", histogram)
	}
	for _, bucket := range histogram.Bucket {
		var want uint64
		if bucket.GetUpperBound() >= 0.01 {
			want = 1
		}
		if bucket.GetUpperBound() >= 2 {
			want = 2
		}
		if bucket.GetCumulativeCount() != want {
			t.Errorf("bucket %v = %v, want %v", bucket.GetUpperBound(), bucket.GetCumulativeCount(), want)
		}
	}
	if histogram := byName["stacklab_job_duration_seconds"].Metric[0].GetHistogram(); histogram.GetSampleCount() != 1 || histogram.GetSampleSum() != 30 {
		t.Fatalf("unexpected job histogram: %v", histogram)
	}
	if byName["go_goroutines"] == nil || byName["stacklab_build_info"] == nil {
		t.Fatal("runtime or build metrics missing")
	}
}

func TestPrometheusRegistryIsolationAndConcurrentScrapes(t *testing.T) {
	c := New(time.Now())
	registry := NewPrometheusRegistry(c, "test", "one")
	other := NewPrometheusRegistry(New(time.Now()), "test", "two")
	var workers sync.WaitGroup
	for range 4 {
		workers.Go(func() {
			for range 100 {
				c.RequestStarted()
				c.RequestFinished(time.Millisecond, 200)
				c.Snapshot(time.Now())
			}
		})
	}
	for range 10 {
		families, err := registry.Gather()
		if err != nil {
			t.Fatal(err)
		}
		for _, family := range families {
			if family.GetName() == "stacklab_http_request_duration_seconds" {
				histogram := family.Metric[0].GetHistogram()
				if histogram.Bucket[len(histogram.Bucket)-1].GetCumulativeCount() != histogram.GetSampleCount() {
					t.Fatal("histogram bucket/count snapshot is not consistent")
				}
			}
		}
	}
	workers.Wait()
	families, err := other.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range families {
		if family.GetName() == "stacklab_http_requests_total" && family.Metric[0].GetCounter().GetValue() != 0 {
			t.Fatal("registries share activity counters")
		}
	}
}
