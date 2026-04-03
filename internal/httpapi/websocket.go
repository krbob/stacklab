package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"stacklab/internal/auth"
	"stacklab/internal/jobs"
	"stacklab/internal/store"
)

const wsHeartbeatInterval = 20 * time.Second

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
	conn    *websocket.Conn
	writeMu sync.Mutex
}

type wsJobSubscription struct {
	stop        chan struct{}
	unsubscribe func()
}

func (h *Handler) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		http.Error(w, "Cross-origin request rejected.", http.StatusForbidden)
		return
	}

	if _, err := h.auth.AuthenticateRequest(r.Context(), r); err != nil {
		http.SetCookie(w, h.auth.ClearSessionCookie())
		http.Error(w, "Authentication required.", http.StatusUnauthorized)
		return
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Warn("websocket upgrade failed", "err", err)
		return
	}
	defer conn.Close()

	wsConn := &wsConnection{conn: conn}
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

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	subscriptions := map[string]wsJobSubscription{}
	defer func() {
		for _, subscription := range subscriptions {
			close(subscription.stop)
			subscription.unsubscribe()
		}
	}()

	go func() {
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
	}()

	for {
		var frame wsClientFrame
		if err := conn.ReadJSON(&frame); err != nil {
			return
		}

		switch frame.Type {
		case "pong":
			continue
		case "jobs.subscribe":
			var payload struct {
				JobID string `json:"job_id"`
			}
			if err := json.Unmarshal(frame.Payload, &payload); err != nil || payload.JobID == "" {
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
				close(existing.stop)
				existing.unsubscribe()
			}

			liveEvents, unsubscribe := h.jobs.Subscribe(payload.JobID)
			subscription := wsJobSubscription{
				stop:        make(chan struct{}),
				unsubscribe: unsubscribe,
			}
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

			go h.forwardJobEvents(ctx, wsConn, frame.StreamID, job, liveEvents, subscription.stop)
		case "jobs.unsubscribe":
			if existing, ok := subscriptions[frame.StreamID]; ok {
				close(existing.stop)
				existing.unsubscribe()
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
		default:
			if err := wsConn.writeJSON(validationErrorFrame(frame, "Unsupported WebSocket command.")); err != nil {
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
