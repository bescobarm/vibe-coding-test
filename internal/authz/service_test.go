package authz

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bescobarm/card-auth-engine/internal/domain"
	"github.com/bescobarm/card-auth-engine/internal/pipeline"
)

type fakeRecorder struct {
	replay  *domain.AuthResponse
	decided int
}

func (f *fakeRecorder) Decide(ctx context.Context, _ domain.AuthRequest, decide DecideFunc) (domain.AuthResponse, bool, error) {
	if f.replay != nil {
		return *f.replay, true, nil
	}
	f.decided++
	resp, err := decide(ctx)
	return resp, false, err
}

type fakeCards struct {
	card domain.Card
	err  error
}

func (f fakeCards) GetCard(context.Context, string) (domain.Card, error) { return f.card, f.err }

type fakePolicies struct {
	version int
	err     error
}

func (f fakePolicies) LatestPolicyVersion(context.Context, string) (int, error) {
	return f.version, f.err
}

type failingStage struct{ err error }

func (failingStage) Name() string { return "failing" }

func (s failingStage) Evaluate(context.Context, *pipeline.Evaluation) error { return s.err }

type cardFoundStage struct{ seen *bool }

func (cardFoundStage) Name() string { return "card_found" }

func (s cardFoundStage) Evaluate(_ context.Context, e *pipeline.Evaluation) error {
	*s.seen = e.CardFound
	if !e.CardFound {
		e.Decline(domain.ReasonCardInactive, "Card not found")
	}
	return nil
}

var (
	activeCard = domain.Card{ID: "card_123", CompanyID: "acme-mx", Status: domain.CardActive}
	errBoom    = errors.New("boom")
)

func req() domain.AuthRequest {
	return domain.AuthRequest{AuthorizationID: "auth_1", CardID: "card_123", AmountMinor: 4500}
}

// Each call to the clock advances it by 1.5 ms, so latency is deterministic.
func steppingClock() func() time.Time {
	t := time.Date(2026, 11, 12, 15, 0, 0, 0, time.UTC)
	return func() time.Time {
		now := t
		t = t.Add(1500 * time.Microsecond)
		return now
	}
}

func TestAuthorizeApproves(t *testing.T) {
	c := require.New(t)
	var seen bool
	svc := NewService(&fakeRecorder{}, fakeCards{card: activeCard}, fakePolicies{version: 3}, pipeline.New(cardFoundStage{&seen}))
	svc.now = steppingClock()

	resp, replayed, err := svc.Authorize(context.Background(), req())

	c.NoError(err)
	c.False(replayed)
	c.True(seen, "pipeline did not see the loaded card")
	c.Equal(domain.AuthResponse{
		AuthorizationID: "auth_1", Decision: domain.Approve, ApprovedAmountMinor: 4500,
		Reasons: []domain.Reason{}, PolicyVersion: 3, LatencyMS: 1.5,
	}, resp)
}

func TestAuthorizeUnknownCardSkipsPolicyAndReachesThePipeline(t *testing.T) {
	c := require.New(t)
	seen := true
	policies := fakePolicies{err: errors.New("must not be called")}
	svc := NewService(&fakeRecorder{}, fakeCards{err: domain.ErrNotFound}, policies, pipeline.New(cardFoundStage{&seen}))

	resp, _, err := svc.Authorize(context.Background(), req())

	c.NoError(err)
	c.False(seen)
	c.Equal(domain.Decline, resp.Decision)
	c.Zero(resp.PolicyVersion)
}

func TestAuthorizeReturnsReplayWithoutEvaluating(t *testing.T) {
	c := require.New(t)
	stored := domain.AuthResponse{AuthorizationID: "auth_1", Decision: domain.Decline}
	rec := &fakeRecorder{replay: &stored}
	svc := NewService(rec, fakeCards{card: activeCard}, fakePolicies{}, pipeline.New(failingStage{errors.New("must not run")}))

	resp, replayed, err := svc.Authorize(context.Background(), req())

	c.NoError(err)
	c.True(replayed)
	c.Equal(stored, resp)
	c.Zero(rec.decided)
}

func TestAuthorizeFailsWhenCardLookupFails(t *testing.T) {
	c := require.New(t)
	rec := &fakeRecorder{}
	svc := NewService(rec, fakeCards{err: errBoom}, fakePolicies{}, pipeline.New(failingStage{}))

	_, _, err := svc.Authorize(context.Background(), req())

	c.ErrorIs(err, errBoom)
	c.Zero(rec.decided, "nothing must be claimed when loading fails")
}

func TestAuthorizeFailsWhenPolicyLookupFails(t *testing.T) {
	c := require.New(t)
	rec := &fakeRecorder{}
	svc := NewService(rec, fakeCards{card: activeCard}, fakePolicies{err: errBoom}, pipeline.New(failingStage{}))

	_, _, err := svc.Authorize(context.Background(), req())

	c.ErrorIs(err, errBoom)
	c.Zero(rec.decided, "nothing must be claimed when loading fails")
}

func TestAuthorizeFailsWhenAStageFails(t *testing.T) {
	c := require.New(t)
	rec := &fakeRecorder{}
	svc := NewService(rec, fakeCards{card: activeCard}, fakePolicies{version: 1}, pipeline.New(failingStage{errBoom}))

	_, _, err := svc.Authorize(context.Background(), req())

	c.ErrorIs(err, errBoom)
	c.Equal(1, rec.decided)
}
