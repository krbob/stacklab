package httpapi

import (
	"errors"
	"log/slog"
	"net/http"

	"stacklab/internal/auth"
)

type authController struct {
	*Handler
}

func (c *authController) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/session", c.handleSession)
	mux.HandleFunc("POST /api/auth/login", c.handleLogin)
	mux.HandleFunc("POST /api/auth/logout", c.withAuth(c.handleLogout))
}

func (c *authController) handleSession(w http.ResponseWriter, r *http.Request) {
	session, err := c.auth.AuthenticateRequest(r.Context(), r)
	if err != nil {
		c.writeSessionAuthenticationError(w, err)
		return
	}
	http.SetCookie(w, c.auth.SessionCookie(session))

	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"user": map[string]any{
			"id":           session.UserID,
			"display_name": "Local Operator",
		},
		"features": map[string]any{
			"host_shell": false,
		},
	})
}

func (c *authController) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}

	var request struct {
		Password string `json:"password"`
	}
	if err := decodeJSONWithLimit(w, r, &request, maxLoginJSONBodyBytes); err != nil {
		writeDecodeJSONErrorWithLimit(w, err, maxLoginJSONBodyBytes)
		return
	}

	session, err := c.auth.Login(r.Context(), request.Password, r.UserAgent(), c.auth.ClientIP(r))
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidCredentials):
			writeError(w, http.StatusUnauthorized, "unauthorized", "Invalid password.", nil)
		case errors.Is(err, auth.ErrTooManyAttempts):
			writeError(w, http.StatusTooManyRequests, "rate_limited", "Too many failed login attempts. Try again later.", nil)
		case errors.Is(err, auth.ErrNotConfigured):
			writeError(w, http.StatusServiceUnavailable, "auth_not_configured", "Authentication is not configured yet.", nil)
		default:
			c.logger.Error("login failed", slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to authenticate.", nil)
		}
		return
	}

	if !c.cfg.CookieSecure && c.auth.SecureRequest(r) {
		c.logger.Warn("session cookie secure flag is disabled while request appears to use HTTPS")
	}
	http.SetCookie(w, c.auth.SessionCookie(session))
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
	})
}

func (c *authController) handleLogout(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}

	cookie, err := r.Cookie(c.cfg.SessionCookieName)
	if err != nil || cookie.Value == "" {
		http.SetCookie(w, c.auth.ClearSessionCookie())
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required.", nil)
		return
	}

	if err := c.auth.Logout(r.Context(), cookie.Value); err != nil {
		if errors.Is(err, auth.ErrUnauthorized) {
			http.SetCookie(w, c.auth.ClearSessionCookie())
			writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required.", nil)
			return
		}
		c.logger.Error("logout session failed", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to end session.", nil)
		return
	}

	http.SetCookie(w, c.auth.ClearSessionCookie())
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": false,
	})
}
