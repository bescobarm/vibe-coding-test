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

func (f fakeSource) ApprovedSince(context.Context, time.Time) ([]Authorization, error) {
	return f.auths, f.err
}

type fakePolicies struct {
	doc policy.Document
	err error
}

func (f fakePolicies) Policy(context.Context, string) (policy.Document, error) { return f.doc, f.err }

const rebuildRules = `[{"id": "card-testing", "window": "10m", "max_count": 5, "filter": {"max_amount_minor": 500}},
	{"id": "daily", "window": "calendar_day", "max_amount": {"amount_minor": 2000, "currency": "USD"}}]`

func approved(card, id string, amount int64, status string, created time.Time) Authorization {
	return Authorization{
		Hold:      lifecycle.Hold{AuthorizationID: id, CardID: card, MCC: "5734", Status: status, HeldMinor: amount},
		CompanyID: "acme-mx", AmountMinor: amount, CreatedAt: created,
	}
}

func newTestRebuilder(t *testing.T, rdb redis.Cmdable, src fakeSource, policies fakePolicies) (*Rebuilder, string) {
	t.Helper()
	b := make([]byte, 6)
	_, err := rand.Read(b)
	require.NoError(t, err)

	marker := "test:counters_ready:" + hex.EncodeToString(b)
	t.Cleanup(func() { rdb.Del(context.Background(), marker) })

	r := NewRebuilder(src, policies, rdb, marker)
	r.now = func() time.Time { return t0 }
	return r, marker
}

func TestRebuildRestoresWindowsAndHolds(t *testing.T) {
	c := require.New(t)
	_, rdb, card := newTestLimiter(t)
	ctx := context.Background()
	src := fakeSource{auths: []Authorization{
		approved(card, "small", 300, lifecycle.StatusPending, t0.Add(-time.Minute)),
		approved(card, "reversed", 200, lifecycle.StatusReversed, t0.Add(-2*time.Minute)),
		approved(card, "morning", 900, lifecycle.StatusPending, t0.Add(-3*time.Hour)),
	}}
	r, _ := newTestRebuilder(t, rdb, src, fakePolicies{doc: dailyPolicy(t, rebuildRules)})

	n, err := r.Rebuild(ctx)

	c.NoError(err)
	c.Equal(3, n)
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
	r, _ := newTestRebuilder(t, rdb, fakeSource{auths: []Authorization{approved(card, "a1", 300, lifecycle.StatusPending, t0)}},
		fakePolicies{err: domain.ErrNotFound})

	n, err := r.Rebuild(context.Background())

	c.NoError(err)
	c.Zero(n)
	c.Zero(rdb.Exists(context.Background(), "hold:{"+card+"}:a1").Val())
}

func TestRebuildIfLostRunsOnlyWithoutTheMarker(t *testing.T) {
	c := require.New(t)
	_, rdb, card := newTestLimiter(t)
	src := fakeSource{auths: []Authorization{approved(card, "a1", 300, lifecycle.StatusPending, t0)}}
	r, marker := newTestRebuilder(t, rdb, src, fakePolicies{doc: dailyPolicy(t, rebuildRules)})

	first, err := r.RebuildIfLost(context.Background())
	c.NoError(err)
	second, err := r.RebuildIfLost(context.Background())

	c.NoError(err)
	c.Equal(1, first)
	c.Zero(second)
	c.Equal(int64(1), rdb.Exists(context.Background(), marker).Val())
}

func TestRebuildFailsWhenPostgresFails(t *testing.T) {
	c := require.New(t)
	_, rdb, _ := newTestLimiter(t)
	r, marker := newTestRebuilder(t, rdb, fakeSource{err: errDBDown}, fakePolicies{})

	_, err := r.RebuildIfLost(context.Background())

	c.ErrorIs(err, errDBDown)
	c.Zero(rdb.Exists(context.Background(), marker).Val(), "the next start must try again")
}

func TestRebuildFailsWhenAPolicyCannotBeLoaded(t *testing.T) {
	c := require.New(t)
	_, rdb, card := newTestLimiter(t)
	r, _ := newTestRebuilder(t, rdb, fakeSource{auths: []Authorization{approved(card, "a1", 300, lifecycle.StatusPending, t0)}},
		fakePolicies{err: errDBDown})

	_, err := r.Rebuild(context.Background())

	c.ErrorIs(err, errDBDown)
}

func TestRebuildFailsWhenRedisIsUnreachable(t *testing.T) {
	c := require.New(t)
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:1", MaxRetries: -1})
	t.Cleanup(func() { _ = rdb.Close() })
	r := NewRebuilder(fakeSource{}, fakePolicies{}, rdb, "marker")

	_, err := r.RebuildIfLost(context.Background())

	c.ErrorContains(err, "check marker")
}

func TestRebuildReportsAFailedWrite(t *testing.T) {
	c := require.New(t)
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:1", MaxRetries: -1})
	t.Cleanup(func() { _ = rdb.Close() })
	r := NewRebuilder(fakeSource{auths: []Authorization{approved("card_x", "a1", 300, lifecycle.StatusPending, t0)}},
		fakePolicies{doc: dailyPolicy(t, rebuildRules)}, rdb, "marker")
	r.now = func() time.Time { return t0 }

	_, err := r.Rebuild(context.Background())

	c.ErrorContains(err, "write counters")
}
