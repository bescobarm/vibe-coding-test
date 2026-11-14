package authz

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bescobarm/card-auth-engine/internal/domain"
	"github.com/bescobarm/card-auth-engine/internal/pipeline"
	"github.com/bescobarm/card-auth-engine/internal/policy"
)

// fakeRecorder behaves like the store: a stored answer is replayed, and a
// decision whose context expired is not stored (pgx fails on a cancelled
// context). down makes every call fail, as when Postgres is down.
type fakeRecorder struct {
	mu        sync.Mutex
	replay    *domain.AuthResponse
	down      error
	ignoreCtx bool
	decided   int
	stored    []domain.AuthResponse
}

func (f *fakeRecorder) Decide(ctx context.Context, _ domain.AuthRequest, decide DecideFunc) (domain.AuthResponse, bool, error) {
	f.mu.Lock()
	replay, down := f.replay, f.down
	f.mu.Unlock()

	if down != nil {
		return domain.AuthResponse{}, false, down
	}

	if replay != nil {
		return *replay, true, nil
	}

	f.mu.Lock()
	f.decided++
	f.mu.Unlock()

	resp, err := decide(ctx)
	if err == nil && !f.ignoreCtx {
		err = ctx.Err()
	}

	if err != nil {
		return domain.AuthResponse{}, false, err
	}

	f.mu.Lock()
	f.stored = append(f.stored, resp)
	f.mu.Unlock()

	return resp, false, nil
}

func (f *fakeRecorder) storedResponses() []domain.AuthResponse {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stored
}

type fakeCards struct {
	card domain.Card
	err  error
}

func (f fakeCards) GetCard(context.Context, string) (domain.Card, error) {
	return f.card, f.err
}

type fakePolicies struct {
	doc policy.Document
	err error
}

func (f fakePolicies) GetPolicy(context.Context, string) (policy.Document, error) {
	return f.doc, f.err
}

// captureStage records the evaluation it sees, so tests can check what was loaded.
type captureStage struct{ got *pipeline.Evaluation }

func (captureStage) GetName() string {
	return "capture"
}

func (s captureStage) Evaluate(_ context.Context, e *pipeline.Evaluation) error {
	*s.got = *e
	return nil
}

type failingStage struct{ err error }

func (failingStage) GetName() string {
	return "failing"
}

func (s failingStage) Evaluate(context.Context, *pipeline.Evaluation) error {
	return s.err
}

type cardFoundStage struct{ seen *bool }

func (cardFoundStage) GetName() string {
	return "card_found"
}

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

func newRequest() domain.AuthRequest {
	return domain.AuthRequest{AuthorizationID: "auth_1", CardID: "card_123", AmountMinor: 4500, Currency: "USD"}
}

// Each call to the clock advances it by 1.5 ms, so latency is deterministic.
func newSteppingClock() func() time.Time {
	current := time.Date(2026, 11, 12, 15, 0, 0, 0, time.UTC)
	return func() time.Time {
		now := current
		current = current.Add(1500 * time.Microsecond)
		return now
	}
}

func TestAuthorizeApproves(t *testing.T) {
	c := require.New(t)
	var seen bool
	svc := NewService(&fakeRecorder{}, fakeCards{card: activeCard}, fakePolicies{doc: policy.Document{Version: 3}}, pipeline.New(cardFoundStage{&seen}))
	svc.now = newSteppingClock()

	resp, replayed, err := svc.Authorize(context.Background(), newRequest())

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

	resp, _, err := svc.Authorize(context.Background(), newRequest())

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

	resp, replayed, err := svc.Authorize(context.Background(), newRequest())

	c.NoError(err)
	c.True(replayed)
	c.Equal(stored, resp)
	c.Zero(rec.decided)
}

func TestAuthorizeFallsBackWhenTheCardCannotBeLoaded(t *testing.T) {
	c := require.New(t)
	rec := &fakeRecorder{}
	svc := NewService(rec, fakeCards{err: errBoom}, fakePolicies{}, pipeline.New(failingStage{}))

	resp, _, err := svc.Authorize(context.Background(), newRequest())

	c.NoError(err)
	c.True(resp.FallbackUsed)
	c.Equal(domain.Decline, resp.Decision, "an unknown company gets the default fail-closed fallback (D-062)")
	c.Equal([]string{domain.ReasonFallbackApplied, domain.ReasonDefaultPolicy}, listReasonCodes(resp))
	c.Equal([]domain.AuthResponse{resp}, rec.storedResponses(), "the fallback is stored")
}

func TestAuthorizeFallsBackWhenThePolicyCannotBeLoaded(t *testing.T) {
	c := require.New(t)
	svc := NewService(&fakeRecorder{}, fakeCards{card: activeCard}, fakePolicies{err: errBoom}, pipeline.New(failingStage{}))

	resp, _, err := svc.Authorize(context.Background(), newRequest())

	c.NoError(err)
	c.True(resp.FallbackUsed)
	c.Equal(domain.Decline, resp.Decision)
}

// AC1 of SP05 with fakes: a failing stage (Redis down) gets the company's fail-open fallback.
func TestAuthorizeFallsBackWhenAStageFails(t *testing.T) {
	c := require.New(t)
	rec := &fakeRecorder{}
	svc := NewService(rec, fakeCards{card: activeCard}, fakePolicies{doc: newFailOpenPolicy(10000)}, pipeline.New(failingStage{errBoom}))

	resp, _, err := svc.Authorize(context.Background(), newRequest())

	c.NoError(err)
	c.Equal(domain.Approve, resp.Decision)
	c.Equal(int64(4500), resp.ApprovedAmountMinor)
	c.Equal(3, resp.PolicyVersion)
	c.Equal([]domain.Reason{{Code: domain.ReasonFallbackApplied, Message: "Fallback fail_open_limited: approved up to 10000 USD"}}, resp.Reasons)
	c.Equal(2, rec.decided, "the failed decision, then the stored fallback")
}

// D-042, D-063: a company without a policy gets the embedded default.
func TestAuthorizeWithoutCompanyPolicyUsesTheDefault(t *testing.T) {
	c := require.New(t)
	var got pipeline.Evaluation
	svc := NewService(&fakeRecorder{}, fakeCards{card: activeCard}, fakePolicies{err: domain.ErrNotFound}, pipeline.New(captureStage{&got}))

	resp, _, err := svc.Authorize(context.Background(), newRequest())

	c.NoError(err)
	c.Equal(domain.Approve, resp.Decision)
	c.Zero(resp.PolicyVersion)
	c.Equal([]string{
		"7995",
		"6051",
		"4829",
	}, got.Controls.BlockedMCCs)
	c.Equal([]string{domain.ReasonDefaultPolicy}, listReasonCodes(resp))
}

func TestAuthorizeAppliesCardOverrides(t *testing.T) {
	c := require.New(t)
	var got pipeline.Evaluation
	card := activeCard
	card.Overrides = []byte(`{"max_per_transaction": {"amount_minor": 100000, "currency": "USD"}}`)
	company := policy.Document{Version: 2, Controls: policy.Controls{
		BlockedMCCs:       []string{"7995"},
		MaxPerTransaction: &policy.Money{AmountMinor: 50000, Currency: "USD"},
	}}
	svc := NewService(&fakeRecorder{}, fakeCards{card: card}, fakePolicies{doc: company}, pipeline.New(captureStage{&got}))

	_, _, err := svc.Authorize(context.Background(), newRequest())

	c.NoError(err)
	c.Equal(int64(100000), got.Controls.MaxPerTransaction.AmountMinor)
	c.Equal([]string{"7995"}, got.Controls.BlockedMCCs)
	c.Equal(2, got.PolicyVersion)
}

func TestAuthorizeFallsBackOnInvalidCardOverrides(t *testing.T) {
	c := require.New(t)
	rec := &fakeRecorder{}
	card := activeCard
	card.Overrides = []byte(`{"blocked_mcc": ["7995"]}`)
	svc := NewService(rec, fakeCards{card: card}, fakePolicies{doc: newFailOpenPolicy(10000)}, pipeline.New(failingStage{}))

	resp, _, err := svc.Authorize(context.Background(), newRequest())

	c.NoError(err)
	c.True(resp.FallbackUsed)
	c.Equal(domain.Approve, resp.Decision, "the company's own fallback applies")
	c.Equal(1, rec.decided, "only the fallback is stored; nothing was claimed before")
}

func TestAuthorizeUsesTheRequestTime(t *testing.T) {
	c := require.New(t)
	var got pipeline.Evaluation
	request := newRequest()
	request.CreatedAt = time.Date(2026, 11, 12, 3, 0, 0, 0, time.UTC)
	svc := NewService(&fakeRecorder{}, fakeCards{card: activeCard}, fakePolicies{}, pipeline.New(captureStage{&got}))
	svc.now = newSteppingClock()

	_, _, err := svc.Authorize(context.Background(), request)

	c.NoError(err)
	c.Equal(request.CreatedAt, got.Now)
}

func TestAuthorizeFallsBackToTheEngineClock(t *testing.T) {
	c := require.New(t)
	var got pipeline.Evaluation
	svc := NewService(&fakeRecorder{}, fakeCards{card: activeCard}, fakePolicies{}, pipeline.New(captureStage{&got}))
	svc.now = newSteppingClock()

	_, _, err := svc.Authorize(context.Background(), newRequest())

	c.NoError(err)
	c.Equal(time.Date(2026, 11, 12, 15, 0, 0, 0, time.UTC), got.Now)
}

type fakeSignals struct {
	sig domain.Signals
	err error
	got []domain.SignalQuery
}

func (f *fakeSignals) GetSignals(_ context.Context, q domain.SignalQuery) (domain.Signals, error) {
	f.got = append(f.got, q)
	return f.sig, f.err
}

func TestAuthorizeLoadsTheSignalsBeforeTheClaim(t *testing.T) {
	c := require.New(t)
	var got pipeline.Evaluation
	signals := &fakeSignals{sig: domain.Signals{Approved: 7, SmallRecent: 2}}
	svc := NewService(&fakeRecorder{}, fakeCards{card: activeCard}, fakePolicies{}, pipeline.New(captureStage{&got})).WithSignals(signals)

	_, _, err := svc.Authorize(context.Background(), newRequest())

	c.NoError(err)
	c.Equal(domain.Signals{Approved: 7, SmallRecent: 2}, got.Signals)
	c.Len(signals.got, 1)
	c.Equal(activeCard.ID, signals.got[0].CardID)
}

func TestAuthorizeSkipsTheSignalsForAnUnknownCard(t *testing.T) {
	c := require.New(t)
	signals := &fakeSignals{}
	svc := NewService(&fakeRecorder{}, fakeCards{err: domain.ErrNotFound}, fakePolicies{}, pipeline.New()).WithSignals(signals)

	_, _, err := svc.Authorize(context.Background(), newRequest())

	c.NoError(err)
	c.Empty(signals.got)
}

func TestAuthorizeFallsBackWhenTheSignalsCannotBeLoaded(t *testing.T) {
	c := require.New(t)
	svc := NewService(&fakeRecorder{}, fakeCards{card: activeCard}, fakePolicies{doc: newFailOpenPolicy(1000)}, pipeline.New(failingStage{})).
		WithSignals(&fakeSignals{err: errBoom})

	resp, _, err := svc.Authorize(context.Background(), newRequest())

	c.NoError(err)
	c.True(resp.FallbackUsed)
	c.Equal(domain.Decline, resp.Decision, "4500 is over the 1000 cap")
}

func newFailOpenPolicy(maxMinor int64) policy.Document {
	return policy.Document{CompanyID: "acme-mx", Version: 3, Fallback: &policy.Fallback{
		Mode: policy.FailOpenLimited, MaxAmount: &policy.Money{AmountMinor: maxMinor, Currency: "USD"},
	}}
}

func listReasonCodes(resp domain.AuthResponse) []string {
	out := []string{}
	for _, reason := range resp.Reasons {
		out = append(out, reason.Code)
	}

	return out
}
