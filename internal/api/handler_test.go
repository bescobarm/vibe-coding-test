package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bescobarm/card-auth-engine/internal/domain"
)

type fakeAuthorizer struct {
	resp     domain.AuthResponse
	replayed bool
	err      error
	got      domain.AuthRequest
}

func (f *fakeAuthorizer) Authorize(_ context.Context, r domain.AuthRequest) (domain.AuthResponse, bool, error) {
	f.got = r
	return f.resp, f.replayed, f.err
}

type fakeAudit struct {
	rec domain.AuditRecord
	err error
}

func (f fakeAudit) GetAuthorization(context.Context, string) (domain.AuditRecord, error) {
	return f.rec, f.err
}

type fakePinger struct{ err error }

func (f fakePinger) Ping(context.Context) error { return f.err }

const validBody = `{"authorization_id":"auth_1","card_id":"card_123","amount_minor":4500,"currency":"USD",
"merchant":{"id":"m_1","name":"ACME","mcc":"5734","country":"US"},"entry_mode":"ecommerce"}`

func newTestHandler(auth Authorizer, audit AuditReader, db Pinger) http.Handler {
	return NewHandler(auth, audit, db, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func do(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
	return w
}

func decode(c *require.Assertions, w *httptest.ResponseRecorder) map[string]any {
	var m map[string]any
	c.NoError(json.Unmarshal(w.Body.Bytes(), &m), "body is not JSON: %q", w.Body.String())
	return m
}

func TestAuthorizeReturnsTheDecision(t *testing.T) {
	c := require.New(t)
	auth := &fakeAuthorizer{resp: domain.AuthResponse{
		AuthorizationID: "auth_1", Decision: domain.Decline,
		Reasons: []domain.Reason{{Code: domain.ReasonCardInactive, Message: "Card is frozen"}},
	}}

	w := do(newTestHandler(auth, fakeAudit{}, fakePinger{}), http.MethodPost, "/v1/authorizations", validBody)

	c.Equal(http.StatusOK, w.Code, w.Body.String())
	c.Equal("application/json", w.Header().Get("Content-Type"))
	c.Empty(w.Header().Get("Idempotent-Replay"))
	c.Equal("DECLINE", decode(c, w)["decision"])
	c.Equal("card_123", auth.got.CardID)
	c.Equal("5734", auth.got.Merchant.MCC)
}

func TestAuthorizeMarksReplays(t *testing.T) {
	c := require.New(t)
	auth := &fakeAuthorizer{resp: domain.AuthResponse{Decision: domain.Approve}, replayed: true}

	w := do(newTestHandler(auth, fakeAudit{}, fakePinger{}), http.MethodPost, "/v1/authorizations", validBody)

	c.Equal(http.StatusOK, w.Code)
	c.Equal("true", w.Header().Get("Idempotent-Replay"))
}

func TestAuthorizeRejectsInvalidJSON(t *testing.T) {
	c := require.New(t)
	auth := &fakeAuthorizer{}

	w := do(newTestHandler(auth, fakeAudit{}, fakePinger{}), http.MethodPost, "/v1/authorizations", `{"authorization_id":`)

	c.Equal(http.StatusBadRequest, w.Code)
	c.Equal("invalid_json", decode(c, w)["error"])
	c.Empty(auth.got.AuthorizationID, "a rejected request must not reach the authorizer")
}

func TestAuthorizeRejectsABodyThatIsTooLarge(t *testing.T) {
	c := require.New(t)
	auth := &fakeAuthorizer{}
	body := `{"pad":"` + strings.Repeat("x", maxBodyBytes) + `"}`

	w := do(newTestHandler(auth, fakeAudit{}, fakePinger{}), http.MethodPost, "/v1/authorizations", body)

	c.Equal(http.StatusBadRequest, w.Code)
	c.Equal("invalid_json", decode(c, w)["error"])
	c.Empty(auth.got.AuthorizationID, "a rejected request must not reach the authorizer")
}

func TestAuthorizeRejectsARequestThatFailsValidation(t *testing.T) {
	c := require.New(t)
	auth := &fakeAuthorizer{}

	w := do(newTestHandler(auth, fakeAudit{}, fakePinger{}), http.MethodPost, "/v1/authorizations", `{"authorization_id":"auth_1"}`)

	c.Equal(http.StatusBadRequest, w.Code)
	body := decode(c, w)
	c.Equal("invalid_request", body["error"])
	fields, ok := body["fields"].(map[string]any)
	c.True(ok, "fields must be an object: %v", body)
	c.Equal("required", fields["card_id"])
	c.Empty(auth.got.AuthorizationID, "a rejected request must not reach the authorizer")
}

func TestAuthorizeHidesInternalErrors(t *testing.T) {
	c := require.New(t)
	auth := &fakeAuthorizer{err: errors.New("pq: connection refused at 10.0.0.5")}

	w := do(newTestHandler(auth, fakeAudit{}, fakePinger{}), http.MethodPost, "/v1/authorizations", validBody)

	c.Equal(http.StatusInternalServerError, w.Code)
	c.Equal("internal", decode(c, w)["error"])
	c.NotContains(w.Body.String(), "10.0.0.5")
}

func TestGetAuthorizationReturnsTheAuditRecord(t *testing.T) {
	c := require.New(t)
	audit := fakeAudit{rec: domain.AuditRecord{
		AuthorizationID: "auth_1", Status: "pending",
		Request: json.RawMessage(`{}`), Response: json.RawMessage(`{}`),
	}}

	w := do(newTestHandler(&fakeAuthorizer{}, audit, fakePinger{}), http.MethodGet, "/v1/authorizations/auth_1", "")

	c.Equal(http.StatusOK, w.Code)
	c.Equal("pending", decode(c, w)["status"])
}

func TestGetAuthorizationReturns404WhenMissing(t *testing.T) {
	c := require.New(t)

	w := do(newTestHandler(&fakeAuthorizer{}, fakeAudit{err: domain.ErrNotFound}, fakePinger{}), http.MethodGet, "/v1/authorizations/auth_1", "")

	c.Equal(http.StatusNotFound, w.Code)
	c.Equal("not_found", decode(c, w)["error"])
}

func TestGetAuthorizationReturns500OnStoreError(t *testing.T) {
	c := require.New(t)

	w := do(newTestHandler(&fakeAuthorizer{}, fakeAudit{err: errors.New("boom")}, fakePinger{}), http.MethodGet, "/v1/authorizations/auth_1", "")

	c.Equal(http.StatusInternalServerError, w.Code)
	c.Equal("internal", decode(c, w)["error"])
}

func TestHealthIsOKWhenTheDatabaseIsUp(t *testing.T) {
	c := require.New(t)

	w := do(newTestHandler(&fakeAuthorizer{}, fakeAudit{}, fakePinger{}), http.MethodGet, "/healthz", "")

	c.Equal(http.StatusOK, w.Code)
	c.Equal("ok", decode(c, w)["status"])
}

func TestHealthIsUnavailableWhenTheDatabaseIsDown(t *testing.T) {
	c := require.New(t)

	w := do(newTestHandler(&fakeAuthorizer{}, fakeAudit{}, fakePinger{errors.New("down")}), http.MethodGet, "/healthz", "")

	c.Equal(http.StatusServiceUnavailable, w.Code)
	c.Equal("unavailable", decode(c, w)["status"])
}

func TestUnknownRouteReturns404(t *testing.T) {
	c := require.New(t)

	w := do(newTestHandler(&fakeAuthorizer{}, fakeAudit{}, fakePinger{}), http.MethodGet, "/v1/nope", "")

	c.Equal(http.StatusNotFound, w.Code)
}

func TestWrongMethodReturns405(t *testing.T) {
	c := require.New(t)

	w := do(newTestHandler(&fakeAuthorizer{}, fakeAudit{}, fakePinger{}), http.MethodDelete, "/v1/authorizations", "")

	c.Equal(http.StatusMethodNotAllowed, w.Code)
}
