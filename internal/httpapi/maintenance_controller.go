package httpapi

import "net/http"

type maintenanceController struct {
	*Handler
}

func (c *maintenanceController) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/maintenance/update-stacks", c.withAuth(c.handleUpdateStacksMaintenance))
	mux.HandleFunc("GET /api/maintenance/images", c.withAuth(c.handleMaintenanceImages))
	mux.HandleFunc("GET /api/templates", c.withAuth(c.handleTemplates))
	mux.HandleFunc("GET /api/maintenance/image-updates", c.withAuth(c.handleImageUpdatesList))
	mux.HandleFunc("POST /api/maintenance/image-updates/check", c.withAuth(c.handleImageUpdatesCheck))
	mux.HandleFunc("GET /api/maintenance/networks", c.withAuth(c.handleMaintenanceNetworks))
	mux.HandleFunc("POST /api/maintenance/networks", c.withAuth(c.handleCreateMaintenanceNetwork))
	mux.HandleFunc("DELETE /api/maintenance/networks/{name}", c.withAuth(c.handleDeleteMaintenanceNetwork))
	mux.HandleFunc("GET /api/maintenance/volumes", c.withAuth(c.handleMaintenanceVolumes))
	mux.HandleFunc("POST /api/maintenance/volumes", c.withAuth(c.handleCreateMaintenanceVolume))
	mux.HandleFunc("DELETE /api/maintenance/volumes/{name}", c.withAuth(c.handleDeleteMaintenanceVolume))
	mux.HandleFunc("GET /api/maintenance/prune-preview", c.withAuth(c.handleMaintenancePrunePreview))
	mux.HandleFunc("POST /api/maintenance/prune", c.withAuth(c.handleMaintenancePrune))
}
