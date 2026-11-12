// Package api exposes the engine over HTTP using only the standard library (D-021).
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/bescobarm/card-auth-engine/internal/domain"
)

const maxBodyBytes = 64 << 10

type Authorizer interface {
	Authorize(ctx context.Context, req domain.AuthRequest) (domain.AuthResponse, bool, error)
}

type AuditReader interface {
	GetAuthorization(ctx context.Context, id string) (domain.AuditRecord, error)
}

type Pinger interface {
	Ping(ctx context.Context) error
}

type handler struct {
	auth  Authorizer
	audit AuditReader
	db    Pinger
	log   *slog.Logger
}

func NewHandler(auth Authorizer, audit AuditReader, db Pinger, log *slog.Logger) http.Handler {
	h := &handler{auth: auth, audit: audit, db: db, log: log}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/authorizations", h.authorize)
	mux.HandleFunc("GET /v1/authorizations/{id}", h.getAuthorization)
	mux.HandleFunc("GET /healthz", h.health)

	return h.logRequests(mux)
}

func (h *handler) authorize(w http.ResponseWriter, r *http.Request) {
	var req domain.AuthRequest

	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes)).Decode(&req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json", "detail": err.Error()})
		return
	}

	errs := req.Validate()
	if len(errs) > 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_request", "fields": errs})
		return
	}

	resp, replayed, err := h.auth.Authorize(r.Context(), req)
	if err != nil {
		h.log.Error("authorize failed", "authorization_id", req.AuthorizationID, "err", err)

		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})

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
		w.Header().Set("Idempotent-Replay", "true")
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *handler) getAuthorization(w http.ResponseWriter, r *http.Request) {
	rec, err := h.audit.GetAuthorization(r.Context(), r.PathValue("id"))
	switch {
	case errors.Is(err, domain.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
	case err != nil:
		h.log.Error("get authorization failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
	default:
		writeJSON(w, http.StatusOK, rec)
	}
}

func (h *handler) health(w http.ResponseWriter, r *http.Request) {
	if err := h.db.Ping(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
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
			"duration_ms", float64(time.Since(start).Microseconds())/1000)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
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
