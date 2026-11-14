package api

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bescobarm/card-auth-engine/internal/domain"
)

var errQueryFailed = errors.New("query failed")

type fakeMetrics struct {
	metrics domain.Metrics
	err     error
	since   time.Time
}

func (f *fakeMetrics) GetMetrics(_ context.Context, since time.Time) (domain.Metrics, error) {
	f.since = since
	return f.metrics, f.err
}

func newMetricsHandler(metrics *fakeMetrics) http.Handler {
	return NewHandler(Deps{Metrics: metrics}, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// AC3 of SP06: percentiles and counts by reason.
func TestMetricsReturnsCountsReasonsAndPercentiles(t *testing.T) {
	c := require.New(t)
	since := time.Date(2026, 11, 14, 15, 0, 0, 0, time.UTC)
	metrics := &fakeMetrics{metrics: domain.Metrics{
		Since: since, Decisions: 10, Approved: 8, Declined: 2, Flagged: 1, ApprovalRate: 0.8,
		DeclineReasons: map[string]int{"MCC_BLOCKED": 2},
		LatencyMS:      domain.LatencyPercentiles{P50: 2.1, P95: 3.4, P99: 5.6},
	}}

	recorder := doRequest(newMetricsHandler(metrics), http.MethodGet, "/v1/metrics?since=2026-11-14T15:00:00Z", "")

	c.Equal(http.StatusOK, recorder.Code)
	c.Equal(since, metrics.since)
	c.JSONEq(`{"since": "2026-11-14T15:00:00Z", "decisions": 10, "approved": 8, "declined": 2, "flagged": 1,
		"fallback_used": 0, "approval_rate": 0.8, "decline_reasons": {"MCC_BLOCKED": 2},
		"latency_ms": {"p50": 2.1, "p95": 3.4, "p99": 5.6},
		"explainer": {"explained": 0, "fallback_used": 0, "failed": 0}}`, recorder.Body.String())
}

func TestMetricsDefaultsToTheLast24Hours(t *testing.T) {
	c := require.New(t)
	metrics := &fakeMetrics{}

	recorder := doRequest(newMetricsHandler(metrics), http.MethodGet, "/v1/metrics", "")

	c.Equal(http.StatusOK, recorder.Code)
	c.WithinDuration(time.Now().Add(-24*time.Hour), metrics.since, time.Minute)
}

func TestMetricsRejectsABadSince(t *testing.T) {
	c := require.New(t)

	recorder := doRequest(newMetricsHandler(&fakeMetrics{}), http.MethodGet, "/v1/metrics?since=yesterday", "")

	c.Equal(http.StatusBadRequest, recorder.Code)
	c.EqualError(errInvalidSince, decodeError(c, recorder).Error)
}

func TestMetricsHidesStoreErrors(t *testing.T) {
	c := require.New(t)

	recorder := doRequest(newMetricsHandler(&fakeMetrics{err: errQueryFailed}), http.MethodGet, "/v1/metrics", "")

	c.Equal(http.StatusInternalServerError, recorder.Code)
	c.EqualError(errMetricsUnavailable, decodeError(c, recorder).Error)
}
