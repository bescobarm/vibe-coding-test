package velocity

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/bescobarm/card-auth-engine/internal/domain"
	"github.com/bescobarm/card-auth-engine/internal/lifecycle"
	"github.com/bescobarm/card-auth-engine/internal/policy"
)

var errDBDown = errors.New("db down")

type fakeSource struct {
	auths []Authorization
	err   error
}

func (f fakeSource) ListApprovedSince(context.Context, time.Time) ([]Authorization, error) {
	return f.auths, f.err
}

type fakePolicies struct {
	doc policy.Document
	err error
}

func (f fakePolicies) GetPolicy(context.Context, string) (policy.Document, error) {
	return f.doc, f.err
}

const rebuildRules = `[{"id": "card-testing", "window": "10m", "max_count": 5, "filter": {"max_amount_minor": 500}},
	{"id": "daily", "window": "calendar_day", "max_amount": {"amount_minor": 2000, "currency": "USD"}}]`

func newApprovedAuthorization(card, id string, amount int64, status string, created time.Time) Authorization {
	return Authorization{
		Hold:      lifecycle.Hold{AuthorizationID: id, CardID: card, MCC: "5734", Status: status, HeldMinor: amount},
		CompanyID: "acme-mx", AmountMinor: amount, CreatedAt: created,
	}
}

func newTestRebuilder(t *testing.T, rdb redis.Cmdable, src fakeSource, policies fakePolicies) (*Rebuilder, string) {
	t.Helper()
	random := make([]byte, 6)
	_, err := rand.Read(random)
	require.NoError(t, err)

	marker := "test:counters_ready:" + hex.EncodeToString(random)
	t.Cleanup(func() {
		rdb.Del(context.Background(), marker)
	})

	rebuilder := NewRebuilder(src, policies, rdb, marker)
	rebuilder.now = func() time.Time {
		return t0
	}
	return rebuilder, marker
}

func TestRebuildRestoresWindowsAndHolds(t *testing.T) {
	c := require.New(t)
	_, rdb, card := newTestLimiter(t)
	ctx := context.Background()
	src := fakeSource{auths: []Authorization{
		newApprovedAuthorization(card, "small", 300, lifecycle.StatusPending, t0.Add(-time.Minute)),
		newApprovedAuthorization(card, "reversed", 200, lifecycle.StatusReversed, t0.Add(-2*time.Minute)),
		newApprovedAuthorization(card, "morning", 900, lifecycle.StatusPending, t0.Add(-3*time.Hour)),
	}}
	rebuilder, _ := newTestRebuilder(t, rdb, src, fakePolicies{doc: newDailyPolicy(t, rebuildRules)})

	restored, err := rebuilder.Rebuild(ctx)

	c.NoError(err)
	c.Equal(3, restored)
	c.ElementsMatch([]string{"small", "reversed"}, rdb.ZRange(ctx, "vel:{"+card+"}:card-testing:ts", 0, -1).Val(),
		"morning is outside the 10m window and over the filter")
	c.Equal(map[string]string{"small": "300", "morning": "900"}, rdb.HGetAll(ctx, "vel:{"+card+"}:daily:amt").Val(),
		"a reversed authorization keeps its count but not its amount (D-051)")
	c.Equal("0", rdb.HGet(ctx, "hold:{"+card+"}:reversed", "amount").Val())
	c.Equal(`["vel:{`+card+`}:card-testing:amt","vel:{`+card+`}:daily:amt"]`, rdb.HGet(ctx, "hold:{"+card+"}:small", "keys").Val())
}

func TestRebuildSkipsAuthorizationsNoRuleApplies(t *testing.T) {
	c := require.New(t)
	_, rdb, card := newTestLimiter(t)
	rebuilder, _ := newTestRebuilder(t, rdb, fakeSource{auths: []Authorization{newApprovedAuthorization(card, "a1", 300, lifecycle.StatusPending, t0)}},
		fakePolicies{err: domain.ErrNotFound})

	restored, err := rebuilder.Rebuild(context.Background())

	c.NoError(err)
	c.Zero(restored)
	c.Zero(rdb.Exists(context.Background(), "hold:{"+card+"}:a1").Val())
}

func TestRebuildIfLostRunsOnlyWithoutTheMarker(t *testing.T) {
	c := require.New(t)
	_, rdb, card := newTestLimiter(t)
	src := fakeSource{auths: []Authorization{newApprovedAuthorization(card, "a1", 300, lifecycle.StatusPending, t0)}}
	rebuilder, marker := newTestRebuilder(t, rdb, src, fakePolicies{doc: newDailyPolicy(t, rebuildRules)})

	first, err := rebuilder.RebuildIfLost(context.Background())
	c.NoError(err)
	second, err := rebuilder.RebuildIfLost(context.Background())

	c.NoError(err)
	c.Equal(1, first)
	c.Zero(second)
	c.Equal(int64(1), rdb.Exists(context.Background(), marker).Val())
}

func TestRebuildFailsWhenPostgresFails(t *testing.T) {
	c := require.New(t)
	_, rdb, _ := newTestLimiter(t)
	rebuilder, marker := newTestRebuilder(t, rdb, fakeSource{err: errDBDown}, fakePolicies{})

	_, err := rebuilder.RebuildIfLost(context.Background())

	c.ErrorIs(err, errDBDown)
	c.Zero(rdb.Exists(context.Background(), marker).Val(), "the next start must try again")
}

func TestRebuildFailsWhenAPolicyCannotBeLoaded(t *testing.T) {
	c := require.New(t)
	_, rdb, card := newTestLimiter(t)
	rebuilder, _ := newTestRebuilder(t, rdb, fakeSource{auths: []Authorization{newApprovedAuthorization(card, "a1", 300, lifecycle.StatusPending, t0)}},
		fakePolicies{err: errDBDown})

	_, err := rebuilder.Rebuild(context.Background())

	c.ErrorIs(err, errDBDown)
}

func TestRebuildFailsWhenRedisIsUnreachable(t *testing.T) {
	c := require.New(t)
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:1", MaxRetries: -1})
	t.Cleanup(func() {
		_ = rdb.Close()
	})
	rebuilder := NewRebuilder(fakeSource{}, fakePolicies{}, rdb, "marker")

	_, err := rebuilder.RebuildIfLost(context.Background())

	c.ErrorContains(err, "check marker")
}

func TestRebuildReportsAFailedWrite(t *testing.T) {
	c := require.New(t)
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:1", MaxRetries: -1})
	t.Cleanup(func() {
		_ = rdb.Close()
	})
	rebuilder := NewRebuilder(fakeSource{auths: []Authorization{newApprovedAuthorization("card_x", "a1", 300, lifecycle.StatusPending, t0)}},
		fakePolicies{doc: newDailyPolicy(t, rebuildRules)}, rdb, "marker")
	rebuilder.now = func() time.Time {
		return t0
	}

	_, err := rebuilder.Rebuild(context.Background())

	c.ErrorContains(err, "write counters")
}
