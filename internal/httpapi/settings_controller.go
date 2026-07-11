package httpapi

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"stacklab/internal/auth"
	"stacklab/internal/hostinfo"
	"stacklab/internal/notifications"
	"stacklab/internal/scheduler"
)

type settingsController struct {
	*Handler
}

func (c *settingsController) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/settings/notifications", c.withAuth(c.handleGetNotificationSettings))
	mux.HandleFunc("PUT /api/settings/notifications", c.withAuth(c.handleUpdateNotificationSettings))
	mux.HandleFunc("POST /api/settings/notifications/test", c.withAuth(c.handleSendNotificationTest))
	mux.HandleFunc("GET /api/settings/host", c.withAuth(c.handleGetHostSettings))
	mux.HandleFunc("PUT /api/settings/host", c.withAuth(c.handleUpdateHostSettings))
	mux.HandleFunc("GET /api/settings/maintenance-schedules", c.withAuth(c.handleGetMaintenanceSchedules))
	mux.HandleFunc("PUT /api/settings/maintenance-schedules", c.withAuth(c.handleUpdateMaintenanceSchedules))
	mux.HandleFunc("POST /api/settings/password", c.withAuth(c.handleUpdatePassword))
}

func (c *settingsController) handleGetNotificationSettings(w http.ResponseWriter, r *http.Request) {
	if c.notifications == nil {
		writeError(w, http.StatusNotImplemented, "not_implemented", "Notifications are not configured yet.", nil)
		return
	}

	response, err := c.notifications.GetSettings(r.Context())
	if err != nil {
		c.logger.Error("get notification settings failed", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load notification settings.", nil)
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (c *settingsController) handleUpdateNotificationSettings(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}
	if c.notifications == nil {
		writeError(w, http.StatusNotImplemented, "not_implemented", "Notifications are not configured yet.", nil)
		return
	}

	var request notifications.UpdateSettingsRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeDecodeJSONError(w, err)
		return
	}

	requestedAt := time.Now().UTC()
	response, err := c.notifications.UpdateSettings(r.Context(), request)
	if err != nil {
		switch {
		case errors.Is(err, notifications.ErrInvalidConfig):
			writeError(w, http.StatusBadRequest, "validation_failed", err.Error(), nil)
		default:
			c.logger.Error("update notification settings failed", slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to update notification settings.", nil)
		}
		return
	}

	finishedAt := time.Now().UTC()
	if err := c.audit.RecordSystemEvent(r.Context(), "update_notification_settings", "local", "succeeded", requestedAt, &finishedAt, map[string]any{
		"enabled":    response.Enabled,
		"configured": response.Configured,
	}); err != nil {
		c.logger.Warn("record notification settings audit failed", slog.String("err", err.Error()))
	}

	writeJSON(w, http.StatusOK, response)
}

func (c *settingsController) handleSendNotificationTest(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}
	if c.notifications == nil {
		writeError(w, http.StatusNotImplemented, "not_implemented", "Notifications are not configured yet.", nil)
		return
	}

	var request notifications.TestRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeDecodeJSONError(w, err)
		return
	}

	requestedAt := time.Now().UTC()
	response, err := c.notifications.SendTest(r.Context(), request)
	if err != nil {
		switch {
		case errors.Is(err, notifications.ErrInvalidConfig):
			writeError(w, http.StatusBadRequest, "validation_failed", err.Error(), nil)
		default:
			c.logger.Warn("send notification test failed", slog.String("err", err.Error()))
			writeError(w, http.StatusBadGateway, "delivery_failed", "Failed to deliver the test notification.", nil)
		}
		return
	}

	finishedAt := time.Now().UTC()
	if err := c.audit.RecordSystemEvent(r.Context(), "send_notification_test", "local", "succeeded", requestedAt, &finishedAt, nil); err != nil {
		c.logger.Warn("record notification test audit failed", slog.String("err", err.Error()))
	}

	writeJSON(w, http.StatusOK, response)
}

func (c *settingsController) handleGetHostSettings(w http.ResponseWriter, r *http.Request) {
	response, err := c.hostInfo.GetSettings(r.Context())
	if err != nil {
		c.logger.Error("get host settings failed", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load host settings.", nil)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (c *settingsController) handleUpdateHostSettings(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}

	var request hostinfo.UpdateSettingsRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeDecodeJSONError(w, err)
		return
	}

	requestedAt := time.Now().UTC()
	response, err := c.hostInfo.UpdateSettings(r.Context(), request)
	if err != nil {
		c.logger.Error("update host settings failed", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to update host settings.", nil)
		return
	}

	finishedAt := time.Now().UTC()
	if err := c.audit.RecordSystemEvent(r.Context(), "update_host_settings", "local", "succeeded", requestedAt, &finishedAt, map[string]any{
		"public_ip_lookup_enabled": response.PublicIPLookupEnabled,
	}); err != nil {
		c.logger.Warn("record host settings update failed", slog.String("err", err.Error()))
	}

	writeJSON(w, http.StatusOK, response)
}

func (c *settingsController) handleGetMaintenanceSchedules(w http.ResponseWriter, r *http.Request) {
	response, err := c.schedules.GetSettings(r.Context())
	if err != nil {
		c.logger.Error("get maintenance schedules failed", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load maintenance schedules.", nil)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (c *settingsController) handleUpdateMaintenanceSchedules(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}

	var request scheduler.UpdateSettingsRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeDecodeJSONError(w, err)
		return
	}

	requestedAt := time.Now().UTC()
	response, err := c.schedules.UpdateSettings(r.Context(), request)
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", err.Error(), nil)
		return
	}

	finishedAt := time.Now().UTC()
	if err := c.audit.RecordSystemEvent(r.Context(), "update_maintenance_schedules", "local", "succeeded", requestedAt, &finishedAt, map[string]any{
		"update_enabled": response.Update.Enabled,
		"prune_enabled":  response.Prune.Enabled,
	}); err != nil {
		c.logger.Warn("record maintenance schedules audit failed", slog.String("err", err.Error()))
	}

	writeJSON(w, http.StatusOK, response)
}

func (c *settingsController) handleUpdatePassword(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}

	var request struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeDecodeJSONError(w, err)
		return
	}

	requestedAt := time.Now().UTC()

	if err := c.auth.UpdatePassword(r.Context(), request.CurrentPassword, request.NewPassword); err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidCredentials):
			writeError(w, http.StatusUnauthorized, "unauthorized", "Current password is invalid.", nil)
		case errors.Is(err, auth.ErrInvalidPassword):
			writeError(w, http.StatusUnprocessableEntity, "validation_failed", fmt.Sprintf(
				"New password must contain between %d and %d characters.",
				auth.PasswordMinimumLength,
				auth.PasswordMaximumLength,
			), map[string]any{
				"min_length": auth.PasswordMinimumLength,
				"max_length": auth.PasswordMaximumLength,
			})
		case errors.Is(err, auth.ErrNotConfigured):
			writeError(w, http.StatusServiceUnavailable, "auth_not_configured", "Authentication is not configured yet.", nil)
		default:
			c.logger.Error("update password failed", slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to update password.", nil)
		}
		return
	}

	finishedAt := time.Now().UTC()
	if err := c.audit.RecordSystemEvent(r.Context(), "update_password", "local", "succeeded", requestedAt, &finishedAt, nil); err != nil {
		c.logger.Warn("record password update audit failed", slog.String("err", err.Error()))
	}

	http.SetCookie(w, c.auth.ClearSessionCookie())
	writeJSON(w, http.StatusOK, map[string]any{
		"updated":                   true,
		"reauthentication_required": true,
	})
}
