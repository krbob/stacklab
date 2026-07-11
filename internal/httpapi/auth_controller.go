package httpapi

import "net/http"

type authController struct {
	*Handler
}

func (c *authController) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/session", c.handleSession)
	mux.HandleFunc("POST /api/auth/login", c.handleLogin)
	mux.HandleFunc("POST /api/auth/logout", c.withAuth(c.handleLogout))
}
