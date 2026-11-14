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

// readCounters reads every key of the fixture's card the way the budgets see it:
// members of each window, amounts, and each hold's amount and window keys.
// Applied event_ids are left out: they only matter between a Redis change and
// its Postgres commit, and Postgres replays committed events itself.
func (f fixture) readCounters() map[string]any {
	f.t.Helper()
	c := require.New(f.t)
	out := map[string]any{}

	keys, err := f.rdb.Keys(f.ctx, "*{"+f.card+"}*").Result()
	c.NoError(err)

	for _, key := range keys {
		switch {
		case strings.HasSuffix(key, ":ts"):
			members := f.rdb.ZRange(f.ctx, key, 0, -1).Val()
			slices.Sort(members)
			out[key] = members
		case strings.HasSuffix(key, ":amt"):
			out[key] = f.rdb.HGetAll(f.ctx, key).Val()
		default:
			out[key] = f.readHold(key)
		}
	}

	return out
}

func (f fixture) readHold(key string) []string {
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
	harness := newFixture(t, rebuildRules)
	harness.authorize("small", 300)
	harness.authorize("reversed", 200)
	harness.applyEvent("reversed", "rev1", lifecycle.TypeReversal, nil)
	harness.authorize("hotel", 600)
	harness.applyEvent("hotel", "inc1", lifecycle.TypeIncrement, newAmount(200))
	harness.authorize("dinner", 400)
	harness.applyEvent("dinner", "cap1", lifecycle.TypeCapture, newAmount(450))
	harness.applyEvent("dinner", "ref1", lifecycle.TypeRefund, newAmount(100))
	c.Equal(domain.Decline, harness.authorize("too-big", 1000).Decision)
	before := harness.readCounters()

	keys, err := harness.rdb.Keys(harness.ctx, "*{"+harness.card+"}*").Result()
	c.NoError(err)
	c.NoError(harness.rdb.Del(harness.ctx, keys...).Err())
	c.Empty(harness.readCounters())

	restored, err := velocity.NewRebuilder(harness.st, policy.NewCache(harness.st), harness.rdb, "unused").Rebuild(harness.ctx)

	c.NoError(err)
	c.Equal(4, restored)
	c.Equal(before, harness.readCounters())
	c.Equal(domain.Decline, harness.authorize("still-too-big", 1000).Decision, "300 + 800 + 350 + 1000 is still over 2000")
	c.Equal(domain.Approve, harness.authorize("fits", 550).Decision)
}
