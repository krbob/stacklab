package httpapi

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"stacklab/internal/servicemetrics"
	"stacklab/internal/stacks"
)

func TestWebSocketLifecycleUpdatesServiceMetrics(t *testing.T) {
	t.Parallel()

	collector := servicemetrics.New(time.Now())
	handler := &Handler{
		serviceMetrics: collector,
		wsConnections:  map[*wsConnection]struct{}{},
	}
	connection := &wsConnection{onError: func(operation, reason string, _ int) { collector.WebSocketFailure(operation, reason) }}
	if !handler.registerWebSocket(connection) {
		t.Fatal("registerWebSocket() = false")
	}
	connection.markError("read", &websocket.CloseError{Code: websocket.CloseAbnormalClosure})
	connection.markError("write", net.ErrClosed)
	handler.unregisterWebSocket(connection)

	snapshot := collector.Snapshot(time.Now())
	if snapshot.WebSockets.ConnectionsTotal != 1 || snapshot.WebSockets.ConnectionsActive != 0 || snapshot.WebSockets.ErrorsTotal != 1 {
		t.Fatalf("WebSocket metrics = %#v", snapshot.WebSockets)
	}
}

func TestWebSocketFailureClassification(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		reason string
		code   int
	}{
		{"empty browser close", &websocket.CloseError{Code: websocket.CloseNoStatusReceived}, "", 0},
		{"normal", &websocket.CloseError{Code: websocket.CloseNormalClosure}, "", 0},
		{"going away", &websocket.CloseError{Code: websocket.CloseGoingAway}, "", 0},
		{"session revoked", &websocket.CloseError{Code: websocket.ClosePolicyViolation}, "", 0},
		{"close already sent", websocket.ErrCloseSent, "", 0},
		{"abrupt disconnect", &websocket.CloseError{Code: websocket.CloseAbnormalClosure}, "abnormal_close", 1006},
		{"timeout", &net.OpError{Op: "read", Err: os.ErrDeadlineExceeded}, "timeout", 0},
		{"invalid frame", &websocket.CloseError{Code: websocket.CloseProtocolError}, "protocol", 1002},
		{"transport", errors.New("broken pipe"), "transport", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			c := &wsConnection{onError: func(operation, reason string, code int) {
				calls++
				if operation != "read" || reason != tc.reason || code != tc.code {
					t.Errorf("failure = %s/%s/%d", operation, reason, code)
				}
			}}
			c.markError("read", tc.err)
			c.markError("read", tc.err)
			want := 1
			if tc.reason == "" {
				want = 0
			}
			if calls != want {
				t.Fatalf("failure count = %d, want %d", calls, want)
			}
			c.close(websocket.CloseNormalClosure, "done")
			c.markError("write", net.ErrClosed)
			if calls != want {
				t.Fatal("cleanup counted another failure")
			}
		})
	}
}

func TestBrowserEmptyCloseDoesNotCountAsFailure(t *testing.T) {
	handler, served, _ := newInternalTestHandler(t)
	cookies := loginInternalTestUser(t, served, "test-password")
	server := httptest.NewServer(served)
	defer server.Close()
	header := http.Header{}
	request := &http.Request{Header: header}
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/api/ws", header)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var hello wsServerFrame
	if err := conn.ReadJSON(&hello); err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteControl(websocket.CloseMessage, nil, time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	_, _, _ = conn.ReadMessage()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := handler.waitForWebSockets(ctx); err != nil {
		t.Fatal(err)
	}
	metrics := handler.serviceMetrics.Snapshot(time.Now())
	if metrics.WebSockets.ErrorsTotal != 0 || metrics.WebSockets.ConnectionsActive != 0 {
		t.Fatalf("empty browser close metrics = %+v", metrics.WebSockets)
	}
}

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
