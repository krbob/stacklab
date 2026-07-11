package httpapi

import "net/http"

type routeController interface {
	registerRoutes(*http.ServeMux)
}
