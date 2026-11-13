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
	"github.com/bescobarm/card-auth-engine/internal/policy"
)

const (
	maxBodyBytes      = 64 << 10
	microsPerMilli    = 1000
	pathID            = "id"
	headerReplay      = "Idempotent-Replay"
	headerContentType = "Content-Type"
	contentTypeJSON   = "application/json"

	errInvalidJSON    = "invalid_json"
	errInvalidRequest = "invalid_request"
	errInvalidPolicy  = "invalid_policy"
	errNotFound       = "not_found"
	errConflict       = "conflict"
	errInternal       = "internal"

	detailConflict    = "another version was published at the same time; retry"
	fieldCompanyID    = "company_id"
	msgMustMatchURL   = "must match the URL"
	statusOK          = "ok"
	statusUnavailable = "unavailable"
)

// apiError is the body of every non-2xx response.
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

type Authorizer interface {
	Authorize(ctx context.Context, req domain.AuthRequest) (domain.AuthResponse, bool, error)
}

type AuditReader interface {
	GetAuthorization(ctx context.Context, id string) (domain.AuditRecord, error)
}

type PolicyPublisher interface {
	Publish(ctx context.Context, doc policy.Document) (policy.Document, error)
}

type Pinger interface {
	Ping(ctx context.Context) error
}

type handler struct {
	auth     Authorizer
	audit    AuditReader
	policies PolicyPublisher
	db       Pinger
	log      *slog.Logger
}

func NewHandler(auth Authorizer, audit AuditReader, policies PolicyPublisher, db Pinger, log *slog.Logger) http.Handler {
	h := &handler{auth: auth, audit: audit, policies: policies, db: db, log: log}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/authorizations", h.authorize)
	mux.HandleFunc("GET /v1/authorizations/{id}", h.getAuthorization)
	mux.HandleFunc("PUT /v1/companies/{id}/policy", h.publishPolicy)
	mux.HandleFunc("GET /healthz", h.health)

	return h.logRequests(mux)
}

func (h *handler) authorize(w http.ResponseWriter, r *http.Request) {
	var req domain.AuthRequest

	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes)).Decode(&req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: errInvalidJSON, Detail: err.Error()})
		return
	}

	errs := req.Validate()
	if len(errs) > 0 {
		writeJSON(w, http.StatusBadRequest, apiError{Error: errInvalidRequest, Fields: errs})
		return
	}

	resp, replayed, err := h.auth.Authorize(r.Context(), req)
	if err != nil {
		h.log.Error("authorize failed", "authorization_id", req.AuthorizationID, "err", err)

		writeJSON(w, http.StatusInternalServerError, apiError{Error: errInternal})

		return
	}

	h.log.Info("decision",
		"authorization_id", resp.AuthorizationID,
		"card_id", req.CardID,
		"decision", resp.Decision,
		"reasons", reasonCodes(resp.Reasons),
		"latency_ms", resp.LatencyMS,
		"replayed", replayed,
	)

	if replayed {
		w.Header().Set(headerReplay, "true")
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *handler) getAuthorization(w http.ResponseWriter, r *http.Request) {
	rec, err := h.audit.GetAuthorization(r.Context(), r.PathValue(pathID))
	switch {
	case errors.Is(err, domain.ErrNotFound):
		writeJSON(w, http.StatusNotFound, apiError{Error: errNotFound})
	case err != nil:
		h.log.Error("get authorization failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, apiError{Error: errInternal})
	default:
		writeJSON(w, http.StatusOK, rec)
	}
}

// publishPolicy validates a policy document and publishes it as the company's
// next version. The server assigns the version; a version in the body is ignored.
func (h *handler) publishPolicy(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: errInvalidJSON, Detail: err.Error()})
		return
	}

	doc, err := policy.Parse(body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: errInvalidJSON, Detail: err.Error()})
		return
	}

	companyID := r.PathValue(pathID)
	if doc.CompanyID != "" && doc.CompanyID != companyID {
		writeJSON(w, http.StatusBadRequest, apiError{
			Error: errInvalidPolicy, Fields: map[string]string{fieldCompanyID: msgMustMatchURL},
		})

		return
	}

	doc.CompanyID = companyID

	errs := doc.Validate()
	if len(errs) > 0 {
		writeJSON(w, http.StatusBadRequest, apiError{Error: errInvalidPolicy, Fields: errs})
		return
	}

	published, err := h.policies.Publish(r.Context(), doc)
	switch {
	case errors.Is(err, domain.ErrConflict):
		writeJSON(w, http.StatusConflict, apiError{Error: errConflict, Detail: detailConflict})
	case err != nil:
		h.log.Error("publish policy failed", fieldCompanyID, companyID, "err", err)
		writeJSON(w, http.StatusInternalServerError, apiError{Error: errInternal})
	default:
		h.log.Info("policy published", fieldCompanyID, companyID, "version", published.Version)
		writeJSON(w, http.StatusCreated, publishedPolicy{CompanyID: companyID, Version: published.Version})
	}
}

func (h *handler) health(w http.ResponseWriter, r *http.Request) {
	err := h.db.Ping(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, healthStatus{Status: statusUnavailable})
		return
	}

	writeJSON(w, http.StatusOK, healthStatus{Status: statusOK})
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

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set(headerContentType, contentTypeJSON)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func reasonCodes(rs []domain.Reason) []string {
	codes := make([]string, len(rs))
	for i, r := range rs {
		codes[i] = r.Code
	}

	return codes
}
