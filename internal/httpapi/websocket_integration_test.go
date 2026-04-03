//go:build integration

package httpapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

type integrationWSFrame struct {
	Type      string          `json:"type"`
	RequestID string          `json:"request_id"`
	StreamID  string          `json:"stream_id"`
	Payload   json.RawMessage `json:"payload"`
	Error     *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func TestIntegrationWebSocketLogStreamWithDockerShim(t *testing.T) {
	handler, cfg := newTestHandler(t)
	installDockerShim(t)
	writeDemoStackFixture(t, cfg.RootDir)

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	cookies := loginTestUserViaNetwork(t, server.URL)
	wsConn := dialWebSocketWithCookies(t, server.URL, cookies)
	defer wsConn.Close()

	writeWSFrame(t, wsConn, map[string]any{
		"type":       "logs.subscribe",
		"request_id": "req_logs_1",
		"stream_id":  "logs_demo",
		"payload": map[string]any{
			"stack_id":      "demo",
			"service_names": []string{},
			"tail":          5,
			"timestamps":    true,
		},
	})

	ack := readUntilWSFrame(t, wsConn, func(frame integrationWSFrame) bool {
		return frame.Type == "ack" && frame.RequestID == "req_logs_1"
	})
	var ackPayload struct {
		Status string `json:"status"`
	}
	decodeRawPayload(t, ack.Payload, &ackPayload)
	if ackPayload.Status != "subscribed" {
		t.Fatalf("ack payload status = %q, want %q", ackPayload.Status, "subscribed")
	}

	logEvent := readUntilWSFrame(t, wsConn, func(frame integrationWSFrame) bool {
		return frame.Type == "logs.event" && frame.StreamID == "logs_demo"
	})
	var payload struct {
		Entries []struct {
			ServiceName string `json:"service_name"`
			ContainerID string `json:"container_id"`
			Line        string `json:"line"`
		} `json:"entries"`
	}
	decodeRawPayload(t, logEvent.Payload, &payload)
	if len(payload.Entries) == 0 {
		t.Fatalf("expected at least one log entry")
	}
	if payload.Entries[0].ServiceName != "app" {
		t.Fatalf("service_name = %q, want %q", payload.Entries[0].ServiceName, "app")
	}
	if payload.Entries[0].ContainerID != "container123" {
		t.Fatalf("container_id = %q, want %q", payload.Entries[0].ContainerID, "container123")
	}
	if payload.Entries[0].Line != "shim log line 1" {
		t.Fatalf("line = %q, want %q", payload.Entries[0].Line, "shim log line 1")
	}
}

func TestIntegrationWebSocketStatsStreamWithDockerShim(t *testing.T) {
	handler, cfg := newTestHandler(t)
	installDockerShim(t)
	writeDemoStackFixture(t, cfg.RootDir)

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	cookies := loginTestUserViaNetwork(t, server.URL)
	wsConn := dialWebSocketWithCookies(t, server.URL, cookies)
	defer wsConn.Close()

	writeWSFrame(t, wsConn, map[string]any{
		"type":       "stats.subscribe",
		"request_id": "req_stats_1",
		"stream_id":  "stats_demo",
		"payload": map[string]any{
			"stack_id": "demo",
		},
	})

	_ = readUntilWSFrame(t, wsConn, func(frame integrationWSFrame) bool {
		return frame.Type == "ack" && frame.RequestID == "req_stats_1"
	})

	statsFrame := readUntilWSFrame(t, wsConn, func(frame integrationWSFrame) bool {
		return frame.Type == "stats.frame" && frame.StreamID == "stats_demo"
	})
	var payload struct {
		StackTotals struct {
			CPUPerc       float64 `json:"cpu_percent"`
			MemoryBytes   uint64  `json:"memory_bytes"`
			MemoryLimit   uint64  `json:"memory_limit_bytes"`
			NetworkRXRate float64 `json:"network_rx_bytes_per_sec"`
			NetworkTXRate float64 `json:"network_tx_bytes_per_sec"`
		} `json:"stack_totals"`
		Containers []struct {
			ContainerID string  `json:"container_id"`
			ServiceName string  `json:"service_name"`
			CPUPerc     float64 `json:"cpu_percent"`
		} `json:"containers"`
	}
	decodeRawPayload(t, statsFrame.Payload, &payload)
	if len(payload.Containers) != 1 {
		t.Fatalf("len(containers) = %d, want %d", len(payload.Containers), 1)
	}
	if payload.Containers[0].ContainerID != "container123" {
		t.Fatalf("container_id = %q, want %q", payload.Containers[0].ContainerID, "container123")
	}
	if payload.Containers[0].ServiceName != "app" {
		t.Fatalf("service_name = %q, want %q", payload.Containers[0].ServiceName, "app")
	}
	if payload.StackTotals.CPUPerc != 12.5 {
		t.Fatalf("cpu_percent = %v, want %v", payload.StackTotals.CPUPerc, 12.5)
	}
	if payload.StackTotals.MemoryBytes == 0 || payload.StackTotals.MemoryLimit == 0 {
		t.Fatalf("expected non-zero memory totals, got %#v", payload.StackTotals)
	}
}

func TestIntegrationWebSocketTerminalOpenAndAttachWithDockerShim(t *testing.T) {
	handler, cfg := newTestHandler(t)
	installDockerShim(t)
	writeDemoStackFixture(t, cfg.RootDir)

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	cookies := loginTestUserViaNetwork(t, server.URL)
	wsConn := dialWebSocketWithCookies(t, server.URL, cookies)

	writeWSFrame(t, wsConn, map[string]any{
		"type":       "terminal.open",
		"request_id": "req_term_open_1",
		"stream_id":  "term_demo",
		"payload": map[string]any{
			"stack_id":     "demo",
			"container_id": "container123",
			"shell":        "/bin/sh",
			"cols":         120,
			"rows":         36,
		},
	})

	opened := readUntilWSFrame(t, wsConn, func(frame integrationWSFrame) bool {
		return frame.Type == "terminal.opened" && frame.RequestID == "req_term_open_1"
	})
	var openedPayload struct {
		SessionID   string `json:"session_id"`
		ContainerID string `json:"container_id"`
		Shell       string `json:"shell"`
	}
	decodeRawPayload(t, opened.Payload, &openedPayload)
	if openedPayload.SessionID == "" {
		t.Fatalf("expected terminal session_id")
	}
	if openedPayload.ContainerID != "container123" {
		t.Fatalf("container_id = %q, want %q", openedPayload.ContainerID, "container123")
	}
	if openedPayload.Shell != "/bin/sh" {
		t.Fatalf("shell = %q, want %q", openedPayload.Shell, "/bin/sh")
	}

	readyOutput := readUntilWSFrame(t, wsConn, func(frame integrationWSFrame) bool {
		if frame.Type != "terminal.output" {
			return false
		}
		var payload struct {
			Data string `json:"data"`
		}
		if err := json.Unmarshal(frame.Payload, &payload); err != nil {
			return false
		}
		return strings.Contains(payload.Data, "shim-shell ready")
	})
	var readyPayload struct {
		Data string `json:"data"`
	}
	decodeRawPayload(t, readyOutput.Payload, &readyPayload)
	if !strings.Contains(readyPayload.Data, "shim-shell ready") {
		t.Fatalf("ready output = %q", readyPayload.Data)
	}

	writeWSFrame(t, wsConn, map[string]any{
		"type":      "terminal.input",
		"stream_id": "term_demo",
		"payload": map[string]any{
			"session_id": openedPayload.SessionID,
			"data":       "hello from test\n",
		},
	})

	echoFrame := readUntilWSFrame(t, wsConn, func(frame integrationWSFrame) bool {
		if frame.Type != "terminal.output" {
			return false
		}
		var payload struct {
			Data string `json:"data"`
		}
		if err := json.Unmarshal(frame.Payload, &payload); err != nil {
			return false
		}
		return strings.Contains(payload.Data, "echo:hello from test")
	})
	var echoPayload struct {
		Data string `json:"data"`
	}
	decodeRawPayload(t, echoFrame.Payload, &echoPayload)
	if !strings.Contains(echoPayload.Data, "echo:hello from test") {
		t.Fatalf("echo output = %q", echoPayload.Data)
	}

	if err := wsConn.Close(); err != nil {
		t.Fatalf("wsConn.Close() error = %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	wsConn2 := dialWebSocketWithCookies(t, server.URL, cookies)
	defer wsConn2.Close()

	writeWSFrame(t, wsConn2, map[string]any{
		"type":       "terminal.attach",
		"request_id": "req_term_attach_1",
		"stream_id":  "term_demo_attach",
		"payload": map[string]any{
			"session_id": openedPayload.SessionID,
			"cols":       120,
			"rows":       36,
		},
	})

	attached := readUntilWSFrame(t, wsConn2, func(frame integrationWSFrame) bool {
		return frame.Type == "terminal.opened" && frame.RequestID == "req_term_attach_1"
	})
	var attachedPayload struct {
		SessionID string `json:"session_id"`
	}
	decodeRawPayload(t, attached.Payload, &attachedPayload)
	if attachedPayload.SessionID != openedPayload.SessionID {
		t.Fatalf("attached session_id = %q, want %q", attachedPayload.SessionID, openedPayload.SessionID)
	}

	writeWSFrame(t, wsConn2, map[string]any{
		"type":      "terminal.input",
		"stream_id": "term_demo_attach",
		"payload": map[string]any{
			"session_id": openedPayload.SessionID,
			"data":       "attached session\n",
		},
	})

	attachedEchoFrame := readUntilWSFrame(t, wsConn2, func(frame integrationWSFrame) bool {
		if frame.Type != "terminal.output" {
			return false
		}
		var payload struct {
			Data string `json:"data"`
		}
		if err := json.Unmarshal(frame.Payload, &payload); err != nil {
			return false
		}
		return strings.Contains(payload.Data, "echo:attached session")
	})
	var attachedEchoPayload struct {
		Data string `json:"data"`
	}
	decodeRawPayload(t, attachedEchoFrame.Payload, &attachedEchoPayload)
	if !strings.Contains(attachedEchoPayload.Data, "echo:attached session") {
		t.Fatalf("attached echo output = %q", attachedEchoPayload.Data)
	}

	writeWSFrame(t, wsConn2, map[string]any{
		"type":       "terminal.close",
		"request_id": "req_term_close_1",
		"stream_id":  "term_demo_attach",
		"payload": map[string]any{
			"session_id": openedPayload.SessionID,
		},
	})

	exited := readUntilWSFrame(t, wsConn2, func(frame integrationWSFrame) bool {
		return frame.Type == "terminal.exited" && frame.StreamID == "term_demo_attach"
	})
	var exitedPayload struct {
		SessionID string `json:"session_id"`
		Reason    string `json:"reason"`
	}
	decodeRawPayload(t, exited.Payload, &exitedPayload)
	if exitedPayload.SessionID != openedPayload.SessionID {
		t.Fatalf("terminal.exited session_id = %q, want %q", exitedPayload.SessionID, openedPayload.SessionID)
	}
	if exitedPayload.Reason != "server_cleanup" {
		t.Fatalf("terminal.exited reason = %q, want %q", exitedPayload.Reason, "server_cleanup")
	}
}

func installDockerShim(t *testing.T) {
	t.Helper()

	shimDir := t.TempDir()
	shimPath := filepath.Join(shimDir, "docker")
	script := `#!/bin/sh
set -eu

cmd="${1:-}"
shift || true

case "$cmd" in
  ps)
    printf 'container123\n'
    ;;
  inspect)
    cat <<'EOF'
[
  {
    "Id":"container123",
    "Name":"/demo-app-1",
    "Image":"sha256:deadbeef",
    "Config":{
      "Image":"nginx:alpine",
      "Labels":{
        "com.docker.compose.project":"demo",
        "com.docker.compose.service":"app"
      }
    },
    "State":{
      "Status":"running",
      "StartedAt":"2026-04-03T12:00:00.000000000Z",
      "Health":{"Status":"healthy"}
    },
    "NetworkSettings":{
      "Ports":{"80/tcp":[{"HostPort":"8080"}]},
      "Networks":{"demo_default":{}}
    }
  }
]
EOF
    ;;
  logs)
    printf '2026-04-03T18:42:01.000000000Z shim log line 1\n'
    printf '2026-04-03T18:42:02.000000000Z shim log line 2\n'
    ;;
  stats)
    cat <<'EOF'
{"ID":"container123","Name":"demo-app-1","CPUPerc":"12.50%","MemUsage":"256.0MiB / 1.0GiB","NetIO":"12.0kB / 4.0kB"}
EOF
    ;;
  exec)
    printf 'shim-shell ready\r\n'
    while IFS= read -r line; do
      printf 'echo:%s\r\n' "$line"
    done
    ;;
  version)
    printf '28.0.0\n'
    ;;
  compose)
    sub="${1:-}"
    shift || true
    case "$sub" in
      version)
        printf 'v2.35.0\n'
        ;;
      *)
        echo "unsupported docker compose subcommand: $sub" >&2
        exit 1
        ;;
    esac
    ;;
  *)
    echo "unsupported docker command: $cmd" >&2
    exit 1
    ;;
esac
`
	if err := os.WriteFile(shimPath, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(docker shim) error = %v", err)
	}

	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func writeDemoStackFixture(t *testing.T, rootDir string) {
	t.Helper()

	stackDir := filepath.Join(rootDir, "stacks", "demo")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(stack fixture) error = %v", err)
	}
	content := "services:\n  app:\n    image: nginx:alpine\n    ports:\n      - \"8080:80\"\n"
	if err := os.WriteFile(filepath.Join(stackDir, "compose.yaml"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(compose fixture) error = %v", err)
	}
}

func loginTestUserViaNetwork(t *testing.T, serverURL string) []*http.Cookie {
	t.Helper()

	client := http.DefaultClient
	loginRequestBody := bytes.NewBufferString(`{"password":"secret"}`)
	loginRequest, err := http.NewRequest(http.MethodPost, serverURL+"/api/auth/login", loginRequestBody)
	if err != nil {
		t.Fatalf("http.NewRequest(login) error = %v", err)
	}
	loginRequest.Header.Set("Origin", serverURL)
	loginRequest.Header.Set("Content-Type", "application/json")

	loginResponse, err := client.Do(loginRequest)
	if err != nil {
		t.Fatalf("client.Do(login) error = %v", err)
	}
	defer loginResponse.Body.Close()
	if loginResponse.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(loginResponse.Body)
		t.Fatalf("login status = %d, want %d, body=%q", loginResponse.StatusCode, http.StatusOK, string(body))
	}

	cookies := loginResponse.Cookies()
	if len(cookies) == 0 {
		t.Fatalf("expected login to set cookies")
	}

	return cookies
}

func dialWebSocketWithCookies(t *testing.T, serverURL string, cookies []*http.Cookie) *websocket.Conn {
	t.Helper()

	wsURL, err := url.Parse(serverURL)
	if err != nil {
		t.Fatalf("url.Parse(serverURL) error = %v", err)
	}
	wsURL.Scheme = strings.Replace(wsURL.Scheme, "http", "ws", 1)
	wsURL.Path = "/api/ws"

	header := http.Header{}
	header.Set("Origin", serverURL)
	for _, cookie := range cookies {
		header.Add("Cookie", cookie.Name+"="+cookie.Value)
	}

	wsConn, wsResponse, err := websocket.DefaultDialer.Dial(wsURL.String(), header)
	if err != nil {
		if wsResponse != nil {
			body, _ := io.ReadAll(wsResponse.Body)
			_ = wsResponse.Body.Close()
			t.Fatalf("websocket dial error = %v (status=%d body=%q)", err, wsResponse.StatusCode, string(body))
		}
		t.Fatalf("websocket dial error = %v", err)
	}

	_ = wsConn.SetReadDeadline(time.Now().Add(5 * time.Second))
	hello := readUntilWSFrame(t, wsConn, func(frame integrationWSFrame) bool {
		return frame.Type == "hello"
	})
	if hello.Type != "hello" {
		t.Fatalf("expected hello frame, got %#v", hello)
	}

	return wsConn
}

func writeWSFrame(t *testing.T, wsConn *websocket.Conn, frame map[string]any) {
	t.Helper()

	if err := wsConn.WriteJSON(frame); err != nil {
		t.Fatalf("WriteJSON(%v) error = %v", frame["type"], err)
	}
}

func readUntilWSFrame(t *testing.T, wsConn *websocket.Conn, match func(integrationWSFrame) bool) integrationWSFrame {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_ = wsConn.SetReadDeadline(time.Now().Add(5 * time.Second))
		var frame integrationWSFrame
		if err := wsConn.ReadJSON(&frame); err != nil {
			t.Fatalf("ReadJSON() error = %v", err)
		}
		if frame.Type == "ping" {
			continue
		}
		if match(frame) {
			return frame
		}
	}

	t.Fatalf("did not receive expected WebSocket frame before deadline")
	return integrationWSFrame{}
}

func decodeRawPayload(t *testing.T, payload json.RawMessage, destination any) {
	t.Helper()
	if err := json.Unmarshal(payload, destination); err != nil {
		t.Fatalf("json.Unmarshal(payload) error = %v", err)
	}
}
