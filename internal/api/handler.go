// Package api exposes the engine over HTTP using only the standard library (D-021).
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/bescobarm/card-auth-engine/internal/domain"
	"github.com/bescobarm/card-auth-engine/internal/lifecycle"
	"github.com/bescobarm/card-auth-engine/internal/policy"
)

const (
	maxBodyBytes      = 64 << 10
	microsPerMilli    = 1000
	pathID            = "id"
	headerReplay      = "Idempotent-Replay"
	headerContentType = "Content-Type"
	contentTypeJSON   = "application/json"
	logKeyCompanyID   = "company_id"
	statusOK          = "ok"
	statusUnavailable = "unavailable"
)

// Each failure has its own error; its message is the "error" field of the response.
var (
	errBodyTooLarge          = errors.New("request body is larger than 64 KiB")
	errMalformedRequest      = errors.New("authorization request is not valid JSON")
	errInvalidRequest        = errors.New("authorization request has invalid fields")
	errAuthorizeFailed       = errors.New("authorization could not be decided")
	errAuthorizationNotFound = errors.New("authorization not found")
	errAuditUnavailable      = errors.New("authorization could not be read")
	errMalformedPolicy       = errors.New("policy is not valid JSON or has an unknown field")
	errCompanyMismatch       = errors.New("company id in the body does not match the URL")
	errInvalidPolicy         = errors.New("policy has invalid fields")
	errPolicyConflict        = errors.New("another policy version was published at the same time, retry")
	errPublishFailed         = errors.New("policy could not be published")
)

type apiError struct {
	Error  string            `json:"error"`
	Detail string            `json:"detail,omitempty"`
	Fields map[string]string `json:"fields,omitempty"`
}

type healthStatus struct {
	Status string `json:"status"`
}

type publishedPolicy struct {
	CompanyID string `json:"company_id"`
	Version   int    `json:"version"`
}

// Authorizer decides an authorization request; the bool reports an idempotent replay.
type Authorizer interface {
	Authorize(ctx context.Context, req domain.AuthRequest) (domain.AuthResponse, bool, error)
}

// AuditReader reads a stored authorization from the audit log.
type AuditReader interface {
	GetAuthorization(ctx context.Context, id string) (domain.AuditRecord, error)
}

// PolicyPublisher publishes a validated policy as the company's next version.
type PolicyPublisher interface {
	Publish(ctx context.Context, doc policy.Document) (policy.Document, error)
}

// Pinger checks that the database is reachable.
type Pinger interface {
	Ping(ctx context.Context) error
}

// LifecycleApplier applies a lifecycle event; the bool reports an idempotent replay.
type LifecycleApplier interface {
	Apply(ctx context.Context, ev lifecycle.Event) (lifecycle.Result, bool, error)
}

// Deps are the services behind the routes.
type Deps struct {
	Auth      Authorizer
	Audit     AuditReader
	Policies  PolicyPublisher
	Lifecycle LifecycleApplier
	Metrics   MetricsReader
	DB        Pinger
}

type handler struct {
	Deps

	log *slog.Logger
}

// NewHandler returns the engine's HTTP routes.
func NewHandler(d Deps, log *slog.Logger) http.Handler {
	routes := &handler{Deps: d, log: log}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/authorizations", routes.authorize)
	mux.HandleFunc("GET /v1/authorizations/{id}", routes.getAuthorization)
	mux.HandleFunc("POST /v1/authorizations/{id}/reversal", routes.handleLifecycleEvent(lifecycle.TypeReversal))
	mux.HandleFunc("POST /v1/authorizations/{id}/expire", routes.handleLifecycleEvent(lifecycle.TypeExpiry))
	mux.HandleFunc("POST /v1/authorizations/{id}/increment", routes.handleLifecycleEvent(lifecycle.TypeIncrement))
	mux.HandleFunc("POST /v1/authorizations/{id}/capture", routes.handleLifecycleEvent(lifecycle.TypeCapture))
	mux.HandleFunc("POST /v1/authorizations/{id}/refund", routes.handleLifecycleEvent(lifecycle.TypeRefund))
	mux.HandleFunc("PUT /v1/companies/{id}/policy", routes.publishPolicy)
	mux.HandleFunc("GET /v1/metrics", routes.getMetrics)
	mux.HandleFunc("GET /v1/metrics/series", routes.getMetricsSeries)
	mux.HandleFunc("GET /healthz", routes.checkHealth)

	return routes.logRequests(mux)
}

func (h *handler) authorize(w http.ResponseWriter, r *http.Request) {
	var req domain.AuthRequest

	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes)).Decode(&req)
	if err != nil {
		h.writeBodyError(w, err, errMalformedRequest)
		return
	}

	errs := req.Validate()
	if len(errs) > 0 {
		h.writeJSON(w, http.StatusBadRequest, apiError{Error: errInvalidRequest.Error(), Fields: errs})
		return
	}

	resp, replayed, err := h.Auth.Authorize(r.Context(), req)
	if err != nil {
		h.log.Error(errAuthorizeFailed.Error(), "authorization_id", req.AuthorizationID, "err", err)
		h.writeError(w, http.StatusInternalServerError, errAuthorizeFailed)

		return
	}

	if replayed {
		w.Header().Set(headerReplay, "true")
	}

	h.writeJSON(w, http.StatusOK, resp)
}

func (h *handler) getAuthorization(w http.ResponseWriter, r *http.Request) {
	rec, err := h.Audit.GetAuthorization(r.Context(), r.PathValue(pathID))
	switch {
	case errors.Is(err, domain.ErrNotFound):
		h.writeError(w, http.StatusNotFound, errAuthorizationNotFound)
	case err != nil:
		h.log.Error(errAuditUnavailable.Error(), "err", err)
		h.writeError(w, http.StatusInternalServerError, errAuditUnavailable)
	default:
		h.writeJSON(w, http.StatusOK, rec)
	}
}

// publishPolicy: the server assigns the version; a version in the body is ignored.
func (h *handler) publishPolicy(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		h.writeBodyError(w, err, errMalformedPolicy)
		return
	}

	doc, err := policy.Parse(body)
	if err != nil {
		h.writeBodyError(w, err, errMalformedPolicy)
		return
	}

	companyID := r.PathValue(pathID)
	if doc.CompanyID != "" && doc.CompanyID != companyID {
		h.writeError(w, http.StatusBadRequest, errCompanyMismatch)
		return
	}

	doc.CompanyID = companyID

	errs := doc.Validate()
	if len(errs) > 0 {
		h.writeJSON(w, http.StatusBadRequest, apiError{Error: errInvalidPolicy.Error(), Fields: errs})
		return
	}

	published, err := h.Policies.Publish(r.Context(), doc)
	switch {
	case errors.Is(err, domain.ErrConflict):
		h.writeError(w, http.StatusConflict, errPolicyConflict)
	case err != nil:
		h.log.Error(errPublishFailed.Error(), logKeyCompanyID, companyID, "err", err)
		h.writeError(w, http.StatusInternalServerError, errPublishFailed)
	default:
		h.log.Info("policy published", logKeyCompanyID, companyID, "version", published.Version)
		h.writeJSON(w, http.StatusCreated, publishedPolicy{CompanyID: companyID, Version: published.Version})
	}
}

func (h *handler) checkHealth(w http.ResponseWriter, r *http.Request) {
	err := h.DB.Ping(r.Context())
	if err != nil {
		h.writeJSON(w, http.StatusServiceUnavailable, healthStatus{Status: statusUnavailable})
		return
	}

	h.writeJSON(w, http.StatusOK, healthStatus{Status: statusOK})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (h *handler) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		h.log.Debug("http",
			"method", r.Method, "path", r.URL.Path, "status", rec.status,
			"duration_ms", float64(time.Since(start).Microseconds())/microsPerMilli)
	})
}

func (h *handler) writeBodyError(w http.ResponseWriter, err, malformed error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		h.writeError(w, http.StatusRequestEntityTooLarge, errBodyTooLarge)
		return
	}

	h.writeJSON(w, http.StatusBadRequest, apiError{Error: malformed.Error(), Detail: err.Error()})
}

func (h *handler) writeError(w http.ResponseWriter, status int, err error) {
	h.writeJSON(w, status, apiError{Error: err.Error()})
}

// writeJSON logs an encode failure: the status is already sent, so the client
// can only see a truncated body.
func (h *handler) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set(headerContentType, contentTypeJSON)
	w.WriteHeader(status)

	err := json.NewEncoder(w).Encode(v)
	if err != nil {
		h.log.Error("write response failed", "status", status, "err", err)
	}
}
