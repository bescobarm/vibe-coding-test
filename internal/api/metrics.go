package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/bescobarm/card-auth-engine/internal/domain"
)

const (
	querySince          = "since"
	defaultMetricsRange = 24 * time.Hour
)

var (
	errInvalidSince       = errors.New("since must be an RFC 3339 time, e.g. 2026-11-14T15:00:00Z")
	errMetricsUnavailable = errors.New("metrics could not be read")
)

// MetricsReader summarizes the decisions since a moment.
type MetricsReader interface {
	GetMetrics(ctx context.Context, since time.Time) (domain.Metrics, error)
}

// getMetrics answers GET /v1/metrics: decisions, approval rate, decline
// reasons and latency percentiles since ?since=, or over the last 24 hours.
func (h *handler) getMetrics(w http.ResponseWriter, r *http.Request) {
	since := time.Now().Add(-defaultMetricsRange)

	raw := r.URL.Query().Get(querySince)
	if raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			h.writeError(w, http.StatusBadRequest, errInvalidSince)
			return
		}

		since = parsed
	}

	metrics, err := h.Metrics.GetMetrics(r.Context(), since)
	if err != nil {
		h.log.Error(errMetricsUnavailable.Error(), "err", err)
		h.writeError(w, http.StatusInternalServerError, errMetricsUnavailable)

		return
	}

	h.writeJSON(w, http.StatusOK, metrics)
}
