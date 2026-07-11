package httpapi

import "net/http"

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
