package httpapi

import (
	"context"
	"net/http"
	"time"
)

func (h *Handler) handleServiceMetrics(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	h.evaluateReadiness(ctx)
	writeJSON(w, http.StatusOK, h.serviceMetrics.Snapshot(time.Now().UTC()))
}
