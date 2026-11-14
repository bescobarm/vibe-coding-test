package api

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"testing/synctest"
	"time"

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
	events := &fakeLifecycle{res: lifecycle.Result{AuthorizationID: "a1", EventID: "e1", Type: lifecycle.TypeReversal,
		Status: lifecycle.StatusPending, HeldMinor: 300}}

	recorder := doRequest(newLifecycleHandler(events), http.MethodPost, "/v1/authorizations/a1/reversal", `{"event_id": "e1", "amount_minor": 100}`)

	c.Equal(http.StatusOK, recorder.Code)
	c.JSONEq(`{"authorization_id": "a1", "event_id": "e1", "type": "reversal", "status": "pending",
		"held_minor": 300, "captured_minor": 0, "refunded_minor": 0}`, recorder.Body.String())
	c.Equal("a1", events.got.AuthorizationID)
	c.Equal(lifecycle.TypeReversal, events.got.Type)
	c.Equal(int64(100), *events.got.AmountMinor)
	c.Empty(recorder.Header().Get(headerReplay))
}

func TestExpireMarksAReplay(t *testing.T) {
	c := require.New(t)
	events := &fakeLifecycle{replayed: true}

	recorder := doRequest(newLifecycleHandler(events), http.MethodPost, "/v1/authorizations/a1/expire", `{"event_id": "e1"}`)

	c.Equal(http.StatusOK, recorder.Code)
	c.Equal(lifecycle.TypeExpiry, events.got.Type)
	c.Nil(events.got.AmountMinor)
	c.Equal("true", recorder.Header().Get(headerReplay))
}

func TestLifecycleRejectsMalformedJSON(t *testing.T) {
	c := require.New(t)

	recorder := doRequest(newLifecycleHandler(&fakeLifecycle{}), http.MethodPost, "/v1/authorizations/a1/reversal", `{"event_id":`)

	c.Equal(http.StatusBadRequest, recorder.Code)
	c.EqualError(errMalformedEvent, decodeError(c, recorder).Error)
}

func TestLifecycleRequiresAnEventIDAndAPositiveAmount(t *testing.T) {
	c := require.New(t)

	recorder := doRequest(newLifecycleHandler(&fakeLifecycle{}), http.MethodPost, "/v1/authorizations/a1/reversal", `{"amount_minor": 0}`)

	body := decodeError(c, recorder)
	c.Equal(http.StatusBadRequest, recorder.Code)
	c.EqualError(errInvalidEvent, body.Error)
	c.Equal(map[string]string{"event_id": domain.MsgRequired, fieldAmountMinor: domain.MsgPositive}, body.Fields)
}

func TestExpireRejectsAnAmount(t *testing.T) {
	c := require.New(t)

	recorder := doRequest(newLifecycleHandler(&fakeLifecycle{}), http.MethodPost, "/v1/authorizations/a1/expire", `{"event_id": "e1", "amount_minor": 5}`)

	c.Equal(http.StatusBadRequest, recorder.Code)
	c.Equal(map[string]string{fieldAmountMinor: msgAmountNotAllowed}, decodeError(c, recorder).Fields)
}

func TestLifecycleOnAnUnknownAuthorizationReturns404(t *testing.T) {
	c := require.New(t)

	recorder := doRequest(newLifecycleHandler(&fakeLifecycle{err: domain.ErrNotFound}), http.MethodPost, "/v1/authorizations/x/expire", `{"event_id": "e1"}`)

	c.Equal(http.StatusNotFound, recorder.Code)
	c.EqualError(errAuthorizationNotFound, decodeError(c, recorder).Error)
}

func TestReversalOverTheHoldReturns409(t *testing.T) {
	c := require.New(t)

	recorder := doRequest(newLifecycleHandler(&fakeLifecycle{err: lifecycle.ErrOverHold}), http.MethodPost, "/v1/authorizations/a1/reversal",
		`{"event_id": "e1", "amount_minor": 999}`)

	c.Equal(http.StatusConflict, recorder.Code)
	c.EqualError(lifecycle.ErrOverHold, decodeError(c, recorder).Error)
}

func TestAReusedEventIDReturns409(t *testing.T) {
	c := require.New(t)

	recorder := doRequest(newLifecycleHandler(&fakeLifecycle{err: lifecycle.ErrEventConflict}), http.MethodPost, "/v1/authorizations/a1/expire", `{"event_id": "e1"}`)

	c.Equal(http.StatusConflict, recorder.Code)
	c.EqualError(lifecycle.ErrEventConflict, decodeError(c, recorder).Error)
}

func TestLifecycleFailureReturns500(t *testing.T) {
	c := require.New(t)

	recorder := doRequest(newLifecycleHandler(&fakeLifecycle{err: errDBDown}), http.MethodPost, "/v1/authorizations/a1/expire", `{"event_id": "e1"}`)

	c.Equal(http.StatusInternalServerError, recorder.Code)
	c.EqualError(errEventFailed, decodeError(c, recorder).Error)
}

func TestIncrementReturnsTheDecision(t *testing.T) {
	c := require.New(t)
	events := &fakeLifecycle{res: lifecycle.Result{AuthorizationID: "a1", EventID: "e1", Type: lifecycle.TypeIncrement,
		Decision: domain.Decline, Reasons: []domain.Reason{{Code: domain.ReasonOverDailyLimit, Message: "over"}},
		Status: lifecycle.StatusPending, HeldMinor: 450}}

	recorder := doRequest(newLifecycleHandler(events), http.MethodPost, "/v1/authorizations/a1/increment", `{"event_id": "e1", "amount_minor": 100}`)

	c.Equal(http.StatusOK, recorder.Code)
	c.JSONEq(`{"authorization_id": "a1", "event_id": "e1", "type": "increment", "decision": "DECLINE",
		"reasons": [{"code": "OVER_DAILY_LIMIT", "message": "over"}], "status": "pending",
		"held_minor": 450, "captured_minor": 0, "refunded_minor": 0}`, recorder.Body.String())
	c.Equal(lifecycle.TypeIncrement, events.got.Type)
}

func TestIncrementAndCaptureRequireAnAmount(t *testing.T) {
	c := require.New(t)
	routes := newLifecycleHandler(&fakeLifecycle{})

	inc := doRequest(routes, http.MethodPost, "/v1/authorizations/a1/increment", `{"event_id": "e1"}`)
	capture := doRequest(routes, http.MethodPost, "/v1/authorizations/a1/capture", `{"event_id": "e1"}`)

	c.Equal(map[string]string{fieldAmountMinor: domain.MsgRequired}, decodeError(c, inc).Fields)
	c.Equal(map[string]string{fieldAmountMinor: domain.MsgRequired}, decodeError(c, capture).Fields)
}

func TestRefundWithoutAmountRefundsWhatIsLeft(t *testing.T) {
	c := require.New(t)
	events := &fakeLifecycle{}

	recorder := doRequest(newLifecycleHandler(events), http.MethodPost, "/v1/authorizations/a1/refund", `{"event_id": "e1"}`)

	c.Equal(http.StatusOK, recorder.Code)
	c.Equal(lifecycle.TypeRefund, events.got.Type)
	c.Nil(events.got.AmountMinor)
}

func TestRefundBeforeCaptureReturns409(t *testing.T) {
	c := require.New(t)

	recorder := doRequest(newLifecycleHandler(&fakeLifecycle{err: lifecycle.ErrNotCaptured}), http.MethodPost, "/v1/authorizations/a1/refund", `{"event_id": "e1"}`)

	c.Equal(http.StatusConflict, recorder.Code)
	c.EqualError(lifecycle.ErrNotCaptured, decodeError(c, recorder).Error)
}

// slowLifecycle stands for a call stuck behind another transaction's row lock.
type slowLifecycle struct{}

func (slowLifecycle) Apply(ctx context.Context, _ lifecycle.Event) (lifecycle.Result, bool, error) {
	<-ctx.Done()
	return lifecycle.Result{}, false, ctx.Err()
}

// I-030: a lifecycle call stuck on a lock answers 500 at its deadline instead of hanging.
func TestALifecycleCallStuckOnALockAnswersAtItsDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := require.New(t)
		routes := NewHandler(Deps{Lifecycle: slowLifecycle{}}, slog.New(slog.NewTextHandler(io.Discard, nil)))
		start := time.Now()

		recorder := doRequest(routes, http.MethodPost, "/v1/authorizations/a1/expire", `{"event_id": "e1"}`)

		c.Equal(lifecycleDeadline, time.Since(start))
		c.Equal(http.StatusInternalServerError, recorder.Code)
		c.EqualError(errEventFailed, decodeError(c, recorder).Error)
	})
}
