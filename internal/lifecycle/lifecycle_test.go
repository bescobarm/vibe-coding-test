package lifecycle

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

var errRedisDown = errors.New("redis down")

func amount(v int64) *int64 { return &v }

func pending(held int64) Hold {
	return Hold{AuthorizationID: "a1", CardID: "card_123", Status: StatusPending, HeldMinor: held}
}

func TestFullReversalReleasesEverything(t *testing.T) {
	c := require.New(t)

	res, err := Transition(pending(400), Event{ID: "e1", Type: TypeReversal})

	c.NoError(err)
	c.Equal(Result{AuthorizationID: "a1", EventID: "e1", Type: TypeReversal, Status: StatusReversed, ReleasedMinor: 400, HeldMinor: 0}, res)
}

func TestPartialReversalKeepsTheAuthorizationPending(t *testing.T) {
	c := require.New(t)

	res, err := Transition(pending(400), Event{ID: "e1", Type: TypeReversal, AmountMinor: amount(150)})

	c.NoError(err)
	c.Equal(StatusPending, res.Status)
	c.Equal(int64(150), res.ReleasedMinor)
	c.Equal(int64(250), res.HeldMinor)
}

func TestPartialReversalOfTheWholeHoldReverses(t *testing.T) {
	c := require.New(t)

	res, err := Transition(pending(400), Event{ID: "e1", Type: TypeReversal, AmountMinor: amount(400)})

	c.NoError(err)
	c.Equal(StatusReversed, res.Status)
}

func TestExpiryReleasesEverything(t *testing.T) {
	c := require.New(t)

	res, err := Transition(pending(400), Event{ID: "e1", Type: TypeExpiry})

	c.NoError(err)
	c.Equal(StatusExpired, res.Status)
	c.Equal(int64(400), res.ReleasedMinor)
}

func TestReversalOverTheHoldIsRefused(t *testing.T) {
	c := require.New(t)

	_, err := Transition(pending(400), Event{ID: "e1", Type: TypeReversal, AmountMinor: amount(401)})

	c.ErrorIs(err, ErrOverHold)
}

func TestADeclinedAuthorizationHasNothingToRelease(t *testing.T) {
	c := require.New(t)
	h := pending(0)
	h.Status = StatusDeclined

	_, err := Transition(h, Event{ID: "e1", Type: TypeReversal})

	c.ErrorIs(err, ErrDeclined)
}

func TestAReversedAuthorizationCannotChange(t *testing.T) {
	c := require.New(t)
	h := pending(0)
	h.Status = StatusReversed

	_, err := Transition(h, Event{ID: "e1", Type: TypeExpiry})

	c.ErrorIs(err, ErrNotPending)
}

func TestSameComparesEveryField(t *testing.T) {
	c := require.New(t)
	ev := Event{ID: "e1", AuthorizationID: "a1", Type: TypeReversal, AmountMinor: amount(100)}

	c.True(ev.Same(Event{ID: "e1", AuthorizationID: "a1", Type: TypeReversal, AmountMinor: amount(100)}))
	c.True(Event{ID: "e1", Type: TypeExpiry}.Same(Event{ID: "e1", Type: TypeExpiry}))
	c.False(ev.Same(Event{ID: "e1", AuthorizationID: "a1", Type: TypeReversal, AmountMinor: amount(200)}))
	c.False(ev.Same(Event{ID: "e1", AuthorizationID: "a1", Type: TypeReversal}))
	c.False(ev.Same(Event{ID: "e1", AuthorizationID: "a2", Type: TypeReversal, AmountMinor: amount(100)}))
	c.False(ev.Same(Event{ID: "e1", AuthorizationID: "a1", Type: TypeExpiry, AmountMinor: amount(100)}))
}

// fakeStore runs apply on a fixed hold, like the real store does under a row lock.
type fakeStore struct{ hold Hold }

func (f fakeStore) ApplyEvent(ctx context.Context, _ Event, apply ApplyFunc) (Result, bool, error) {
	res, err := apply(ctx, f.hold)
	return res, false, err
}

type fakeReleaser struct {
	err error
	got []Release
}

func (f *fakeReleaser) Release(_ context.Context, r Release) error {
	f.got = append(f.got, r)
	return f.err
}

func TestApplyReleasesTheAmountInRedis(t *testing.T) {
	c := require.New(t)
	r := &fakeReleaser{}

	res, _, err := NewService(fakeStore{hold: pending(400)}, r).Apply(context.Background(),
		Event{ID: "e1", AuthorizationID: "a1", Type: TypeReversal, AmountMinor: amount(150)})

	c.NoError(err)
	c.Equal(int64(250), res.HeldMinor)
	c.Equal([]Release{{CardID: "card_123", AuthorizationID: "a1", EventID: "e1", AmountMinor: 150}}, r.got)
}

func TestApplyDoesNotTouchRedisWhenTheTransitionIsRefused(t *testing.T) {
	c := require.New(t)
	r := &fakeReleaser{}

	_, _, err := NewService(fakeStore{hold: pending(100)}, r).Apply(context.Background(),
		Event{ID: "e1", Type: TypeReversal, AmountMinor: amount(500)})

	c.ErrorIs(err, ErrOverHold)
	c.Empty(r.got)
}

func TestApplyFailsWhenRedisFails(t *testing.T) {
	c := require.New(t)

	_, _, err := NewService(fakeStore{hold: pending(400)}, &fakeReleaser{err: errRedisDown}).Apply(context.Background(),
		Event{ID: "e1", Type: TypeExpiry})

	c.ErrorIs(err, errRedisDown)
}
