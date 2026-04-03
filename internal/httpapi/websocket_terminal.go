package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"stacklab/internal/stacks"
	"stacklab/internal/terminal"
)

func (h *Handler) openTerminalStream(ctx context.Context, wsConn *wsConnection, subscriptions map[string]*wsSubscription, ownerSessionID, connectionID string, frame wsClientFrame) error {
	var payload struct {
		StackID     string `json:"stack_id"`
		ContainerID string `json:"container_id"`
		Shell       string `json:"shell"`
		Cols        int    `json:"cols"`
		Rows        int    `json:"rows"`
	}
	if err := decodeWSFrame(frame, &payload); err != nil || strings.TrimSpace(frame.StreamID) == "" || strings.TrimSpace(payload.StackID) == "" || strings.TrimSpace(payload.ContainerID) == "" {
		return wsConn.writeJSON(validationErrorFrame(frame, "Invalid terminal.open payload."))
	}

	stackDetail, err := h.stackReader.Get(ctx, payload.StackID)
	if err != nil {
		if errors.Is(err, stacks.ErrNotFound) {
			return wsConn.writeJSON(notFoundErrorFrame(frame, "Stack was not found."))
		}
		return wsConn.writeJSON(internalErrorFrame(frame, "Failed to open terminal session."))
	}
	if !stackDetail.Stack.Capabilities.CanOpenTerminal {
		return wsConn.writeJSON(validationErrorFrame(frame, "Terminal is not available for this stack."))
	}

	var targetContainer *stacks.Container
	for _, container := range stackDetail.Stack.Containers {
		if container.ID == payload.ContainerID {
			targetContainer = &container
			break
		}
	}
	if targetContainer == nil {
		return wsConn.writeJSON(notFoundErrorFrame(frame, "Container was not found."))
	}
	if targetContainer.Status != "running" {
		return wsConn.writeJSON(wsServerFrame{
			Type:      "error",
			RequestID: frame.RequestID,
			StreamID:  frame.StreamID,
			Error: map[string]any{
				"code":    "invalid_state",
				"message": "Container is not running.",
			},
		})
	}

	if existing, ok := subscriptions[frame.StreamID]; ok {
		existing.Close()
		delete(subscriptions, frame.StreamID)
	}

	info, attachmentID, events, err := h.terminals.Open(ownerSessionID, payload.StackID, payload.ContainerID, payload.Shell, payload.Cols, payload.Rows, connectionID)
	if err != nil {
		switch {
		case errors.Is(err, terminal.ErrSessionLimitExceeded):
			return wsConn.writeJSON(wsServerFrame{
				Type:      "error",
				RequestID: frame.RequestID,
				StreamID:  frame.StreamID,
				Error: map[string]any{
					"code":    "limit_exceeded",
					"message": "Maximum concurrent terminal sessions reached.",
				},
			})
		case errors.Is(err, terminal.ErrInvalidShell):
			return wsConn.writeJSON(validationErrorFrame(frame, "Shell is not allowed."))
		default:
			return wsConn.writeJSON(internalErrorFrame(frame, "Failed to open terminal session."))
		}
	}

	subscription := newWSSubscription(func() {
		h.terminals.Detach(ownerSessionID, info.ID, attachmentID)
	})
	subscriptions[frame.StreamID] = subscription

	if err := wsConn.writeJSON(wsServerFrame{
		Type:      "terminal.opened",
		RequestID: frame.RequestID,
		StreamID:  frame.StreamID,
		Payload: map[string]any{
			"session_id":   info.ID,
			"container_id": info.ContainerID,
			"shell":        info.Shell,
		},
	}); err != nil {
		subscription.Close()
		delete(subscriptions, frame.StreamID)
		return err
	}

	go h.forwardTerminalEvents(wsConn, frame.StreamID, info.ID, events, subscription.stop)
	return nil
}

func (h *Handler) attachTerminalStream(wsConn *wsConnection, subscriptions map[string]*wsSubscription, ownerSessionID, connectionID string, frame wsClientFrame) error {
	var payload struct {
		SessionID string `json:"session_id"`
		Cols      int    `json:"cols"`
		Rows      int    `json:"rows"`
	}
	if err := decodeWSFrame(frame, &payload); err != nil || strings.TrimSpace(frame.StreamID) == "" || strings.TrimSpace(payload.SessionID) == "" {
		return wsConn.writeJSON(validationErrorFrame(frame, "Invalid terminal.attach payload."))
	}

	if existing, ok := subscriptions[frame.StreamID]; ok {
		existing.Close()
		delete(subscriptions, frame.StreamID)
	}

	info, attachmentID, events, err := h.terminals.Attach(ownerSessionID, payload.SessionID, payload.Cols, payload.Rows, connectionID)
	if err != nil {
		switch {
		case errors.Is(err, terminal.ErrSessionNotFound):
			return wsConn.writeJSON(wsServerFrame{
				Type:      "error",
				RequestID: frame.RequestID,
				StreamID:  frame.StreamID,
				Error: map[string]any{
					"code":    "terminal_session_not_found",
					"message": "Terminal session was not found.",
				},
			})
		default:
			return wsConn.writeJSON(internalErrorFrame(frame, "Failed to attach terminal session."))
		}
	}

	subscription := newWSSubscription(func() {
		h.terminals.Detach(ownerSessionID, info.ID, attachmentID)
	})
	subscriptions[frame.StreamID] = subscription

	if err := wsConn.writeJSON(wsServerFrame{
		Type:      "terminal.opened",
		RequestID: frame.RequestID,
		StreamID:  frame.StreamID,
		Payload: map[string]any{
			"session_id":   info.ID,
			"container_id": info.ContainerID,
			"shell":        info.Shell,
		},
	}); err != nil {
		subscription.Close()
		delete(subscriptions, frame.StreamID)
		return err
	}

	go h.forwardTerminalEvents(wsConn, frame.StreamID, info.ID, events, subscription.stop)
	return nil
}

func (h *Handler) handleTerminalInput(ownerSessionID string, wsConn *wsConnection, frame wsClientFrame) error {
	var payload struct {
		SessionID string `json:"session_id"`
		Data      string `json:"data"`
	}
	if err := decodeWSFrame(frame, &payload); err != nil || strings.TrimSpace(payload.SessionID) == "" {
		return wsConn.writeJSON(validationErrorFrame(frame, "Invalid terminal.input payload."))
	}
	if err := h.terminals.Input(ownerSessionID, payload.SessionID, payload.Data); err != nil {
		return h.writeTerminalSessionError(wsConn, frame, err)
	}
	return nil
}

func (h *Handler) handleTerminalResize(ownerSessionID string, wsConn *wsConnection, frame wsClientFrame) error {
	var payload struct {
		SessionID string `json:"session_id"`
		Cols      int    `json:"cols"`
		Rows      int    `json:"rows"`
	}
	if err := decodeWSFrame(frame, &payload); err != nil || strings.TrimSpace(payload.SessionID) == "" {
		return wsConn.writeJSON(validationErrorFrame(frame, "Invalid terminal.resize payload."))
	}
	if err := h.terminals.Resize(ownerSessionID, payload.SessionID, payload.Cols, payload.Rows); err != nil {
		return h.writeTerminalSessionError(wsConn, frame, err)
	}
	return nil
}

func (h *Handler) handleTerminalClose(ownerSessionID string, wsConn *wsConnection, frame wsClientFrame) error {
	var payload struct {
		SessionID string `json:"session_id"`
	}
	if err := decodeWSFrame(frame, &payload); err != nil || strings.TrimSpace(payload.SessionID) == "" {
		return wsConn.writeJSON(validationErrorFrame(frame, "Invalid terminal.close payload."))
	}
	if err := h.terminals.Close(ownerSessionID, payload.SessionID, "client_close"); err != nil {
		return h.writeTerminalSessionError(wsConn, frame, err)
	}
	return nil
}

func (h *Handler) forwardTerminalEvents(wsConn *wsConnection, streamID, sessionID string, events <-chan terminal.Event, stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			switch event.Type {
			case "output":
				if err := wsConn.writeJSON(wsServerFrame{
					Type:     "terminal.output",
					StreamID: streamID,
					Payload: map[string]any{
						"session_id": sessionID,
						"data":       event.Data,
					},
				}); err != nil {
					return
				}
			case "exited":
				if err := wsConn.writeJSON(wsServerFrame{
					Type:     "terminal.exited",
					StreamID: streamID,
					Payload: map[string]any{
						"session_id": sessionID,
						"exit_code":  event.ExitCode,
						"reason":     event.Reason,
					},
				}); err != nil {
					return
				}
				return
			}
		}
	}
}

func (h *Handler) writeTerminalSessionError(wsConn *wsConnection, frame wsClientFrame, err error) error {
	switch {
	case errors.Is(err, terminal.ErrSessionNotFound):
		return wsConn.writeJSON(wsServerFrame{
			Type:      "error",
			RequestID: frame.RequestID,
			StreamID:  frame.StreamID,
			Error: map[string]any{
				"code":    "terminal_session_not_found",
				"message": "Terminal session was not found.",
			},
		})
	default:
		return wsConn.writeJSON(internalErrorFrame(frame, "Terminal session operation failed."))
	}
}

func decodeWSFrame(frame wsClientFrame, destination any) error {
	return json.Unmarshal(frame.Payload, destination)
}
