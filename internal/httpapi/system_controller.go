package httpapi

import "net/http"

type systemController struct {
	*Handler
}

func (c *systemController) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/live", c.handleLive)
	mux.HandleFunc("GET /api/ready", c.handleReady)
	mux.HandleFunc("GET /api/health", c.handleReady)
	mux.HandleFunc("GET /api/ws", c.handleWebSocket)
	mux.HandleFunc("GET /api/meta", c.withAuth(c.handleMeta))
	mux.HandleFunc("GET /api/service/metrics", c.withAuth(c.handleServiceMetrics))
	mux.HandleFunc("GET /api/host/overview", c.withAuth(c.handleHostOverview))
	mux.HandleFunc("GET /api/host/metrics", c.withAuth(c.handleHostMetrics))
	mux.HandleFunc("GET /api/host/stacklab-logs", c.withAuth(c.handleStacklabLogs))
	mux.HandleFunc("GET /api/docker/admin/overview", c.withAuth(c.handleDockerAdminOverview))
	mux.HandleFunc("GET /api/docker/admin/daemon-config", c.withAuth(c.handleDockerAdminDaemonConfig))
	mux.HandleFunc("POST /api/docker/admin/daemon-config/validate", c.withAuth(c.handleDockerAdminValidateDaemonConfig))
	mux.HandleFunc("POST /api/docker/admin/daemon-config/apply", c.withAuth(c.handleDockerAdminApplyDaemonConfig))
	mux.HandleFunc("GET /api/docker/registries", c.withAuth(c.handleDockerRegistryStatus))
	mux.HandleFunc("POST /api/docker/registries/login", c.withAuth(c.handleDockerRegistryLogin))
	mux.HandleFunc("POST /api/docker/registries/logout", c.withAuth(c.handleDockerRegistryLogout))
	mux.HandleFunc("GET /api/stacklab/update/overview", c.withAuth(c.handleStacklabUpdateOverview))
	mux.HandleFunc("POST /api/stacklab/update/apply", c.withAuth(c.handleStacklabUpdateApply))
	mux.HandleFunc("/api/", c.withAuth(c.handleAPINotImplemented))
	mux.HandleFunc("/", c.handleFrontend)
}
