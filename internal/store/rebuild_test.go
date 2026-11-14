package store_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bescobarm/card-auth-engine/internal/domain"
	"github.com/bescobarm/card-auth-engine/internal/lifecycle"
	"github.com/bescobarm/card-auth-engine/internal/policy"
	"github.com/bescobarm/card-auth-engine/internal/velocity"
)

const rebuildRules = `[{"id": "card-testing", "window": "10m", "max_count": 5, "filter": {"max_amount_minor": 500}},
	{"id": "daily-spend", "window": "calendar_day", "max_amount": {"amount_minor": 2000, "currency": "USD"}}]`

// counters reads every key of the fixture's card the way the budgets see it:
// members of each window, amounts, and each hold's amount and window keys.
// Applied event_ids are left out: they only matter between a Redis change and
// its Postgres commit, and Postgres replays committed events itself.
func (f fixture) counters() map[string]any {
	f.t.Helper()
	c := require.New(f.t)
	out := map[string]any{}

	keys, err := f.rdb.Keys(f.ctx, "*{"+f.card+"}*").Result()
	c.NoError(err)

	for _, k := range keys {
		switch {
		case strings.HasSuffix(k, ":ts"):
			members := f.rdb.ZRange(f.ctx, k, 0, -1).Val()
			slices.Sort(members)
			out[k] = members
		case strings.HasSuffix(k, ":amt"):
			out[k] = f.rdb.HGetAll(f.ctx, k).Val()
		default:
			out[k] = f.hold(k)
		}
	}

	return out
}

func (f fixture) hold(key string) []string {
	f.t.Helper()
	var windows []string
	require.NoError(f.t, json.Unmarshal([]byte(f.rdb.HGet(f.ctx, key, "keys").Val()), &windows))
	slices.Sort(windows)
	return append([]string{f.rdb.HGet(f.ctx, key, "amount").Val()}, windows...)
}

// AC5 of SP03: after Redis loses a card's counters, the rebuild restores
// exactly what the budgets saw: pending, reversed, incremented, captured with a
// tip and partly refunded authorizations. A declined one leaves nothing.
func TestRebuildRestoresTheCountersAfterRedisLosesThem(t *testing.T) {
	c := require.New(t)
	f := newFixture(t, rebuildRules)
	f.authorize("small", 300)
	f.authorize("reversed", 200)
	f.event("reversed", "rev1", lifecycle.TypeReversal, nil)
	f.authorize("hotel", 600)
	f.event("hotel", "inc1", lifecycle.TypeIncrement, amount(200))
	f.authorize("dinner", 400)
	f.event("dinner", "cap1", lifecycle.TypeCapture, amount(450))
	f.event("dinner", "ref1", lifecycle.TypeRefund, amount(100))
	c.Equal(domain.Decline, f.authorize("too-big", 1000).Decision)
	before := f.counters()

	keys, err := f.rdb.Keys(f.ctx, "*{"+f.card+"}*").Result()
	c.NoError(err)
	c.NoError(f.rdb.Del(f.ctx, keys...).Err())
	c.Empty(f.counters())

	n, err := velocity.NewRebuilder(f.st, policy.NewCache(f.st), f.rdb, "unused").Rebuild(f.ctx)

	c.NoError(err)
	c.Equal(4, n)
	c.Equal(before, f.counters())
	c.Equal(domain.Decline, f.authorize("still-too-big", 1000).Decision, "300 + 800 + 350 + 1000 is still over 2000")
	c.Equal(domain.Approve, f.authorize("fits", 550).Decision)
}
