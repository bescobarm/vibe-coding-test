package lifecycle

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func newAmount(v int64) *int64 {
	return &v
}

func newPendingHold(held int64) Hold {
	return Hold{AuthorizationID: "a1", CardID: "card_123", Currency: "USD", MCC: "7011", Status: StatusPending, HeldMinor: held}
}

func newCapturedHold(capturedMinor, refunded int64) Hold {
	hold := newPendingHold(0)
	hold.Status, hold.CapturedMinor, hold.RefundedMinor = StatusCaptured, capturedMinor, refunded
	return hold
}

func TestFullReversalReleasesEverything(t *testing.T) {
	c := require.New(t)

	res, delta, err := ComputeTransition(newPendingHold(400), Event{ID: "e1", Type: TypeReversal})

	c.NoError(err)
	c.Equal(Result{AuthorizationID: "a1", EventID: "e1", Type: TypeReversal, Status: StatusReversed}, res)
	c.Equal(int64(-400), delta)
}

func TestPartialReversalKeepsTheAuthorizationPending(t *testing.T) {
	c := require.New(t)

	res, delta, err := ComputeTransition(newPendingHold(400), Event{ID: "e1", Type: TypeReversal, AmountMinor: newAmount(150)})

	c.NoError(err)
	c.Equal(StatusPending, res.Status)
	c.Equal(int64(250), res.HeldMinor)
	c.Equal(int64(-150), delta)
}

func TestPartialReversalOfTheWholeHoldReverses(t *testing.T) {
	c := require.New(t)

	res, _, err := ComputeTransition(newPendingHold(400), Event{ID: "e1", Type: TypeReversal, AmountMinor: newAmount(400)})

	c.NoError(err)
	c.Equal(StatusReversed, res.Status)
}

func TestExpiryReleasesEverything(t *testing.T) {
	c := require.New(t)

	res, delta, err := ComputeTransition(newPendingHold(400), Event{ID: "e1", Type: TypeExpiry})

	c.NoError(err)
	c.Equal(StatusExpired, res.Status)
	c.Equal(int64(-400), delta)
}

func TestReversalOverTheHoldIsRefused(t *testing.T) {
	c := require.New(t)

	_, _, err := ComputeTransition(newPendingHold(400), Event{ID: "e1", Type: TypeReversal, AmountMinor: newAmount(401)})

	c.ErrorIs(err, ErrOverHold)
}

func TestADeclinedAuthorizationHasNothingToRelease(t *testing.T) {
	c := require.New(t)
	hold := newPendingHold(0)
	hold.Status = StatusDeclined

	_, _, err := ComputeTransition(hold, Event{ID: "e1", Type: TypeReversal})

	c.ErrorIs(err, ErrDeclined)
}

func TestAReversedAuthorizationCannotChange(t *testing.T) {
	c := require.New(t)
	hold := newPendingHold(0)
	hold.Status = StatusReversed

	_, _, err := ComputeTransition(hold, Event{ID: "e1", Type: TypeExpiry})

	c.ErrorIs(err, ErrNotPending)
}

func TestIncrementRaisesTheHold(t *testing.T) {
	c := require.New(t)

	res, delta, err := ComputeTransition(newPendingHold(300), Event{ID: "e1", Type: TypeIncrement, AmountMinor: newAmount(150)})

	c.NoError(err)
	c.Equal("APPROVE", string(res.Decision))
	c.Equal(int64(450), res.HeldMinor)
	c.Equal(int64(150), delta)
}

func TestIncrementNeedsAPendingAuthorization(t *testing.T) {
	c := require.New(t)

	_, _, err := ComputeTransition(newCapturedHold(300, 0), Event{ID: "e1", Type: TypeIncrement, AmountMinor: newAmount(150)})

	c.ErrorIs(err, ErrNotPending)
}

// D-053: a capture above the hold is accepted and counts the captured amount.
func TestCaptureAboveTheHoldCountsTheCapturedAmount(t *testing.T) {
	c := require.New(t)

	res, delta, err := ComputeTransition(newPendingHold(400), Event{ID: "e1", Type: TypeCapture, AmountMinor: newAmount(460)})

	c.NoError(err)
	c.Equal(StatusCaptured, res.Status)
	c.Equal(int64(460), res.CapturedMinor)
	c.Zero(res.HeldMinor)
	c.Equal(int64(60), delta)
}

func TestCaptureBelowTheHoldReleasesTheDifference(t *testing.T) {
	c := require.New(t)

	_, delta, err := ComputeTransition(newPendingHold(400), Event{ID: "e1", Type: TypeCapture, AmountMinor: newAmount(250)})

	c.NoError(err)
	c.Equal(int64(-150), delta)
}

func TestCaptureNeedsAPendingAuthorization(t *testing.T) {
	c := require.New(t)

	_, _, err := ComputeTransition(newCapturedHold(300, 0), Event{ID: "e1", Type: TypeCapture, AmountMinor: newAmount(300)})

	c.ErrorIs(err, ErrNotPending)
}

func TestPartialRefundKeepsTheAuthorizationCaptured(t *testing.T) {
	c := require.New(t)

	res, delta, err := ComputeTransition(newCapturedHold(460, 0), Event{ID: "e1", Type: TypeRefund, AmountMinor: newAmount(60)})

	c.NoError(err)
	c.Equal(StatusCaptured, res.Status)
	c.Equal(int64(60), res.RefundedMinor)
	c.Equal(int64(460), res.CapturedMinor)
	c.Equal(int64(-60), delta)
}

func TestFullRefundRefundsWhatIsLeft(t *testing.T) {
	c := require.New(t)

	res, delta, err := ComputeTransition(newCapturedHold(460, 60), Event{ID: "e1", Type: TypeRefund})

	c.NoError(err)
	c.Equal(StatusRefunded, res.Status)
	c.Equal(int64(460), res.RefundedMinor)
	c.Equal(int64(-400), delta)
}

func TestRefundOverWhatIsLeftIsRefused(t *testing.T) {
	c := require.New(t)

	_, _, err := ComputeTransition(newCapturedHold(460, 60), Event{ID: "e1", Type: TypeRefund, AmountMinor: newAmount(401)})

	c.ErrorIs(err, ErrOverRefund)
}

func TestRefundNeedsACapture(t *testing.T) {
	c := require.New(t)

	_, _, err := ComputeTransition(newPendingHold(400), Event{ID: "e1", Type: TypeRefund})

	c.ErrorIs(err, ErrNotCaptured)
}

func TestUnknownEventTypeIsRefused(t *testing.T) {
	c := require.New(t)

	_, _, err := ComputeTransition(newPendingHold(400), Event{ID: "e1", Type: "chargeback"})

	c.ErrorIs(err, ErrUnknownType)
}

func TestSameComparesEveryField(t *testing.T) {
	c := require.New(t)
	ev := Event{ID: "e1", AuthorizationID: "a1", Type: TypeReversal, AmountMinor: newAmount(100)}

	c.True(ev.IsSame(Event{ID: "e1", AuthorizationID: "a1", Type: TypeReversal, AmountMinor: newAmount(100)}))
	c.True(Event{ID: "e1", Type: TypeExpiry}.IsSame(Event{ID: "e1", Type: TypeExpiry}))
	c.False(ev.IsSame(Event{ID: "e1", AuthorizationID: "a1", Type: TypeReversal, AmountMinor: newAmount(200)}))
	c.False(ev.IsSame(Event{ID: "e1", AuthorizationID: "a1", Type: TypeReversal}))
	c.False(ev.IsSame(Event{ID: "e1", AuthorizationID: "a2", Type: TypeReversal, AmountMinor: newAmount(100)}))
	c.False(ev.IsSame(Event{ID: "e1", AuthorizationID: "a1", Type: TypeExpiry, AmountMinor: newAmount(100)}))
}

func TestAPendingAuthorizationCountsItsHold(t *testing.T) {
	c := require.New(t)

	c.Equal(int64(400), newPendingHold(400).GetCountedMinor())
}

func TestACapturedAuthorizationCountsWhatWasNotRefunded(t *testing.T) {
	c := require.New(t)

	c.Equal(int64(400), newCapturedHold(460, 60).GetCountedMinor())
}

func TestARefundedAuthorizationCountsNothing(t *testing.T) {
	c := require.New(t)
	hold := newCapturedHold(460, 460)
	hold.Status = StatusRefunded

	c.Zero(hold.GetCountedMinor())
}

func TestAReversedAuthorizationCountsNothing(t *testing.T) {
	c := require.New(t)
	hold := newPendingHold(400)
	hold.Status = StatusReversed

	c.Zero(hold.GetCountedMinor())
}
