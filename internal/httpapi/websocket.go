package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"stacklab/internal/auth"
	"stacklab/internal/jobs"
	"stacklab/internal/store"
)

const (
	wsHeartbeatInterval                   = 20 * time.Second
	wsReadLimitBytes                int64 = 64 << 10
	wsPongWait                            = 2*wsHeartbeatInterval + 10*time.Second
	wsMaxSubscriptionsPerConnection       = 32
)

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		return auth.SameOrigin(r)
	},
}

type wsClientFrame struct {
	Type      string          `json:"type"`
	RequestID string          `json:"request_id,omitempty"`
	StreamID  string          `json:"stream_id,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

type wsServerFrame struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id,omitempty"`
	StreamID  string `json:"stream_id,omitempty"`
	Payload   any    `json:"payload,omitempty"`
	Error     any    `json:"error,omitempty"`
}

type wsConnection struct {
	conn      *websocket.Conn
	writeMu   sync.Mutex
	closeOnce sync.Once
	workers   sync.WaitGroup
}

func (h *Handler) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		http.Error(w, "Cross-origin request rejected.", http.StatusForbidden)
		return
	}

	session, terminations, unsubscribeSession, err := h.auth.AuthenticateWebSocket(r.Context(), r)
	if err != nil {
		if errors.Is(err, auth.ErrUnauthorized) {
			http.SetCookie(w, h.auth.ClearSessionCookie())
			http.Error(w, "Authentication required.", http.StatusUnauthorized)
			return
		}
		h.logger.Error("validate websocket session failed", "err", err)
		http.Error(w, "Failed to validate session.", http.StatusInternalServerError)
		return
	}

	upgradeHeaders := http.Header{}
	upgradeHeaders.Add("Set-Cookie", h.auth.SessionCookie(session).String())
	conn, err := wsUpgrader.Upgrade(w, r, upgradeHeaders)
	if err != nil {
		unsubscribeSession()
		h.logger.Warn("websocket upgrade failed", "err", err)
		return
	}
	conn.SetReadLimit(wsReadLimitBytes)
	_ = conn.SetReadDeadline(time.Now().Add(wsPongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(wsPongWait))
	})

	wsConn := &wsConnection{conn: conn}
	if !h.registerWebSocket(wsConn) {
		unsubscribeSession()
		wsConn.close(websocket.CloseGoingAway, "server shutting down")
		return
	}
	defer func() {
		wsConn.wait()
		h.unregisterWebSocket(wsConn)
		wsConn.close(websocket.CloseNormalClosure, "connection closed")
	}()
	defer unsubscribeSession()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	wsConn.goRun(func() {
		select {
		case <-ctx.Done():
			return
		case termination, ok := <-terminations:
			if !ok {
				return
			}
			wsConn.close(websocket.ClosePolicyViolation, "authentication "+string(termination.Reason))
			cancel()
		}
	})

	connectionID := "conn_" + randomID(18)
	if err := wsConn.writeJSON(wsServerFrame{
		Type: "hello",
		Payload: map[string]any{
			"connection_id":         connectionID,
			"protocol_version":      1,
			"heartbeat_interval_ms": wsHeartbeatInterval.Milliseconds(),
			"features": map[string]any{
				"host_shell": false,
			},
		},
	}); err != nil {
		return
	}

	subscriptions := map[string]*wsSubscription{}
	defer func() {
		for _, subscription := range subscriptions {
			subscription.Close()
		}
	}()

	wsConn.goRun(func() {
		ticker := time.NewTicker(wsHeartbeatInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case tick := <-ticker.C:
				if err := wsConn.writeJSON(wsServerFrame{
					Type: "ping",
					Payload: map[string]any{
						"ts": tick.UTC().Format(time.RFC3339Nano),
					},
				}); err != nil {
					cancel()
					return
				}
			}
		}
	})

	for {
		var frame wsClientFrame
		if err := conn.ReadJSON(&frame); err != nil {
			return
		}
		if frame.Type != "pong" {
			if err := h.auth.TouchSessionActivity(ctx, session.ID); err != nil {
				h.terminals.CloseOwner(session.ID, "auth_revoked")
				if errors.Is(err, auth.ErrUnauthorized) {
					wsConn.close(websocket.ClosePolicyViolation, "authentication revoked")
				} else {
					h.logger.Warn("persist websocket session activity failed", "session_id", session.ID, "err", err)
					wsConn.close(websocket.CloseInternalServerErr, "session persistence failed")
				}
				return
			}
		}

		switch frame.Type {
		case "pong":
			_ = conn.SetReadDeadline(time.Now().Add(wsPongWait))
			continue
		case "logs.subscribe":
			if err := h.subscribeLogStream(ctx, wsConn, subscriptions, frame); err != nil {
				return
			}
		case "logs.unsubscribe":
			if existing, ok := subscriptions[frame.StreamID]; ok {
				existing.Close()
				delete(subscriptions, frame.StreamID)
			}

			if err := wsConn.writeJSON(wsServerFrame{
				Type:      "ack",
				RequestID: frame.RequestID,
				StreamID:  frame.StreamID,
				Payload: map[string]any{
					"status": "unsubscribed",
				},
			}); err != nil {
				return
			}
		case "stats.subscribe":
			if err := h.subscribeStatsStream(ctx, wsConn, subscriptions, frame); err != nil {
				return
			}
		case "stats.unsubscribe":
			if existing, ok := subscriptions[frame.StreamID]; ok {
				existing.Close()
				delete(subscriptions, frame.StreamID)
			}

			if err := wsConn.writeJSON(wsServerFrame{
				Type:      "ack",
				RequestID: frame.RequestID,
				StreamID:  frame.StreamID,
				Payload: map[string]any{
					"status": "unsubscribed",
				},
			}); err != nil {
				return
			}
		case "activity.subscribe":
			if strings.TrimSpace(frame.StreamID) == "" {
				_ = wsConn.writeJSON(validationErrorFrame(frame, "Invalid activity.subscribe payload."))
				continue
			}
			if ok, err := ensureWSSubscriptionSlot(wsConn, subscriptions, frame); err != nil || !ok {
				if err != nil {
					return
				}
				continue
			}
			if existing, ok := subscriptions[frame.StreamID]; ok {
				existing.Close()
			}

			signal, unsubscribe := h.jobs.SubscribeActivity()
			subscription := newWSSubscription(unsubscribe)
			subscriptions[frame.StreamID] = subscription

			if err := wsConn.writeJSON(wsServerFrame{
				Type:      "ack",
				RequestID: frame.RequestID,
				StreamID:  frame.StreamID,
				Payload: map[string]any{
					"status": "subscribed",
				},
			}); err != nil {
				return
			}

			if snapshot, err := h.jobs.ListActive(ctx); err == nil {
				if err := wsConn.writeJSON(wsServerFrame{Type: "activity.snapshot", StreamID: frame.StreamID, Payload: snapshot}); err != nil {
					return
				}
			}

			wsConn.goRun(func() { h.forwardActivity(ctx, wsConn, frame.StreamID, signal, subscription.stop) })
		case "activity.unsubscribe":
			if existing, ok := subscriptions[frame.StreamID]; ok {
				existing.Close()
				delete(subscriptions, frame.StreamID)
			}

			if err := wsConn.writeJSON(wsServerFrame{
				Type:      "ack",
				RequestID: frame.RequestID,
				StreamID:  frame.StreamID,
				Payload: map[string]any{
					"status": "unsubscribed",
				},
			}); err != nil {
				return
			}
		case "jobs.subscribe":
			var payload struct {
				JobID string `json:"job_id"`
			}
			if err := json.Unmarshal(frame.Payload, &payload); err != nil || payload.JobID == "" || strings.TrimSpace(frame.StreamID) == "" {
				_ = wsConn.writeJSON(validationErrorFrame(frame, "Invalid jobs.subscribe payload."))
				continue
			}

			job, err := h.jobs.Get(ctx, payload.JobID)
			if err != nil {
				switch {
				case errors.Is(err, jobs.ErrNotFound):
					_ = wsConn.writeJSON(notFoundErrorFrame(frame, "Job was not found."))
				default:
					_ = wsConn.writeJSON(internalErrorFrame(frame, "Failed to subscribe to job."))
				}
				continue
			}

			if existing, ok := subscriptions[frame.StreamID]; ok {
				existing.Close()
			}
			if ok, err := ensureWSSubscriptionSlot(wsConn, subscriptions, frame); err != nil || !ok {
				if err != nil {
					return
				}
				continue
			}

			liveEvents, unsubscribe := h.jobs.Subscribe(payload.JobID)
			subscription := newWSSubscription(unsubscribe)
			subscriptions[frame.StreamID] = subscription

			if err := wsConn.writeJSON(wsServerFrame{
				Type:      "ack",
				RequestID: frame.RequestID,
				StreamID:  frame.StreamID,
				Payload: map[string]any{
					"status": "subscribed",
				},
			}); err != nil {
				return
			}

			replayEvents, err := h.jobs.ReplayEvents(ctx, payload.JobID)
			if err != nil {
				_ = wsConn.writeJSON(internalErrorFrame(frame, "Failed to replay job events."))
				continue
			}
			for _, event := range replayEvents {
				if err := wsConn.writeJSON(jobEventFrame(frame.StreamID, job, event)); err != nil {
					return
				}
			}

			wsConn.goRun(func() { h.forwardJobEvents(ctx, wsConn, frame.StreamID, job, liveEvents, subscription.stop) })
		case "jobs.unsubscribe":
			if existing, ok := subscriptions[frame.StreamID]; ok {
				existing.Close()
				delete(subscriptions, frame.StreamID)
			}

			if err := wsConn.writeJSON(wsServerFrame{
				Type:      "ack",
				RequestID: frame.RequestID,
				StreamID:  frame.StreamID,
				Payload: map[string]any{
					"status": "unsubscribed",
				},
			}); err != nil {
				return
			}
		case "terminal.open":
			if err := h.openTerminalStream(ctx, wsConn, subscriptions, session.ID, connectionID, frame); err != nil {
				return
			}
		case "terminal.attach":
			if err := h.attachTerminalStream(wsConn, subscriptions, session.ID, connectionID, frame); err != nil {
				return
			}
		case "terminal.input":
			if err := h.handleTerminalInput(session.ID, wsConn, frame); err != nil {
				return
			}
		case "terminal.resize":
			if err := h.handleTerminalResize(session.ID, wsConn, frame); err != nil {
				return
			}
		case "terminal.close":
			if err := h.handleTerminalClose(session.ID, wsConn, frame); err != nil {
				return
			}
		default:
			if err := wsConn.writeJSON(validationErrorFrame(frame, "Unsupported WebSocket command.")); err != nil {
				return
			}
		}
	}
}

func (c *wsConnection) close(code int, reason string) {
	if c == nil || c.conn == nil {
		return
	}
	c.closeOnce.Do(func() {
		_ = c.conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, reason), time.Now().Add(time.Second))
		_ = c.conn.Close()
	})
}

func (c *wsConnection) goRun(run func()) {
	if run == nil {
		return
	}
	c.workers.Add(1)
	go func() {
		defer c.workers.Done()
		run()
	}()
}

func (c *wsConnection) wait() {
	c.workers.Wait()
}

func (h *Handler) registerWebSocket(conn *wsConnection) bool {
	h.wsMu.Lock()
	defer h.wsMu.Unlock()
	if h.wsClosing {
		return false
	}
	if h.wsConnections == nil {
		h.wsConnections = map[*wsConnection]struct{}{}
	}
	h.wsConnections[conn] = struct{}{}
	h.wsWG.Add(1)
	return true
}

func (h *Handler) unregisterWebSocket(conn *wsConnection) {
	h.wsMu.Lock()
	if _, ok := h.wsConnections[conn]; ok {
		delete(h.wsConnections, conn)
		h.wsWG.Done()
	}
	h.wsMu.Unlock()
}

func (h *Handler) closeWebSockets() {
	h.wsMu.Lock()
	h.wsClosing = true
	connections := make([]*wsConnection, 0, len(h.wsConnections))
	for conn := range h.wsConnections {
		connections = append(connections, conn)
	}
	h.wsMu.Unlock()

	for _, conn := range connections {
		conn.close(websocket.CloseGoingAway, "server shutting down")
	}
}

func (h *Handler) waitForWebSockets(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		h.wsWG.Wait()
		close(done)
	}()
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for websocket connections: %w", ctx.Err())
	}
}

func ensureWSSubscriptionSlot(wsConn *wsConnection, subscriptions map[string]*wsSubscription, frame wsClientFrame) (bool, error) {
	if _, exists := subscriptions[frame.StreamID]; exists {
		return true, nil
	}
	if len(subscriptions) < wsMaxSubscriptionsPerConnection {
		return true, nil
	}
	return false, wsConn.writeJSON(wsServerFrame{
		Type:      "error",
		RequestID: frame.RequestID,
		StreamID:  frame.StreamID,
		Error: map[string]any{
			"code":    "limit_exceeded",
			"message": "Maximum WebSocket subscriptions reached for this connection.",
			"details": map[string]any{
				"max_subscriptions": wsMaxSubscriptionsPerConnection,
			},
		},
	})
}

// forwardActivity pushes throttled activity.update frames whenever the jobs
// service signals a change; each frame carries the full active-jobs payload
// (same shape as GET /api/jobs/active), latest wins.
func (h *Handler) forwardActivity(ctx context.Context, wsConn *wsConnection, streamID string, signal <-chan struct{}, stop <-chan struct{}) {
	const minInterval = 500 * time.Millisecond
	var lastSent time.Time

	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case <-signal:
			if wait := minInterval - time.Since(lastSent); wait > 0 {
				timer := time.NewTimer(wait)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-stop:
					timer.Stop()
					return
				case <-timer.C:
				}
			}
			// Coalesce anything that queued while throttled.
			for {
				select {
				case <-signal:
					continue
				default:
				}
				break
			}

			response, err := h.jobs.ListActive(ctx)
			if err != nil {
				continue
			}
			lastSent = time.Now()
			if err := wsConn.writeJSON(wsServerFrame{Type: "activity.update", StreamID: streamID, Payload: response}); err != nil {
				return
			}
		}
	}
}

func (h *Handler) forwardJobEvents(ctx context.Context, wsConn *wsConnection, streamID string, job store.Job, events <-chan store.JobEvent, stop <-chan struct{}) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case event := <-events:
			if err := wsConn.writeJSON(jobEventFrame(streamID, job, event)); err != nil {
				return
			}
		}
	}
}

func (c *wsConnection) writeJSON(frame wsServerFrame) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.conn.WriteJSON(frame)
}

func jobEventFrame(streamID string, job store.Job, event store.JobEvent) wsServerFrame {
	return wsServerFrame{
		Type:     "jobs.event",
		StreamID: streamID,
		Payload: map[string]any{
			"job_id":    job.ID,
			"stack_id":  job.StackID,
			"action":    job.Action,
			"state":     event.State,
			"event":     event.Event,
			"message":   event.Message,
			"data":      emptyToNil(event.Data),
			"step":      event.Step,
			"progress":  event.Progress,
			"timestamp": event.Timestamp.UTC().Format(time.RFC3339Nano),
		},
	}
}

func validationErrorFrame(frame wsClientFrame, message string) wsServerFrame {
	return wsServerFrame{
		Type:      "error",
		RequestID: frame.RequestID,
		StreamID:  frame.StreamID,
		Error: map[string]any{
			"code":    "validation_failed",
			"message": message,
		},
	}
}

func notFoundErrorFrame(frame wsClientFrame, message string) wsServerFrame {
	return wsServerFrame{
		Type:      "error",
		RequestID: frame.RequestID,
		StreamID:  frame.StreamID,
		Error: map[string]any{
			"code":    "not_found",
			"message": message,
		},
	}
}

func internalErrorFrame(frame wsClientFrame, message string) wsServerFrame {
	return wsServerFrame{
		Type:      "error",
		RequestID: frame.RequestID,
		StreamID:  frame.StreamID,
		Error: map[string]any{
			"code":    "internal_error",
			"message": message,
		},
	}
}

func streamErrorFrame(streamID, code, message string) wsServerFrame {
	return wsServerFrame{
		Type:     "error",
		StreamID: streamID,
		Error: map[string]any{
			"code":    code,
			"message": message,
		},
	}
}

func emptyToNil(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func randomID(length int) string {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return base64.RawURLEncoding.EncodeToString(bytes)
}
