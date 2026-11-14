package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bescobarm/card-auth-engine/internal/domain"
	"github.com/bescobarm/card-auth-engine/internal/policy"
)

var (
	errRedisDown = errors.New("redis down")
	errDBDown    = errors.New("db down")
)

// fakeStore runs apply on a fixed hold, like the real store does under a row lock.
type fakeStore struct {
	hold    Hold
	readErr error
}

func (f fakeStore) ApplyEvent(ctx context.Context, _ Event, apply ApplyFunc) (Result, bool, error) {
	res, err := apply(ctx, f.hold)
	return res, false, err
}

func (f fakeStore) GetHold(context.Context, string) (Hold, error) { return f.hold, f.readErr }

type fakeHolds struct {
	limit     Limit
	err       error
	adjusts   []Adjust
	increases []Increase
}

func (f *fakeHolds) Adjust(_ context.Context, a Adjust) error {
	f.adjusts = append(f.adjusts, a)
	return f.err
}

func (f *fakeHolds) Increase(_ context.Context, inc Increase) (Limit, error) {
	f.increases = append(f.increases, inc)
	return f.limit, f.err
}

type fakeCards struct {
	card domain.Card
	err  error
}

func (f fakeCards) GetCard(context.Context, string) (domain.Card, error) { return f.card, f.err }

type fakePolicies struct {
	doc policy.Document
	err error
}

func (f fakePolicies) Policy(context.Context, string) (policy.Document, error) { return f.doc, f.err }

var activeCard = domain.Card{ID: "card_123", CompanyID: "acme-mx", Status: domain.CardActive}

func newTestService(h Hold, holds *fakeHolds, cards fakeCards, policies fakePolicies) *Service {
	st := fakeStore{hold: h}
	s := NewService(st, st, holds, cards, policies)
	s.now = func() time.Time { return time.Date(2026, 11, 13, 18, 0, 0, 0, time.UTC) }
	return s
}

func limitPolicy(t *testing.T, amountMinor int64) policy.Document {
	t.Helper()
	doc := policy.Document{CompanyID: "acme-mx", Controls: policy.Controls{
		MaxPerTransaction: &policy.Money{AmountMinor: amountMinor, Currency: "USD"},
	}}
	require.Empty(t, doc.Validate())
	return doc
}

func TestApplyAdjustsTheBudgetInRedis(t *testing.T) {
	c := require.New(t)
	holds := &fakeHolds{}

	res, _, err := newTestService(pending(400), holds, fakeCards{}, fakePolicies{}).Apply(context.Background(),
		Event{ID: "e1", AuthorizationID: "a1", Type: TypeReversal, AmountMinor: amount(150)})

	c.NoError(err)
	c.Equal(int64(250), res.HeldMinor)
	c.Equal([]Adjust{{CardID: "card_123", AuthorizationID: "a1", EventID: "e1", DeltaMinor: -150}}, holds.adjusts)
}

func TestApplySkipsRedisWhenNothingChanges(t *testing.T) {
	c := require.New(t)
	holds := &fakeHolds{}

	_, _, err := newTestService(pending(400), holds, fakeCards{}, fakePolicies{}).Apply(context.Background(),
		Event{ID: "e1", Type: TypeCapture, AmountMinor: amount(400)})

	c.NoError(err)
	c.Empty(holds.adjusts)
}

func TestApplyDoesNotTouchRedisWhenTheTransitionIsRefused(t *testing.T) {
	c := require.New(t)
	holds := &fakeHolds{}

	_, _, err := newTestService(pending(100), holds, fakeCards{}, fakePolicies{}).Apply(context.Background(),
		Event{ID: "e1", Type: TypeReversal, AmountMinor: amount(500)})

	c.ErrorIs(err, ErrOverHold)
	c.Empty(holds.adjusts)
}

func TestApplyFailsWhenRedisFails(t *testing.T) {
	c := require.New(t)

	_, _, err := newTestService(pending(400), &fakeHolds{err: errRedisDown}, fakeCards{}, fakePolicies{}).Apply(context.Background(),
		Event{ID: "e1", Type: TypeExpiry})

	c.ErrorIs(err, errRedisDown)
}

func TestIncrementWithinTheLimitsIsApproved(t *testing.T) {
	c := require.New(t)
	holds := &fakeHolds{}
	doc := limitPolicy(t, 50000)

	res, _, err := newTestService(pending(30000), holds, fakeCards{card: activeCard}, fakePolicies{doc: doc}).Apply(context.Background(),
		Event{ID: "e1", AuthorizationID: "a1", Type: TypeIncrement, AmountMinor: amount(15000)})

	c.NoError(err)
	c.Equal(domain.Approve, res.Decision)
	c.Equal(int64(45000), res.HeldMinor)
	c.Len(holds.increases, 1)
	c.Equal(Increase{CardID: "card_123", AuthorizationID: "a1", EventID: "e1", AmountMinor: 15000, TotalMinor: 45000,
		Currency: "USD", MCC: "7011", Now: time.Date(2026, 11, 13, 18, 0, 0, 0, time.UTC), Policy: doc}, holds.increases[0])
}

func TestIncrementOverThePerTransactionLimitIsDeclined(t *testing.T) {
	c := require.New(t)
	holds := &fakeHolds{}

	res, _, err := newTestService(pending(30000), holds, fakeCards{card: activeCard}, fakePolicies{doc: limitPolicy(t, 40000)}).Apply(
		context.Background(), Event{ID: "e1", Type: TypeIncrement, AmountMinor: amount(15000)})

	c.NoError(err)
	c.Equal(domain.Decline, res.Decision)
	c.Equal(domain.ReasonOverTransactionLimit, res.Reasons[0].Code)
	c.Equal(int64(30000), res.HeldMinor, "a declined increment keeps the hold")
	c.Empty(holds.increases)
}

func TestIncrementOnAFrozenCardIsDeclined(t *testing.T) {
	c := require.New(t)
	frozen := activeCard
	frozen.Status = domain.CardFrozen

	res, _, err := newTestService(pending(300), &fakeHolds{}, fakeCards{card: frozen}, fakePolicies{err: domain.ErrNotFound}).Apply(
		context.Background(), Event{ID: "e1", Type: TypeIncrement, AmountMinor: amount(100)})

	c.NoError(err)
	c.Equal(domain.ReasonCardInactive, res.Reasons[0].Code)
}

func TestIncrementOverABudgetIsDeclined(t *testing.T) {
	c := require.New(t)
	holds := &fakeHolds{limit: Limit{Exceeded: true, Code: domain.ReasonOverDailyLimit, Message: "over"}}

	res, _, err := newTestService(pending(300), holds, fakeCards{card: activeCard}, fakePolicies{}).Apply(context.Background(),
		Event{ID: "e1", Type: TypeIncrement, AmountMinor: amount(100)})

	c.NoError(err)
	c.Equal(domain.Decline, res.Decision)
	c.Equal([]domain.Reason{{Code: domain.ReasonOverDailyLimit, Message: "over"}}, res.Reasons)
	c.Equal(int64(300), res.HeldMinor)
}

func TestIncrementFailsWhenRedisFails(t *testing.T) {
	c := require.New(t)

	_, _, err := newTestService(pending(300), &fakeHolds{err: errRedisDown}, fakeCards{card: activeCard}, fakePolicies{}).Apply(
		context.Background(), Event{ID: "e1", Type: TypeIncrement, AmountMinor: amount(100)})

	c.ErrorIs(err, errRedisDown)
}

func TestIncrementOnAnUnknownAuthorizationIsNotFound(t *testing.T) {
	c := require.New(t)
	st := fakeStore{readErr: domain.ErrNotFound}

	_, _, err := NewService(st, st, &fakeHolds{}, fakeCards{}, fakePolicies{}).Apply(context.Background(),
		Event{ID: "e1", Type: TypeIncrement, AmountMinor: amount(100)})

	c.ErrorIs(err, domain.ErrNotFound)
}

func TestIncrementFailsWhenTheCardCannotBeLoaded(t *testing.T) {
	c := require.New(t)

	_, _, err := newTestService(pending(300), &fakeHolds{}, fakeCards{err: errDBDown}, fakePolicies{}).Apply(context.Background(),
		Event{ID: "e1", Type: TypeIncrement, AmountMinor: amount(100)})

	c.ErrorIs(err, errDBDown)
}

func TestIncrementFailsWhenThePolicyCannotBeLoaded(t *testing.T) {
	c := require.New(t)

	_, _, err := newTestService(pending(300), &fakeHolds{}, fakeCards{card: activeCard}, fakePolicies{err: errDBDown}).Apply(
		context.Background(), Event{ID: "e1", Type: TypeIncrement, AmountMinor: amount(100)})

	c.ErrorIs(err, errDBDown)
}

func TestIncrementFailsOnInvalidCardOverrides(t *testing.T) {
	c := require.New(t)
	card := activeCard
	card.Overrides = json.RawMessage(`{"blocked_mcc": ["7995"]}`)

	_, _, err := newTestService(pending(300), &fakeHolds{}, fakeCards{card: card}, fakePolicies{}).Apply(context.Background(),
		Event{ID: "e1", Type: TypeIncrement, AmountMinor: amount(100)})

	c.ErrorContains(err, "card_123")
}

func TestIncrementOnACapturedAuthorizationIsRefused(t *testing.T) {
	c := require.New(t)

	_, _, err := newTestService(captured(300, 0), &fakeHolds{}, fakeCards{card: activeCard}, fakePolicies{}).Apply(context.Background(),
		Event{ID: "e1", Type: TypeIncrement, AmountMinor: amount(100)})

	c.ErrorIs(err, ErrNotPending)
}
