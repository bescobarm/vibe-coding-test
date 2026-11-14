package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/bescobarm/card-auth-engine/internal/domain"
)

const (
	querySince          = "since"
	queryBucketSeconds  = "bucket_seconds"
	defaultMetricsRange = 24 * time.Hour
	defaultBucket       = time.Minute
	maxBucket           = 24 * time.Hour
	maxBuckets          = 1500 // 24 hours by the minute, with room to spare
)

var (
	errInvalidSince       = errors.New("since must be an RFC 3339 time, e.g. 2026-11-14T15:00:00Z")
	errInvalidBucket      = errors.New("bucket_seconds must be a whole number between 1 and 86400")
	errTooManyBuckets     = errors.New("the period has more than 1500 buckets: use a later since or a larger bucket_seconds")
	errMetricsUnavailable = errors.New("metrics could not be read")
)

// MetricsReader summarizes the decisions since a moment, as one total or as
// a series of buckets.
type MetricsReader interface {
	GetMetrics(ctx context.Context, since time.Time) (domain.Metrics, error)
	GetMetricsSeries(ctx context.Context, since time.Time, bucket time.Duration) (domain.MetricsSeries, error)
}

// getMetrics answers GET /v1/metrics: decisions, approval rate, decline
// reasons and latency percentiles since ?since=, or over the last 24 hours.
func (h *handler) getMetrics(w http.ResponseWriter, r *http.Request) {
	since, err := readSince(r)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, err)
		return
	}

	metrics, err := h.Metrics.GetMetrics(r.Context(), since)
	if err != nil {
		h.log.Error(errMetricsUnavailable.Error(), "err", err)
		h.writeError(w, http.StatusInternalServerError, errMetricsUnavailable)

		return
	}

	h.writeJSON(w, http.StatusOK, metrics)
}

// getMetricsSeries answers GET /v1/metrics/series: outcomes and latency
// percentiles per bucket of ?bucket_seconds= (default 60) since ?since=.
func (h *handler) getMetricsSeries(w http.ResponseWriter, r *http.Request) {
	since, err := readSince(r)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, err)
		return
	}

	bucket, err := readBucket(r)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, err)
		return
	}

	if time.Since(since)/bucket > maxBuckets {
		h.writeError(w, http.StatusBadRequest, errTooManyBuckets)
		return
	}

	series, err := h.Metrics.GetMetricsSeries(r.Context(), since, bucket)
	if err != nil {
		h.log.Error(errMetricsUnavailable.Error(), "err", err)
		h.writeError(w, http.StatusInternalServerError, errMetricsUnavailable)

		return
	}

	h.writeJSON(w, http.StatusOK, series)
}

// readSince reads ?since=, or returns 24 hours ago when it is missing.
func readSince(r *http.Request) (time.Time, error) {
	raw := r.URL.Query().Get(querySince)
	if raw == "" {
		return time.Now().Add(-defaultMetricsRange), nil
	}

	since, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, errInvalidSince
	}

	return since, nil
}

// readBucket reads ?bucket_seconds=, or returns one minute when it is missing.
func readBucket(r *http.Request) (time.Duration, error) {
	raw := r.URL.Query().Get(queryBucketSeconds)
	if raw == "" {
		return defaultBucket, nil
	}

	seconds, err := strconv.Atoi(raw)
	if err != nil {
		return 0, errInvalidBucket
	}

	bucket := time.Duration(seconds) * time.Second
	if bucket < time.Second || bucket > maxBucket {
		return 0, errInvalidBucket
	}

	return bucket, nil
}
