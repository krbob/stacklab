package httpapi

import "net/http"

type stackController struct {
	*Handler
}

func (c *stackController) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/stacks", c.withAuth(c.handleListStacks))
	mux.HandleFunc("POST /api/stacks", c.withAuth(c.handleCreateStack))
	mux.HandleFunc("GET /api/stacks/{stackId}", c.withAuth(c.handleGetStack))
	mux.HandleFunc("DELETE /api/stacks/{stackId}", c.withAuth(c.handleDeleteStack))
	mux.HandleFunc("GET /api/stacks/{stackId}/definition", c.withAuth(c.handleGetDefinition))
	mux.HandleFunc("PUT /api/stacks/{stackId}/definition", c.withAuth(c.handlePutDefinition))
	mux.HandleFunc("GET /api/stacks/{stackId}/workspace/tree", c.withAuth(c.handleStackWorkspaceTree))
	mux.HandleFunc("GET /api/stacks/{stackId}/workspace/file", c.withAuth(c.handleStackWorkspaceFile))
	mux.HandleFunc("PUT /api/stacks/{stackId}/workspace/file", c.withAuth(c.handlePutStackWorkspaceFile))
	mux.HandleFunc("POST /api/stacks/{stackId}/workspace/repair-permissions", c.withAuth(c.handleRepairStackWorkspacePermissions))
	mux.HandleFunc("GET /api/stacks/{stackId}/resolved-config", c.withAuth(c.handleGetResolvedConfig))
	mux.HandleFunc("POST /api/stacks/{stackId}/resolved-config", c.withAuth(c.handlePostResolvedConfig))
	mux.HandleFunc("POST /api/stacks/{stackId}/actions/{action}", c.withAuth(c.handleRunStackAction))
}
