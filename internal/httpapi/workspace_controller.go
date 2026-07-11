package httpapi

import "net/http"

type workspaceController struct {
	*Handler
}

func (c *workspaceController) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/config/workspace/tree", c.withAuth(c.handleConfigWorkspaceTree))
	mux.HandleFunc("GET /api/config/workspace/file", c.withAuth(c.handleConfigWorkspaceFile))
	mux.HandleFunc("PUT /api/config/workspace/file", c.withAuth(c.handlePutConfigWorkspaceFile))
	mux.HandleFunc("POST /api/config/workspace/repair-permissions", c.withAuth(c.handleRepairConfigWorkspacePermissions))
	mux.HandleFunc("GET /api/git/workspace/status", c.withAuth(c.handleGitWorkspaceStatus))
	mux.HandleFunc("GET /api/git/workspace/diff", c.withAuth(c.handleGitWorkspaceDiff))
	mux.HandleFunc("POST /api/git/workspace/commit", c.withAuth(c.handleGitWorkspaceCommit))
	mux.HandleFunc("POST /api/git/workspace/push", c.withAuth(c.handleGitWorkspacePush))
}
