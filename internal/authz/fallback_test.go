package authz

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bescobarm/card-auth-engine/internal/domain"
	"github.com/bescobarm/card-auth-engine/internal/pipeline"
	"github.com/bescobarm/card-auth-engine/internal/policy"
)

// slowStage stands for a stage that ignores the deadline, e.g. stuck in CPU.
type slowStage struct{ d time.Duration }

func (slowStage) GetName() string {
	return "slow"
}

func (s slowStage) Evaluate(context.Context, *pipeline.Evaluation) error {
	time.Sleep(s.d)
	return nil
}

func newJSONLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, nil))
}

// AC3 and AC7 of SP05 (D-036, D-064): a 2 s stage, a 500 ms deadline. The
// answer is the fallback at exactly 500 ms, and it is stored once the slow
// decision ends (D-031), so a retry replays it (AC6).
func TestTheDeadlineAnswersTheFallbackOnTime(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := require.New(t)
		rec := &fakeRecorder{}
		svc := NewService(rec, fakeCards{card: activeCard}, fakePolicies{doc: newFailOpenPolicy(10000)}, pipeline.New(slowStage{2 * time.Second}))
		start := time.Now()

		resp, _, err := svc.Authorize(context.Background(), newRequest())

		c.NoError(err)
		c.Equal(500*time.Millisecond, time.Since(start))
		c.True(resp.FallbackUsed)
		c.Equal(domain.Approve, resp.Decision)
		c.Equal(500.0, resp.LatencyMS)
		c.Empty(rec.storedResponses(), "the slow decision still holds the claim")

		c.NoError(svc.Wait(context.Background()))
		c.Equal([]domain.AuthResponse{resp}, rec.storedResponses())
	})
}

// If the slow decision managed to commit anyway, the processor already got the
// fallback: the mismatch is logged loudly for reconciliation.
func TestADecisionCommittedAfterTheDeadlineIsLogged(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := require.New(t)
		var logs bytes.Buffer
		rec := &fakeRecorder{ignoreCtx: true}
		svc := NewService(rec, fakeCards{card: activeCard}, fakePolicies{doc: newFailOpenPolicy(10000)}, pipeline.New(slowStage{time.Second})).
			WithLogger(newJSONLogger(&logs))

		_, _, err := svc.Authorize(context.Background(), newRequest())
		c.NoError(err)
		c.NoError(svc.Wait(context.Background()))

		c.Contains(logs.String(), "decision committed after the deadline")
		c.Len(rec.storedResponses(), 1, "only the late decision; the fallback is not stored over it")
	})
}

func TestAFallbackIsNotStoredWhileTheLateDecisionKeepsRunning(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := require.New(t)
		var logs bytes.Buffer
		rec := &fakeRecorder{}
		svc := NewService(rec, fakeCards{card: activeCard}, fakePolicies{doc: newFailOpenPolicy(10000)}, pipeline.New(slowStage{time.Minute})).
			WithLogger(newJSONLogger(&logs))

		_, _, err := svc.Authorize(context.Background(), newRequest())
		c.NoError(err)
		c.NoError(svc.Wait(context.Background()))

		c.Contains(logs.String(), "fallback not stored: the late decision is still running")
		c.Empty(rec.storedResponses())
	})
}

func TestWaitGivesUpWhenItsContextEnds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := require.New(t)
		svc := NewService(&fakeRecorder{}, fakeCards{card: activeCard}, fakePolicies{}, pipeline.New(slowStage{time.Minute}))
		_, _, err := svc.Authorize(context.Background(), newRequest())
		c.NoError(err)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		err = svc.Wait(ctx)

		c.ErrorIs(err, context.DeadlineExceeded)
		c.NoError(svc.Wait(context.Background()), "the bubble ends only when the background work does")
	})
}

// AC4 of SP05 (D-062): with Postgres down nothing can be stored; the engine
// still answers, and a structured log line records the decision.
func TestWithPostgresDownTheFallbackIsLoggedInsteadOfStored(t *testing.T) {
	c := require.New(t)
	var logs bytes.Buffer
	svc := NewService(&fakeRecorder{down: errBoom}, fakeCards{err: errBoom}, fakePolicies{}, pipeline.New()).WithLogger(newJSONLogger(&logs))

	resp, _, err := svc.Authorize(context.Background(), newRequest())

	c.NoError(err)
	c.Equal(domain.Decline, resp.Decision)
	c.True(resp.FallbackUsed)
	c.Contains(logs.String(), `"msg":"fallback not stored","authorization_id":"auth_1","decision":"DECLINE","card_id":"card_123"`)
}

// A retry that falls back finds the first answer stored: that answer wins.
func TestAStoredAnswerWinsOverANewFallback(t *testing.T) {
	c := require.New(t)
	stored := domain.AuthResponse{AuthorizationID: "auth_1", Decision: domain.Approve, FallbackUsed: true}
	svc := NewService(&fakeRecorder{replay: &stored}, fakeCards{err: errBoom}, fakePolicies{}, pipeline.New())

	resp, _, err := svc.Authorize(context.Background(), newRequest())

	c.NoError(err)
	c.Equal(stored, resp)
}

func TestFallbackFailsClosedWithoutAFallbackSection(t *testing.T) {
	c := require.New(t)

	resp := BuildFallback(newRequest(), policy.Document{CompanyID: "acme-mx"}, 2)

	c.Equal(domain.AuthResponse{AuthorizationID: "auth_1", Decision: domain.Decline, PolicyVersion: 2, FallbackUsed: true,
		Reasons: []domain.Reason{{Code: domain.ReasonFallbackApplied, Message: msgFailClosed}}}, resp)
}

// AC2 of SP05: fail-open declines over the cap.
func TestFallbackDeclinesOverTheCap(t *testing.T) {
	c := require.New(t)

	resp := BuildFallback(newRequest(), newFailOpenPolicy(4499), 3)

	c.Equal(domain.Decline, resp.Decision)
	c.Zero(resp.ApprovedAmountMinor)
	c.Equal("Fallback fail_open_limited: amount 4500 is over 4499 USD", resp.Reasons[0].Message)
}

func TestFallbackApprovesExactlyAtTheCap(t *testing.T) {
	c := require.New(t)

	resp := BuildFallback(newRequest(), newFailOpenPolicy(4500), 3)

	c.Equal(domain.Approve, resp.Decision)
}

// D-061: a cap in another currency declines.
func TestFallbackDeclinesAnotherCurrency(t *testing.T) {
	c := require.New(t)
	request := newRequest()
	request.Currency = "MXN"

	resp := BuildFallback(request, newFailOpenPolicy(10000), 3)

	c.Equal(domain.Decline, resp.Decision)
	c.Equal("Fallback fail_open_limited: limit is in USD, request is in MXN", resp.Reasons[0].Message)
}

func TestFallbackUnderTheDefaultPolicySaysSo(t *testing.T) {
	c := require.New(t)

	resp := BuildFallback(newRequest(), policy.GetDefault(), 0)

	c.Equal([]string{domain.ReasonFallbackApplied, domain.ReasonDefaultPolicy}, listReasonCodes(resp))
	c.Equal(msgDefaultUnknown, resp.Reasons[1].Message)
}

func TestWithDeadlineChangesTheDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := require.New(t)
		svc := NewService(&fakeRecorder{}, fakeCards{card: activeCard}, fakePolicies{}, pipeline.New(slowStage{time.Second})).
			WithDeadline(100 * time.Millisecond)
		start := time.Now()

		_, _, err := svc.Authorize(context.Background(), newRequest())

		c.NoError(err)
		c.Equal(100*time.Millisecond, time.Since(start))
		c.NoError(svc.Wait(context.Background()))
	})
}

// Without a signal reader the score is almost blind; that is logged once, not per request.
func TestAMissingSignalReaderIsLoggedOnce(t *testing.T) {
	c := require.New(t)
	var logs bytes.Buffer
	svc := NewService(&fakeRecorder{}, fakeCards{card: activeCard}, fakePolicies{}, pipeline.New()).WithLogger(newJSONLogger(&logs))

	for range 3 {
		_, _, err := svc.Authorize(context.Background(), newRequest())
		c.NoError(err)
	}

	c.Equal(1, strings.Count(logs.String(), msgNoSignals))
}
