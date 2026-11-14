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

func (f fakePinger) Ping(context.Context) error {
	return f.err
}

const validBody = `{"authorization_id":"auth_1","card_id":"card_123","amount_minor":4500,"currency":"USD",
"merchant":{"id":"m_1","name":"ACME","mcc":"5734","country":"US"},"entry_mode":"ecommerce"}`

func newTestHandler(auth Authorizer, audit AuditReader, db Pinger) http.Handler {
	return NewHandler(Deps{Auth: auth, Audit: audit, Policies: &fakePublisher{}, DB: db}, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func doRequest(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, httptest.NewRequest(method, path, strings.NewReader(body)))
	return recorder
}

func decodeError(c *require.Assertions, w *httptest.ResponseRecorder) apiError {
	var body apiError
	c.NoError(json.Unmarshal(w.Body.Bytes(), &body), "body is not an API error: %q", w.Body.String())

	return body
}

func decodeBody(c *require.Assertions, w *httptest.ResponseRecorder) map[string]any {
	var body map[string]any
	c.NoError(json.Unmarshal(w.Body.Bytes(), &body), "body is not JSON: %q", w.Body.String())
	return body
}

func TestAuthorizeReturnsTheDecision(t *testing.T) {
	c := require.New(t)
	auth := &fakeAuthorizer{resp: domain.AuthResponse{
		AuthorizationID: "auth_1", Decision: domain.Decline,
		Reasons: []domain.Reason{{Code: domain.ReasonCardInactive, Message: "Card is frozen"}},
	}}

	recorder := doRequest(newTestHandler(auth, fakeAudit{}, fakePinger{}), http.MethodPost, "/v1/authorizations", validBody)

	c.Equal(http.StatusOK, recorder.Code, recorder.Body.String())
	c.Equal("application/json", recorder.Header().Get("Content-Type"))
	c.Empty(recorder.Header().Get("Idempotent-Replay"))
	c.Equal("DECLINE", decodeBody(c, recorder)["decision"])
	c.Equal("card_123", auth.got.CardID)
	c.Equal("5734", auth.got.Merchant.MCC)
}

func TestAuthorizeMarksReplays(t *testing.T) {
	c := require.New(t)
	auth := &fakeAuthorizer{resp: domain.AuthResponse{Decision: domain.Approve}, replayed: true}

	recorder := doRequest(newTestHandler(auth, fakeAudit{}, fakePinger{}), http.MethodPost, "/v1/authorizations", validBody)

	c.Equal(http.StatusOK, recorder.Code)
	c.Equal("true", recorder.Header().Get("Idempotent-Replay"))
}

func TestAuthorizeRejectsInvalidJSON(t *testing.T) {
	c := require.New(t)
	auth := &fakeAuthorizer{}

	recorder := doRequest(newTestHandler(auth, fakeAudit{}, fakePinger{}), http.MethodPost, "/v1/authorizations", `{"authorization_id":`)

	c.Equal(http.StatusBadRequest, recorder.Code)
	c.EqualError(errMalformedRequest, decodeError(c, recorder).Error)
	c.Empty(auth.got.AuthorizationID, "a rejected request must not reach the authorizer")
}

func TestAuthorizeRejectsABodyThatIsTooLarge(t *testing.T) {
	c := require.New(t)
	auth := &fakeAuthorizer{}
	body := `{"pad":"` + strings.Repeat("x", maxBodyBytes) + `"}`

	recorder := doRequest(newTestHandler(auth, fakeAudit{}, fakePinger{}), http.MethodPost, "/v1/authorizations", body)

	c.Equal(http.StatusRequestEntityTooLarge, recorder.Code)
	c.EqualError(errBodyTooLarge, decodeError(c, recorder).Error)
	c.Empty(auth.got.AuthorizationID, "a rejected request must not reach the authorizer")
}

func TestAuthorizeRejectsARequestThatFailsValidation(t *testing.T) {
	c := require.New(t)
	auth := &fakeAuthorizer{}

	recorder := doRequest(newTestHandler(auth, fakeAudit{}, fakePinger{}), http.MethodPost, "/v1/authorizations", `{"authorization_id":"auth_1"}`)

	c.Equal(http.StatusBadRequest, recorder.Code)
	body := decodeError(c, recorder)
	c.EqualError(errInvalidRequest, body.Error)
	c.Equal("required", body.Fields["card_id"])
	c.Empty(auth.got.AuthorizationID, "a rejected request must not reach the authorizer")
}

func TestAuthorizeHidesInternalErrors(t *testing.T) {
	c := require.New(t)
	auth := &fakeAuthorizer{err: errors.New("pq: connection refused at 10.0.0.5")}

	recorder := doRequest(newTestHandler(auth, fakeAudit{}, fakePinger{}), http.MethodPost, "/v1/authorizations", validBody)

	c.Equal(http.StatusInternalServerError, recorder.Code)
	c.EqualError(errAuthorizeFailed, decodeError(c, recorder).Error)
	c.NotContains(recorder.Body.String(), "10.0.0.5")
}

func TestGetAuthorizationReturnsTheAuditRecord(t *testing.T) {
	c := require.New(t)
	audit := fakeAudit{rec: domain.AuditRecord{
		AuthorizationID: "auth_1", Status: "pending",
		Request: json.RawMessage(`{}`), Response: json.RawMessage(`{}`),
	}}

	recorder := doRequest(newTestHandler(&fakeAuthorizer{}, audit, fakePinger{}), http.MethodGet, "/v1/authorizations/auth_1", "")

	c.Equal(http.StatusOK, recorder.Code)
	c.Equal("pending", decodeBody(c, recorder)["status"])
}

func TestGetAuthorizationReturns404WhenMissing(t *testing.T) {
	c := require.New(t)

	recorder := doRequest(newTestHandler(&fakeAuthorizer{}, fakeAudit{err: domain.ErrNotFound}, fakePinger{}), http.MethodGet, "/v1/authorizations/auth_1", "")

	c.Equal(http.StatusNotFound, recorder.Code)
	c.EqualError(errAuthorizationNotFound, decodeError(c, recorder).Error)
}

func TestGetAuthorizationReturns500OnStoreError(t *testing.T) {
	c := require.New(t)

	recorder := doRequest(newTestHandler(&fakeAuthorizer{}, fakeAudit{err: errors.New("boom")}, fakePinger{}), http.MethodGet, "/v1/authorizations/auth_1", "")

	c.Equal(http.StatusInternalServerError, recorder.Code)
	c.EqualError(errAuditUnavailable, decodeError(c, recorder).Error)
}

func TestHealthIsOKWhenTheDatabaseIsUp(t *testing.T) {
	c := require.New(t)

	recorder := doRequest(newTestHandler(&fakeAuthorizer{}, fakeAudit{}, fakePinger{}), http.MethodGet, "/healthz", "")

	c.Equal(http.StatusOK, recorder.Code)
	c.Equal("ok", decodeBody(c, recorder)["status"])
}

func TestHealthIsUnavailableWhenTheDatabaseIsDown(t *testing.T) {
	c := require.New(t)

	recorder := doRequest(newTestHandler(&fakeAuthorizer{}, fakeAudit{}, fakePinger{errors.New("down")}), http.MethodGet, "/healthz", "")

	c.Equal(http.StatusServiceUnavailable, recorder.Code)
	c.Equal("unavailable", decodeBody(c, recorder)["status"])
}

func TestUnknownRouteReturns404(t *testing.T) {
	c := require.New(t)

	recorder := doRequest(newTestHandler(&fakeAuthorizer{}, fakeAudit{}, fakePinger{}), http.MethodGet, "/v1/nope", "")

	c.Equal(http.StatusNotFound, recorder.Code)
}

func TestWrongMethodReturns405(t *testing.T) {
	c := require.New(t)

	recorder := doRequest(newTestHandler(&fakeAuthorizer{}, fakeAudit{}, fakePinger{}), http.MethodDelete, "/v1/authorizations", "")

	c.Equal(http.StatusMethodNotAllowed, recorder.Code)
}

const validPolicy = `{"timezone":"America/Mexico_City","controls":{"blocked_mccs":["7995"],"allowed_entry_modes":["chip"]}}`

func newPolicyHandler(p PolicyPublisher) http.Handler {
	return NewHandler(Deps{Auth: &fakeAuthorizer{}, Audit: fakeAudit{}, Policies: p, DB: fakePinger{}}, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestPublishPolicyReturnsTheNewVersion(t *testing.T) {
	c := require.New(t)
	pub := &fakePublisher{}

	recorder := doRequest(newPolicyHandler(pub), http.MethodPut, "/v1/companies/acme-mx/policy", validPolicy)

	c.Equal(http.StatusCreated, recorder.Code, recorder.Body.String())
	body := decodeBody(c, recorder)
	c.Equal("acme-mx", body["company_id"])
	c.EqualValues(4, body["version"])
	c.Equal("acme-mx", pub.got.CompanyID, "company comes from the URL")
	c.Equal([]string{"7995"}, pub.got.Controls.BlockedMCCs)
}

func TestPublishPolicyRejectsInvalidJSON(t *testing.T) {
	c := require.New(t)

	recorder := doRequest(newPolicyHandler(&fakePublisher{}), http.MethodPut, "/v1/companies/acme-mx/policy", `{"controls":`)

	c.Equal(http.StatusBadRequest, recorder.Code)
	c.EqualError(errMalformedPolicy, decodeError(c, recorder).Error)
}

func TestPublishPolicyRejectsABodyThatIsTooLarge(t *testing.T) {
	c := require.New(t)
	body := `{"pad":"` + strings.Repeat("x", maxBodyBytes) + `"}`

	recorder := doRequest(newPolicyHandler(&fakePublisher{}), http.MethodPut, "/v1/companies/acme-mx/policy", body)

	c.Equal(http.StatusRequestEntityTooLarge, recorder.Code)
	c.EqualError(errBodyTooLarge, decodeError(c, recorder).Error)
}

func TestPublishPolicyRejectsUnknownFields(t *testing.T) {
	c := require.New(t)

	recorder := doRequest(newPolicyHandler(&fakePublisher{}), http.MethodPut, "/v1/companies/acme-mx/policy", `{"controls":{"blocked_mcc":["7995"]}}`)

	c.Equal(http.StatusBadRequest, recorder.Code)
	body := decodeError(c, recorder)
	c.EqualError(errMalformedPolicy, body.Error)
	c.Contains(body.Detail, "blocked_mcc")
}

func TestPublishPolicyListsInvalidFields(t *testing.T) {
	c := require.New(t)
	pub := &fakePublisher{}

	recorder := doRequest(newPolicyHandler(pub), http.MethodPut, "/v1/companies/acme-mx/policy", `{"controls":{"allowed_entry_modes":["chip","telepathy"]}}`)

	c.Equal(http.StatusBadRequest, recorder.Code)
	body := decodeError(c, recorder)
	c.EqualError(errInvalidPolicy, body.Error)
	c.Equal("unknown entry mode", body.Fields["controls.allowed_entry_modes[1]"])
	c.Empty(pub.got.CompanyID, "an invalid policy must not be published")
}

func TestPublishPolicyRejectsAnotherCompanysID(t *testing.T) {
	c := require.New(t)

	recorder := doRequest(newPolicyHandler(&fakePublisher{}), http.MethodPut, "/v1/companies/acme-mx/policy", `{"company_id":"other"}`)

	c.Equal(http.StatusBadRequest, recorder.Code)
	c.EqualError(errCompanyMismatch, decodeError(c, recorder).Error)
}

func TestPublishPolicyReturns409OnAConcurrentPublish(t *testing.T) {
	c := require.New(t)

	recorder := doRequest(newPolicyHandler(&fakePublisher{err: domain.ErrConflict}), http.MethodPut, "/v1/companies/acme-mx/policy", validPolicy)

	c.Equal(http.StatusConflict, recorder.Code)
	c.EqualError(errPolicyConflict, decodeError(c, recorder).Error)
}

func TestPublishPolicyHidesStoreErrors(t *testing.T) {
	c := require.New(t)

	recorder := doRequest(newPolicyHandler(&fakePublisher{err: errors.New("pq: disk full")}), http.MethodPut, "/v1/companies/acme-mx/policy", validPolicy)

	c.Equal(http.StatusInternalServerError, recorder.Code)
	c.EqualError(errPublishFailed, decodeError(c, recorder).Error)
	c.NotContains(recorder.Body.String(), "disk full")
}

func TestWriteJSONLogsAValueThatCannotBeEncoded(t *testing.T) {
	c := require.New(t)
	var logs strings.Builder
	routes := &handler{log: slog.New(slog.NewTextHandler(&logs, nil))}
	recorder := httptest.NewRecorder()

	routes.writeJSON(recorder, http.StatusOK, func() {})

	c.Equal(http.StatusOK, recorder.Code)
	c.Contains(logs.String(), "write response failed")
}
