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
	"github.com/bescobarm/card-auth-engine/internal/policy"
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

type fakePublisher struct {
	err error
	got policy.Document
}

func (f *fakePublisher) Publish(_ context.Context, doc policy.Document) (policy.Document, error) {
	f.got = doc
	doc.Version = 4
	return doc, f.err
}

type fakePinger struct{ err error }

func (f fakePinger) Ping(context.Context) error { return f.err }

const validBody = `{"authorization_id":"auth_1","card_id":"card_123","amount_minor":4500,"currency":"USD",
"merchant":{"id":"m_1","name":"ACME","mcc":"5734","country":"US"},"entry_mode":"ecommerce"}`

func newTestHandler(auth Authorizer, audit AuditReader, db Pinger) http.Handler {
	return NewHandler(Deps{Auth: auth, Audit: audit, Policies: &fakePublisher{}, DB: db}, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func do(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
	return w
}

func decodeError(c *require.Assertions, w *httptest.ResponseRecorder) apiError {
	var body apiError
	c.NoError(json.Unmarshal(w.Body.Bytes(), &body), "body is not an API error: %q", w.Body.String())

	return body
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
	c.EqualError(errMalformedRequest, decodeError(c, w).Error)
	c.Empty(auth.got.AuthorizationID, "a rejected request must not reach the authorizer")
}

func TestAuthorizeRejectsABodyThatIsTooLarge(t *testing.T) {
	c := require.New(t)
	auth := &fakeAuthorizer{}
	body := `{"pad":"` + strings.Repeat("x", maxBodyBytes) + `"}`

	w := do(newTestHandler(auth, fakeAudit{}, fakePinger{}), http.MethodPost, "/v1/authorizations", body)

	c.Equal(http.StatusRequestEntityTooLarge, w.Code)
	c.EqualError(errBodyTooLarge, decodeError(c, w).Error)
	c.Empty(auth.got.AuthorizationID, "a rejected request must not reach the authorizer")
}

func TestAuthorizeRejectsARequestThatFailsValidation(t *testing.T) {
	c := require.New(t)
	auth := &fakeAuthorizer{}

	w := do(newTestHandler(auth, fakeAudit{}, fakePinger{}), http.MethodPost, "/v1/authorizations", `{"authorization_id":"auth_1"}`)

	c.Equal(http.StatusBadRequest, w.Code)
	body := decodeError(c, w)
	c.EqualError(errInvalidRequest, body.Error)
	c.Equal("required", body.Fields["card_id"])
	c.Empty(auth.got.AuthorizationID, "a rejected request must not reach the authorizer")
}

func TestAuthorizeHidesInternalErrors(t *testing.T) {
	c := require.New(t)
	auth := &fakeAuthorizer{err: errors.New("pq: connection refused at 10.0.0.5")}

	w := do(newTestHandler(auth, fakeAudit{}, fakePinger{}), http.MethodPost, "/v1/authorizations", validBody)

	c.Equal(http.StatusInternalServerError, w.Code)
	c.EqualError(errAuthorizeFailed, decodeError(c, w).Error)
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
	c.EqualError(errAuthorizationNotFound, decodeError(c, w).Error)
}

func TestGetAuthorizationReturns500OnStoreError(t *testing.T) {
	c := require.New(t)

	w := do(newTestHandler(&fakeAuthorizer{}, fakeAudit{err: errors.New("boom")}, fakePinger{}), http.MethodGet, "/v1/authorizations/auth_1", "")

	c.Equal(http.StatusInternalServerError, w.Code)
	c.EqualError(errAuditUnavailable, decodeError(c, w).Error)
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

const validPolicy = `{"timezone":"America/Mexico_City","controls":{"blocked_mccs":["7995"],"allowed_entry_modes":["chip"]}}`

func newPolicyHandler(p PolicyPublisher) http.Handler {
	return NewHandler(Deps{Auth: &fakeAuthorizer{}, Audit: fakeAudit{}, Policies: p, DB: fakePinger{}}, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestPublishPolicyReturnsTheNewVersion(t *testing.T) {
	c := require.New(t)
	pub := &fakePublisher{}

	w := do(newPolicyHandler(pub), http.MethodPut, "/v1/companies/acme-mx/policy", validPolicy)

	c.Equal(http.StatusCreated, w.Code, w.Body.String())
	body := decode(c, w)
	c.Equal("acme-mx", body["company_id"])
	c.EqualValues(4, body["version"])
	c.Equal("acme-mx", pub.got.CompanyID, "company comes from the URL")
	c.Equal([]string{"7995"}, pub.got.Controls.BlockedMCCs)
}

func TestPublishPolicyRejectsInvalidJSON(t *testing.T) {
	c := require.New(t)

	w := do(newPolicyHandler(&fakePublisher{}), http.MethodPut, "/v1/companies/acme-mx/policy", `{"controls":`)

	c.Equal(http.StatusBadRequest, w.Code)
	c.EqualError(errMalformedPolicy, decodeError(c, w).Error)
}

func TestPublishPolicyRejectsABodyThatIsTooLarge(t *testing.T) {
	c := require.New(t)
	body := `{"pad":"` + strings.Repeat("x", maxBodyBytes) + `"}`

	w := do(newPolicyHandler(&fakePublisher{}), http.MethodPut, "/v1/companies/acme-mx/policy", body)

	c.Equal(http.StatusRequestEntityTooLarge, w.Code)
	c.EqualError(errBodyTooLarge, decodeError(c, w).Error)
}

func TestPublishPolicyRejectsUnknownFields(t *testing.T) {
	c := require.New(t)

	w := do(newPolicyHandler(&fakePublisher{}), http.MethodPut, "/v1/companies/acme-mx/policy", `{"controls":{"blocked_mcc":["7995"]}}`)

	c.Equal(http.StatusBadRequest, w.Code)
	body := decodeError(c, w)
	c.EqualError(errMalformedPolicy, body.Error)
	c.Contains(body.Detail, "blocked_mcc")
}

func TestPublishPolicyListsInvalidFields(t *testing.T) {
	c := require.New(t)
	pub := &fakePublisher{}

	w := do(newPolicyHandler(pub), http.MethodPut, "/v1/companies/acme-mx/policy", `{"controls":{"allowed_entry_modes":["chip","telepathy"]}}`)

	c.Equal(http.StatusBadRequest, w.Code)
	body := decodeError(c, w)
	c.EqualError(errInvalidPolicy, body.Error)
	c.Equal("unknown entry mode", body.Fields["controls.allowed_entry_modes[1]"])
	c.Empty(pub.got.CompanyID, "an invalid policy must not be published")
}

func TestPublishPolicyRejectsAnotherCompanysID(t *testing.T) {
	c := require.New(t)

	w := do(newPolicyHandler(&fakePublisher{}), http.MethodPut, "/v1/companies/acme-mx/policy", `{"company_id":"other"}`)

	c.Equal(http.StatusBadRequest, w.Code)
	c.EqualError(errCompanyMismatch, decodeError(c, w).Error)
}

func TestPublishPolicyReturns409OnAConcurrentPublish(t *testing.T) {
	c := require.New(t)

	w := do(newPolicyHandler(&fakePublisher{err: domain.ErrConflict}), http.MethodPut, "/v1/companies/acme-mx/policy", validPolicy)

	c.Equal(http.StatusConflict, w.Code)
	c.EqualError(errPolicyConflict, decodeError(c, w).Error)
}

func TestPublishPolicyHidesStoreErrors(t *testing.T) {
	c := require.New(t)

	w := do(newPolicyHandler(&fakePublisher{err: errors.New("pq: disk full")}), http.MethodPut, "/v1/companies/acme-mx/policy", validPolicy)

	c.Equal(http.StatusInternalServerError, w.Code)
	c.EqualError(errPublishFailed, decodeError(c, w).Error)
	c.NotContains(w.Body.String(), "disk full")
}

func TestWriteJSONLogsAValueThatCannotBeEncoded(t *testing.T) {
	c := require.New(t)
	var logs strings.Builder
	h := &handler{log: slog.New(slog.NewTextHandler(&logs, nil))}
	w := httptest.NewRecorder()

	h.writeJSON(w, http.StatusOK, func() {})

	c.Equal(http.StatusOK, w.Code)
	c.Contains(logs.String(), "write response failed")
}
