package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"stacklab/internal/audit"
	"stacklab/internal/auth"
	"stacklab/internal/jobs"
	"stacklab/internal/stacks"
)

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

func (h *operationsController) handleGetJob(w http.ResponseWriter, r *http.Request) {
	job, err := h.jobs.Get(r.Context(), r.PathValue("jobId"))
	if err != nil {
		switch {
		case errors.Is(err, jobs.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Job was not found.", nil)
		default:
			h.logger.Error("get job failed", slog.String("job_id", r.PathValue("jobId")), slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load job.", nil)
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

func (h *operationsController) handleListActiveJobs(w http.ResponseWriter, r *http.Request) {
	response, err := h.jobs.ListActive(r.Context())
	if err != nil {
		h.logger.Error("list active jobs failed", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load active jobs.", nil)
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *operationsController) handleListJobEvents(w http.ResponseWriter, r *http.Request) {
	response, err := h.jobs.Events(r.Context(), r.PathValue("jobId"))
	if err != nil {
		switch {
		case errors.Is(err, jobs.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Job was not found.", nil)
		default:
			h.logger.Error("list job events failed", slog.String("job_id", r.PathValue("jobId")), slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load job events.", nil)
		}
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *operationsController) handleCancelJob(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}

	job, err := h.jobs.Cancel(r.Context(), r.PathValue("jobId"))
	if err != nil {
		switch {
		case errors.Is(err, jobs.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Job was not found.", nil)
		case errors.Is(err, jobs.ErrNotCancellable):
			writeError(w, http.StatusConflict, "invalid_state", "Job cannot be cancelled.", nil)
		default:
			h.logger.Error("cancel job failed", slog.String("job_id", r.PathValue("jobId")), slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to cancel job.", nil)
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

func (h *operationsController) handleListStackAudit(w http.ResponseWriter, r *http.Request) {
	if _, err := h.stackReader.Get(r.Context(), r.PathValue("stackId")); err != nil {
		switch {
		case errors.Is(err, stacks.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Stack was not found.", nil)
		default:
			h.logger.Error("stack audit stack lookup failed", slog.String("stack_id", r.PathValue("stackId")), slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load audit entries.", nil)
		}
		return
	}

	query, err := parseAuditListQuery(r, r.PathValue("stackId"))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "validation_failed", "Invalid audit filters.", nil)
		return
	}
	response, err := h.audit.List(r.Context(), query)
	if err != nil {
		h.logger.Error("list stack audit failed", slog.String("stack_id", r.PathValue("stackId")), slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load audit entries.", nil)
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *operationsController) handleListAudit(w http.ResponseWriter, r *http.Request) {
	query, err := parseAuditListQuery(r, strings.TrimSpace(r.URL.Query().Get("stack_id")))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "validation_failed", "Invalid audit filters.", nil)
		return
	}
	response, err := h.audit.List(r.Context(), query)
	if err != nil {
		h.logger.Error("list audit failed", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load audit entries.", nil)
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func parseLimit(value string) int {
	if strings.TrimSpace(value) == "" {
		return 50
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 50
	}
	return parsed
}

func parseAuditListQuery(r *http.Request, stackID string) (audit.ListQuery, error) {
	const maxSearchLength = 200
	values := r.URL.Query()
	search := strings.TrimSpace(values.Get("q"))
	if utf8.RuneCountInString(search) > maxSearchLength {
		return audit.ListQuery{}, errors.New("audit search is too long")
	}

	var results []string
	switch result := strings.TrimSpace(values.Get("result")); result {
	case "", "all":
	case "failed":
		results = []string{"failed", "timed_out"}
	case "succeeded", "cancelled", "timed_out":
		results = []string{result}
	default:
		return audit.ListQuery{}, errors.New("invalid audit result")
	}

	from, err := parseAuditTime(values.Get("from"))
	if err != nil {
		return audit.ListQuery{}, err
	}
	before, err := parseAuditTime(values.Get("to"))
	if err != nil {
		return audit.ListQuery{}, err
	}
	if from != nil && before != nil && !from.Before(*before) {
		return audit.ListQuery{}, errors.New("invalid audit date range")
	}

	return audit.ListQuery{
		StackID:         stackID,
		Cursor:          strings.TrimSpace(values.Get("cursor")),
		Search:          search,
		Results:         results,
		RequestedFrom:   from,
		RequestedBefore: before,
		Limit:           parseLimit(values.Get("limit")),
	}, nil
}

func parseAuditTime(value string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, errors.New("invalid audit timestamp")
	}
	utc := parsed.UTC()
	return &utc, nil
}
