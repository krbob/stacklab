package httpapi

import "net/http"

type operationsController struct {
	*Handler
}

func (c *operationsController) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/jobs/active", c.withAuth(c.handleListActiveJobs))
	mux.HandleFunc("GET /api/jobs/{jobId}/events", c.withAuth(c.handleListJobEvents))
	mux.HandleFunc("POST /api/jobs/{jobId}/cancel", c.withAuth(c.handleCancelJob))
	mux.HandleFunc("GET /api/jobs/{jobId}", c.withAuth(c.handleGetJob))
	mux.HandleFunc("GET /api/stacks/{stackId}/audit", c.withAuth(c.handleListStackAudit))
	mux.HandleFunc("GET /api/audit", c.withAuth(c.handleListAudit))
}
