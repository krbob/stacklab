package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"stacklab/internal/servicemetrics"
)

const testMetricsToken = "test-metrics-test-metrics-test-metrics"

func TestPrometheusMetricsAreOptInAndRequireSeparateBearerToken(t *testing.T) {
	handler, served, _ := newInternalTestHandler(t)
	cookies := loginInternalTestUser(t, served, "test-password")
	requestMetrics := func(authorization string, withSession bool) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		request.Header.Set("Authorization", authorization)
		if withSession {
			for _, cookie := range cookies {
				request.AddCookie(cookie)
			}
		}
		response := httptest.NewRecorder()
		served.ServeHTTP(response, request)
		return response
	}
	if response := requestMetrics("Bearer "+testMetricsToken, true); response.Code != http.StatusNotFound {
		t.Fatalf("disabled metrics status = %d", response.Code)
	}
	tokenPath := filepath.Join(t.TempDir(), "metrics-token")
	if err := os.WriteFile(tokenPath, []byte(testMetricsToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	handler.cfg.MetricsTokenFile = tokenPath
	handler.serviceMetrics = servicemetrics.New(time.Now())
	if err := handler.initializePrometheusMetrics(); err != nil {
		t.Fatal(err)
	}
	probes := 0
	handler.readinessChecks = []ReadinessCheck{{Name: "runtime", Check: func(context.Context) error {
		probes++
		return errors.New("sensitive docker diagnostic")
	}}}
	before := handler.serviceMetrics.Snapshot(time.Now())
	for _, header := range []string{"", "Basic " + testMetricsToken, "Bearer incorrect", "Bearer " + testMetricsToken + "suffix"} {
		response := requestMetrics(header, true)
		if response.Code != http.StatusUnauthorized || response.Header().Get("WWW-Authenticate") != "Bearer" {
			t.Fatalf("unauthorized scrape = %d, %s", response.Code, response.Body.String())
		}
	}
	if probes != 0 {
		t.Fatal("unauthorized scrapes executed readiness checks")
	}
	for range 2 {
		response := requestMetrics("Bearer "+testMetricsToken, false)
		if response.Code != http.StatusOK || !strings.HasPrefix(response.Header().Get("Content-Type"), "text/plain") || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("scrape = %d, %s", response.Code, response.Body.String())
		}
		for _, metric := range []string{"stacklab_ready 0", "stacklab_build_info", "go_goroutines", `stacklab_readiness_check{component="runtime"} 0`} {
			if !strings.Contains(response.Body.String(), metric) {
				t.Errorf("missing metric %q", metric)
			}
		}
		for _, secret := range []string{testMetricsToken, "sensitive docker diagnostic"} {
			if strings.Contains(response.Body.String(), secret) {
				t.Fatal("scrape leaks private data")
			}
		}
	}
	after := handler.serviceMetrics.Snapshot(time.Now())
	if before.HTTP != after.HTTP {
		t.Fatalf("scrapes changed HTTP counters: before=%+v, after=%+v", before.HTTP, after.HTTP)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/service/metrics", nil)
	request.Header.Set("Authorization", "Bearer "+testMetricsToken)
	response := httptest.NewRecorder()
	served.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatal("metrics token granted access to session-authenticated API")
	}
}

func TestPrometheusMetricsRejectMissingOrInvalidConfiguredToken(t *testing.T) {
	handler, _, _ := newInternalTestHandler(t)
	handler.cfg.MetricsTokenFile = filepath.Join(t.TempDir(), "metrics-token")
	if err := handler.initializePrometheusMetrics(); err == nil {
		t.Fatal("missing token file was accepted")
	}
	for _, value := range []string{"", "short", strings.Repeat("x", 257), strings.Repeat("x", 32) + " embedded whitespace"} {
		if err := os.WriteFile(handler.cfg.MetricsTokenFile, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := handler.initializePrometheusMetrics(); err == nil {
			t.Fatal("invalid token file was accepted")
		}
	}
}
