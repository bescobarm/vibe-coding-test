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

func amount(v int64) *int64 { return &v }

func (f fixture) reverse(name, event string, amt *int64) (lifecycle.Result, bool, error) {
	return f.events.Apply(f.ctx, lifecycle.Event{ID: f.id(event), AuthorizationID: f.id(name), Type: lifecycle.TypeReversal, AmountMinor: amt})
}

func (f fixture) stored(name string) (string, int64) {
	f.t.Helper()
	var (
		status string
		held   int64
	)

	err := f.pool.QueryRow(f.ctx, `SELECT status, held_minor FROM authorizations WHERE authorization_id = $1`, f.id(name)).Scan(&status, &held)
	require.NoError(f.t, err)
	return status, held
}

func (f fixture) redisHold(name string) string {
	return f.rdb.HGet(f.ctx, "hold:{"+f.card+"}:"+f.id(name), "amount").Val()
}

// AC2 of SP03: a reversed hold gives its budget back.
func TestReversalReleasesTheBudgetForTheNextRequest(t *testing.T) {
	c := require.New(t)
	f := newFixture(t, budget500)
	c.Equal(domain.Approve, f.authorize("a1", 400).Decision)
	c.Equal(domain.Decline, f.authorize("a2", 450).Decision)

	res, replayed, err := f.reverse("a1", "e1", nil)

	c.NoError(err)
	c.False(replayed)
	c.Equal(lifecycle.Result{AuthorizationID: f.id("a1"), EventID: f.id("e1"), Type: lifecycle.TypeReversal,
		Status: lifecycle.StatusReversed, ReleasedMinor: 400, HeldMinor: 0}, res)
	c.Equal(domain.Approve, f.authorize("a3", 450).Decision)
}

// AC4 of SP03: the same event twice releases once.
func TestRepeatedEventReleasesOnce(t *testing.T) {
	c := require.New(t)
	f := newFixture(t, budget500)
	f.authorize("a1", 400)

	first, _, err := f.reverse("a1", "e1", amount(100))
	c.NoError(err)
	again, replayed, err := f.reverse("a1", "e1", amount(100))

	c.NoError(err)
	c.True(replayed)
	c.Equal(first, again)
	status, held := f.stored("a1")
	c.Equal(lifecycle.StatusPending, status)
	c.Equal(int64(300), held)
	c.Equal("300", f.redisHold("a1"))
}

// AC7 of SP03 (D-051): reversed card-testing charges still count.
func TestReversedCardTestingChargesStillCount(t *testing.T) {
	c := require.New(t)
	f := newFixture(t, cardTesting)
	for i := range 5 {
		name := fmt.Sprintf("small%d", i)
		c.Equal(domain.Approve, f.authorize(name, 300).Decision)
		_, _, err := f.reverse(name, "rev"+name, nil)
		c.NoError(err)
	}

	resp := f.authorize("small6", 300)

	c.Equal(domain.Decline, resp.Decision)
	c.Equal(domain.ReasonVelocityExceeded, resp.Reasons[0].Code)
}

func TestExpiryReleasesTheWholeHold(t *testing.T) {
	c := require.New(t)
	f := newFixture(t, budget500)
	f.authorize("a1", 400)

	res, _, err := f.events.Apply(f.ctx, lifecycle.Event{ID: f.id("e1"), AuthorizationID: f.id("a1"), Type: lifecycle.TypeExpiry})

	c.NoError(err)
	c.Equal(lifecycle.StatusExpired, res.Status)
	c.Equal("0", f.redisHold("a1"))

	var topics int
	err = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM outbox WHERE topic = 'authorization.lifecycle' AND payload->>'event_id' = $1`, f.id("e1")).Scan(&topics)
	c.NoError(err)
	c.Equal(1, topics)
}

func TestEventIDReusedForAnotherCallIsAConflict(t *testing.T) {
	c := require.New(t)
	f := newFixture(t, budget500)
	f.authorize("a1", 400)
	_, _, err := f.reverse("a1", "e1", amount(100))
	c.NoError(err)

	_, _, err = f.reverse("a1", "e1", amount(200))

	c.ErrorIs(err, lifecycle.ErrEventConflict)
}

func TestARefusedEventIsNotStored(t *testing.T) {
	c := require.New(t)
	f := newFixture(t, budget500)
	f.authorize("a1", 400)

	_, _, err := f.reverse("a1", "e1", amount(401))
	c.ErrorIs(err, lifecycle.ErrOverHold)
	_, replayed, err := f.reverse("a1", "e1", nil)

	c.NoError(err)
	c.False(replayed, "the refused call must not have stored e1")
}

func TestAnEventOnAnUnknownAuthorizationIsNotFound(t *testing.T) {
	c := require.New(t)
	f := newFixture(t, budget500)

	_, _, err := f.reverse("missing", "e1", nil)

	c.ErrorIs(err, domain.ErrNotFound)
}

func TestADeclinedAuthorizationCannotBeReversed(t *testing.T) {
	c := require.New(t)
	f := newFixture(t, budget500)
	f.authorize("a1", 600)

	_, _, err := f.reverse("a1", "e1", nil)

	c.ErrorIs(err, lifecycle.ErrDeclined)
}

// Parallel partial reversals with different event_ids never release more than the hold.
func TestConcurrentPartialReversalsNeverReleaseMoreThanTheHold(t *testing.T) {
	c := require.New(t)
	f := newFixture(t, budget500)
	f.authorize("a1", 400)

	const n = 10
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, errs[i] = f.reverse("a1", fmt.Sprintf("e%d", i), amount(100))
		}()
	}

	wg.Wait()

	applied := 0
	for i := range n {
		if errs[i] == nil {
			applied++
			continue
		}

		c.ErrorIs(errs[i], lifecycle.ErrNotPending)
	}

	status, held := f.stored("a1")
	c.Equal(4, applied)
	c.Equal(lifecycle.StatusReversed, status)
	c.Zero(held)
	c.Equal("0", f.redisHold("a1"))
}

// Parallel copies of one event are applied once; the others replay it.
func TestConcurrentCopiesOfAnEventApplyOnce(t *testing.T) {
	c := require.New(t)
	f := newFixture(t, budget500)
	f.authorize("a1", 400)

	const n = 10
	var wg sync.WaitGroup
	replays := make([]bool, n)
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, replays[i], errs[i] = f.reverse("a1", "e1", amount(100))
		}()
	}

	wg.Wait()

	fresh := 0
	for i := range n {
		c.NoError(errs[i], "call %d", i)
		if !replays[i] {
			fresh++
		}
	}

	_, held := f.stored("a1")
	c.Equal(1, fresh)
	c.Equal(int64(300), held)
	c.Equal("300", f.redisHold("a1"))
}
