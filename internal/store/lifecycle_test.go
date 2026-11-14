package store_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bescobarm/card-auth-engine/internal/domain"
	"github.com/bescobarm/card-auth-engine/internal/lifecycle"
)

const (
	budget500   = `[{"id": "daily-spend", "window": "calendar_day", "max_amount": {"amount_minor": 500, "currency": "USD"}}]`
	cardTesting = `[{"id": "card-testing", "window": "10m", "max_count": 5, "filter": {"max_amount_minor": 500}}]`
)

func newAmount(v int64) *int64 {
	return &v
}

func (f fixture) reverse(name, event string, amt *int64) (lifecycle.Result, bool, error) {
	return f.events.Apply(f.ctx, lifecycle.Event{ID: f.buildID(event), AuthorizationID: f.buildID(name), Type: lifecycle.TypeReversal, AmountMinor: amt})
}

func (f fixture) applyEvent(name, event, eventType string, amt *int64) lifecycle.Result {
	f.t.Helper()
	res, _, err := f.events.Apply(f.ctx, lifecycle.Event{ID: f.buildID(event), AuthorizationID: f.buildID(name), Type: eventType, AmountMinor: amt})
	require.NoError(f.t, err)
	return res
}

func (f fixture) readStored(name string) (string, int64) {
	f.t.Helper()
	var (
		status string
		held   int64
	)

	err := f.pool.QueryRow(f.ctx, `SELECT status, held_minor FROM authorizations WHERE authorization_id = $1`, f.buildID(name)).Scan(&status, &held)
	require.NoError(f.t, err)
	return status, held
}

func (f fixture) readRedisHold(name string) string {
	return f.rdb.HGet(f.ctx, "hold:{"+f.card+"}:"+f.buildID(name), "amount").Val()
}

// AC2 of SP03: a reversed hold gives its budget back.
func TestReversalReleasesTheBudgetForTheNextRequest(t *testing.T) {
	c := require.New(t)
	harness := newFixture(t, budget500)
	c.Equal(domain.Approve, harness.authorize("a1", 400).Decision)
	c.Equal(domain.Decline, harness.authorize("a2", 450).Decision)

	res, replayed, err := harness.reverse("a1", "e1", nil)

	c.NoError(err)
	c.False(replayed)
	c.Equal(lifecycle.Result{AuthorizationID: harness.buildID("a1"), EventID: harness.buildID("e1"), Type: lifecycle.TypeReversal,
		Status: lifecycle.StatusReversed, HeldMinor: 0}, res)
	c.Equal(domain.Approve, harness.authorize("a3", 450).Decision)
}

// AC4 of SP03: the same event twice releases once.
func TestRepeatedEventReleasesOnce(t *testing.T) {
	c := require.New(t)
	harness := newFixture(t, budget500)
	harness.authorize("a1", 400)

	first, _, err := harness.reverse("a1", "e1", newAmount(100))
	c.NoError(err)
	again, replayed, err := harness.reverse("a1", "e1", newAmount(100))

	c.NoError(err)
	c.True(replayed)
	c.Equal(first, again)
	status, held := harness.readStored("a1")
	c.Equal(lifecycle.StatusPending, status)
	c.Equal(int64(300), held)
	c.Equal("300", harness.readRedisHold("a1"))
}

// AC7 of SP03 (D-051): reversed card-testing charges still count.
func TestReversedCardTestingChargesStillCount(t *testing.T) {
	c := require.New(t)
	harness := newFixture(t, cardTesting)
	for i := range 5 {
		name := fmt.Sprintf("small%d", i)
		c.Equal(domain.Approve, harness.authorize(name, 300).Decision)
		_, _, err := harness.reverse(name, "rev"+name, nil)
		c.NoError(err)
	}

	resp := harness.authorize("small6", 300)

	c.Equal(domain.Decline, resp.Decision)
	c.Equal(domain.ReasonVelocityExceeded, resp.Reasons[0].Code)
}

func TestExpiryReleasesTheWholeHold(t *testing.T) {
	c := require.New(t)
	harness := newFixture(t, budget500)
	harness.authorize("a1", 400)

	res, _, err := harness.events.Apply(harness.ctx, lifecycle.Event{ID: harness.buildID("e1"), AuthorizationID: harness.buildID("a1"), Type: lifecycle.TypeExpiry})

	c.NoError(err)
	c.Equal(lifecycle.StatusExpired, res.Status)
	c.Equal("0", harness.readRedisHold("a1"))

	var topics int
	err = harness.pool.QueryRow(harness.ctx, `SELECT count(*) FROM outbox WHERE topic = 'authorization.lifecycle' AND payload->>'event_id' = $1`, harness.buildID("e1")).Scan(&topics)
	c.NoError(err)
	c.Equal(1, topics)
}

func TestEventIDReusedForAnotherCallIsAConflict(t *testing.T) {
	c := require.New(t)
	harness := newFixture(t, budget500)
	harness.authorize("a1", 400)
	_, _, err := harness.reverse("a1", "e1", newAmount(100))
	c.NoError(err)

	_, _, err = harness.reverse("a1", "e1", newAmount(200))

	c.ErrorIs(err, lifecycle.ErrEventConflict)
}

func TestARefusedEventIsNotStored(t *testing.T) {
	c := require.New(t)
	harness := newFixture(t, budget500)
	harness.authorize("a1", 400)

	_, _, err := harness.reverse("a1", "e1", newAmount(401))
	c.ErrorIs(err, lifecycle.ErrOverHold)
	_, replayed, err := harness.reverse("a1", "e1", nil)

	c.NoError(err)
	c.False(replayed, "the refused call must not have stored e1")
}

func TestAnEventOnAnUnknownAuthorizationIsNotFound(t *testing.T) {
	c := require.New(t)
	harness := newFixture(t, budget500)

	_, _, err := harness.reverse("missing", "e1", nil)

	c.ErrorIs(err, domain.ErrNotFound)
}

func TestADeclinedAuthorizationCannotBeReversed(t *testing.T) {
	c := require.New(t)
	harness := newFixture(t, budget500)
	harness.authorize("a1", 600)

	_, _, err := harness.reverse("a1", "e1", nil)

	c.ErrorIs(err, lifecycle.ErrDeclined)
}

// Parallel partial reversals with different event_ids never release more than the hold.
func TestConcurrentPartialReversalsNeverReleaseMoreThanTheHold(t *testing.T) {
	c := require.New(t)
	harness := newFixture(t, budget500)
	harness.authorize("a1", 400)

	const parallel = 10
	var wg sync.WaitGroup
	errs := make([]error, parallel)
	for i := range parallel {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, errs[i] = harness.reverse("a1", fmt.Sprintf("e%d", i), newAmount(100))
		}()
	}

	wg.Wait()

	applied := 0
	for i := range parallel {
		if errs[i] == nil {
			applied++
			continue
		}

		c.ErrorIs(errs[i], lifecycle.ErrNotPending)
	}

	status, held := harness.readStored("a1")
	c.Equal(4, applied)
	c.Equal(lifecycle.StatusReversed, status)
	c.Zero(held)
	c.Equal("0", harness.readRedisHold("a1"))
}

// Parallel copies of one event are applied once; the others replay it.
func TestConcurrentCopiesOfAnEventApplyOnce(t *testing.T) {
	c := require.New(t)
	harness := newFixture(t, budget500)
	harness.authorize("a1", 400)

	const parallel = 10
	var wg sync.WaitGroup
	replays := make([]bool, parallel)
	errs := make([]error, parallel)
	for i := range parallel {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, replays[i], errs[i] = harness.reverse("a1", "e1", newAmount(100))
		}()
	}

	wg.Wait()

	fresh := 0
	for i := range parallel {
		c.NoError(errs[i], "call %d", i)
		if !replays[i] {
			fresh++
		}
	}

	_, held := harness.readStored("a1")
	c.Equal(1, fresh)
	c.Equal(int64(300), held)
	c.Equal("300", harness.readRedisHold("a1"))
}

// AC3 of SP03, the hotel scenario: auth 300 with a 500 daily limit, +150
// approved, +100 more declined OVER_DAILY_LIMIT.
func TestHotelIncrementsStopAtTheDailyLimit(t *testing.T) {
	c := require.New(t)
	harness := newFixture(t, budget500)
	c.Equal(domain.Approve, harness.authorize("hotel", 300).Decision)

	first := harness.applyEvent("hotel", "inc1", lifecycle.TypeIncrement, newAmount(150))
	second := harness.applyEvent("hotel", "inc2", lifecycle.TypeIncrement, newAmount(100))

	c.Equal(domain.Approve, first.Decision)
	c.Equal(int64(450), first.HeldMinor)
	c.Equal(domain.Decline, second.Decision)
	c.Equal(domain.ReasonOverDailyLimit, second.Reasons[0].Code)
	_, held := harness.readStored("hotel")
	c.Equal(int64(450), held)
}

// A declined increment is stored: a retry replays the decline instead of re-deciding.
func TestADeclinedIncrementIsReplayed(t *testing.T) {
	c := require.New(t)
	harness := newFixture(t, budget500)
	harness.authorize("hotel", 300)
	declined := harness.applyEvent("hotel", "inc1", lifecycle.TypeIncrement, newAmount(300))

	again, replayed, err := harness.events.Apply(harness.ctx, lifecycle.Event{ID: harness.buildID("inc1"), AuthorizationID: harness.buildID("hotel"),
		Type: lifecycle.TypeIncrement, AmountMinor: newAmount(300)})

	c.NoError(err)
	c.True(replayed)
	c.Equal(declined, again)
}

// D-053: a capture above the hold counts the captured amount.
func TestCaptureAboveTheHoldUsesUpTheBudget(t *testing.T) {
	c := require.New(t)
	harness := newFixture(t, budget500)
	harness.authorize("dinner", 400)

	res := harness.applyEvent("dinner", "cap1", lifecycle.TypeCapture, newAmount(460))

	c.Equal(lifecycle.StatusCaptured, res.Status)
	c.Equal(int64(460), res.CapturedMinor)
	c.Equal(domain.Decline, harness.authorize("coffee", 50).Decision, "460 + 50 is over 500")
	c.Equal(domain.Approve, harness.authorize("water", 40).Decision)
}

func TestRefundGivesTheBudgetBack(t *testing.T) {
	c := require.New(t)
	harness := newFixture(t, budget500)
	harness.authorize("dinner", 400)
	harness.applyEvent("dinner", "cap1", lifecycle.TypeCapture, newAmount(400))

	partial := harness.applyEvent("dinner", "ref1", lifecycle.TypeRefund, newAmount(100))
	full := harness.applyEvent("dinner", "ref2", lifecycle.TypeRefund, nil)

	c.Equal(lifecycle.StatusCaptured, partial.Status)
	c.Equal(int64(100), partial.RefundedMinor)
	c.Equal(lifecycle.StatusRefunded, full.Status)
	c.Equal(int64(400), full.RefundedMinor)
	c.Equal(domain.Approve, harness.authorize("next", 500).Decision)
}

func TestRefundBeforeCaptureIsRefused(t *testing.T) {
	c := require.New(t)
	harness := newFixture(t, budget500)
	harness.authorize("dinner", 400)

	_, _, err := harness.events.Apply(harness.ctx, lifecycle.Event{ID: harness.buildID("ref1"), AuthorizationID: harness.buildID("dinner"), Type: lifecycle.TypeRefund})

	c.ErrorIs(err, lifecycle.ErrNotCaptured)
}

func TestGetHoldOfAnUnknownAuthorizationIsNotFound(t *testing.T) {
	c := require.New(t)
	harness := newFixture(t, budget500)

	_, err := harness.st.GetHold(harness.ctx, "missing")

	c.ErrorIs(err, domain.ErrNotFound)
}
