package api

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bescobarm/card-auth-engine/internal/domain"
	"github.com/bescobarm/card-auth-engine/internal/lifecycle"
)

var errDBDown = errors.New("db down")

type fakeLifecycle struct {
	res      lifecycle.Result
	replayed bool
	err      error
	got      lifecycle.Event
}

func (f *fakeLifecycle) Apply(_ context.Context, ev lifecycle.Event) (lifecycle.Result, bool, error) {
	f.got = ev
	return f.res, f.replayed, f.err
}

func newLifecycleHandler(f *fakeLifecycle) http.Handler {
	return NewHandler(Deps{Lifecycle: f}, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestReversalReturnsTheNewHold(t *testing.T) {
	c := require.New(t)
	f := &fakeLifecycle{res: lifecycle.Result{AuthorizationID: "a1", EventID: "e1", Type: lifecycle.TypeReversal,
		Status: lifecycle.StatusPending, ReleasedMinor: 100, HeldMinor: 300}}

	w := do(newLifecycleHandler(f), http.MethodPost, "/v1/authorizations/a1/reversal", `{"event_id": "e1", "amount_minor": 100}`)

	c.Equal(http.StatusOK, w.Code)
	c.JSONEq(`{"authorization_id": "a1", "event_id": "e1", "type": "reversal", "status": "pending",
		"released_minor": 100, "held_minor": 300}`, w.Body.String())
	c.Equal("a1", f.got.AuthorizationID)
	c.Equal(lifecycle.TypeReversal, f.got.Type)
	c.Equal(int64(100), *f.got.AmountMinor)
	c.Empty(w.Header().Get(headerReplay))
}

func TestExpireMarksAReplay(t *testing.T) {
	c := require.New(t)
	f := &fakeLifecycle{replayed: true}

	w := do(newLifecycleHandler(f), http.MethodPost, "/v1/authorizations/a1/expire", `{"event_id": "e1"}`)

	c.Equal(http.StatusOK, w.Code)
	c.Equal(lifecycle.TypeExpiry, f.got.Type)
	c.Nil(f.got.AmountMinor)
	c.Equal("true", w.Header().Get(headerReplay))
}

func TestLifecycleRejectsMalformedJSON(t *testing.T) {
	c := require.New(t)

	w := do(newLifecycleHandler(&fakeLifecycle{}), http.MethodPost, "/v1/authorizations/a1/reversal", `{"event_id":`)

	c.Equal(http.StatusBadRequest, w.Code)
	c.EqualError(errMalformedEvent, decodeError(c, w).Error)
}

func TestLifecycleRequiresAnEventIDAndAPositiveAmount(t *testing.T) {
	c := require.New(t)

	w := do(newLifecycleHandler(&fakeLifecycle{}), http.MethodPost, "/v1/authorizations/a1/reversal", `{"amount_minor": 0}`)

	body := decodeError(c, w)
	c.Equal(http.StatusBadRequest, w.Code)
	c.EqualError(errInvalidEvent, body.Error)
	c.Equal(map[string]string{"event_id": domain.MsgRequired, fieldAmountMinor: domain.MsgPositive}, body.Fields)
}

func TestExpireRejectsAnAmount(t *testing.T) {
	c := require.New(t)

	w := do(newLifecycleHandler(&fakeLifecycle{}), http.MethodPost, "/v1/authorizations/a1/expire", `{"event_id": "e1", "amount_minor": 5}`)

	c.Equal(http.StatusBadRequest, w.Code)
	c.Equal(map[string]string{fieldAmountMinor: msgAmountNotAllowed}, decodeError(c, w).Fields)
}

func TestLifecycleOnAnUnknownAuthorizationReturns404(t *testing.T) {
	c := require.New(t)

	w := do(newLifecycleHandler(&fakeLifecycle{err: domain.ErrNotFound}), http.MethodPost, "/v1/authorizations/x/expire", `{"event_id": "e1"}`)

	c.Equal(http.StatusNotFound, w.Code)
	c.EqualError(errAuthorizationNotFound, decodeError(c, w).Error)
}

func TestReversalOverTheHoldReturns409(t *testing.T) {
	c := require.New(t)

	w := do(newLifecycleHandler(&fakeLifecycle{err: lifecycle.ErrOverHold}), http.MethodPost, "/v1/authorizations/a1/reversal",
		`{"event_id": "e1", "amount_minor": 999}`)

	c.Equal(http.StatusConflict, w.Code)
	c.EqualError(lifecycle.ErrOverHold, decodeError(c, w).Error)
}

func TestAReusedEventIDReturns409(t *testing.T) {
	c := require.New(t)

	w := do(newLifecycleHandler(&fakeLifecycle{err: lifecycle.ErrEventConflict}), http.MethodPost, "/v1/authorizations/a1/expire", `{"event_id": "e1"}`)

	c.Equal(http.StatusConflict, w.Code)
	c.EqualError(lifecycle.ErrEventConflict, decodeError(c, w).Error)
}

func TestLifecycleFailureReturns500(t *testing.T) {
	c := require.New(t)

	w := do(newLifecycleHandler(&fakeLifecycle{err: errDBDown}), http.MethodPost, "/v1/authorizations/a1/expire", `{"event_id": "e1"}`)

	c.Equal(http.StatusInternalServerError, w.Code)
	c.EqualError(errEventFailed, decodeError(c, w).Error)
}
