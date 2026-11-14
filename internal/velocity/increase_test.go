package velocity

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/bescobarm/card-auth-engine/internal/domain"
	"github.com/bescobarm/card-auth-engine/internal/lifecycle"
	"github.com/bescobarm/card-auth-engine/internal/policy"
)

func newDailyPolicy(t *testing.T, rules string) policy.Document {
	t.Helper()
	doc, err := policy.Parse([]byte(`{"company_id": "acme-mx", "velocity": ` + rules + `}`))
	require.NoError(t, err)
	require.Empty(t, doc.Validate())
	return doc
}

const daily500 = `[{"id": "daily", "window": "calendar_day", "max_amount": {"amount_minor": 500, "currency": "USD"}},
	{"id": "count", "window": "1h", "max_count": 1}]`

func increaseHold(l *Limiter, card, id, event string, amount, total int64, doc policy.Document) (lifecycle.Limit, error) {
	return l.Increase(context.Background(), lifecycle.Increase{CardID: card, AuthorizationID: id, EventID: event,
		AmountMinor: amount, TotalMinor: total, Currency: "USD", MCC: "7011", Now: t0, Policy: doc})
}

func newDailyWindow(doc policy.Document) Window {
	rule := doc.Velocity[0]
	return Window{RuleID: rule.ID, Start: rule.GetWindowStart(t0, time.UTC), MaxCount: NoLimit, MaxAmount: rule.MaxAmount.AmountMinor, Retention: rule.GetRetention()}
}

// AC3 of SP03: hotel 300, +150 approved, +100 more declined.
func TestIncreaseRaisesTheHoldUntilTheBudgetIsFull(t *testing.T) {
	c := require.New(t)
	limiter, rdb, card := newTestLimiter(t)
	doc := newDailyPolicy(t, daily500)
	reserveAmount(c, limiter, card, "hotel", 300, newDailyWindow(doc))

	first, err := increaseHold(limiter, card, "hotel", "inc1", 150, 450, doc)
	c.NoError(err)
	second, err := increaseHold(limiter, card, "hotel", "inc2", 100, 550, doc)
	c.NoError(err)

	c.False(first.Exceeded)
	c.Equal(lifecycle.Limit{Exceeded: true, Code: domain.ReasonOverDailyLimit,
		Message: "Rule daily: 450 already spent plus 100 is over the limit of 500 USD per calendar_day"}, second)
	c.Equal("450", rdb.HGet(context.Background(), "vel:{"+card+"}:daily:amt", "hotel").Val())
	c.Equal("450", rdb.HGet(context.Background(), "hold:{"+card+"}:hotel", "amount").Val())
}

func TestIncreaseAppliesAnEventOnlyOnce(t *testing.T) {
	c := require.New(t)
	limiter, rdb, card := newTestLimiter(t)
	doc := newDailyPolicy(t, daily500)
	reserveAmount(c, limiter, card, "hotel", 300, newDailyWindow(doc))

	_, err := increaseHold(limiter, card, "hotel", "inc1", 100, 400, doc)
	c.NoError(err)
	again, err := increaseHold(limiter, card, "hotel", "inc1", 100, 400, doc)

	c.NoError(err)
	c.False(again.Exceeded)
	c.Equal("400", rdb.HGet(context.Background(), "vel:{"+card+"}:daily:amt", "hotel").Val())
}

// Yesterday's hotel check-in is outside today's window: only the increment counts today.
func TestIncreaseCountsOnlyTheIncrementInANewWindow(t *testing.T) {
	c := require.New(t)
	limiter, rdb, card := newTestLimiter(t)
	doc := newDailyPolicy(t, daily500)

	limit, err := increaseHold(limiter, card, "hotel", "inc1", 150, 450, doc)

	c.NoError(err)
	c.False(limit.Exceeded)
	c.Equal("150", rdb.HGet(context.Background(), "vel:{"+card+"}:daily:amt", "hotel").Val())
	c.Equal(int64(1), rdb.ZCard(context.Background(), "vel:{"+card+"}:daily:ts").Val())
}

func TestIncreaseWithoutAmountRulesSkipsRedis(t *testing.T) {
	c := require.New(t)
	limiter := NewLimiter(redis.NewClient(&redis.Options{Addr: "localhost:1"}), time.Millisecond)

	limit, err := increaseHold(limiter, "card_x", "a1", "inc1", 100, 400, newDailyPolicy(t, `[{"id": "count", "window": "1h", "max_count": 1}]`))

	c.NoError(err)
	c.False(limit.Exceeded)
}

func TestIncreaseInAnotherCurrencyIsDeclined(t *testing.T) {
	c := require.New(t)
	limiter := NewLimiter(redis.NewClient(&redis.Options{Addr: "localhost:1"}), time.Millisecond)

	limit, err := limiter.Increase(context.Background(), lifecycle.Increase{CardID: "card_x", AuthorizationID: "a1", EventID: "inc1",
		AmountMinor: 100, TotalMinor: 400, Currency: "MXN", Now: t0, Policy: newDailyPolicy(t, daily500)})

	c.NoError(err)
	c.Equal(lifecycle.Limit{Exceeded: true, Code: domain.ReasonOverDailyLimit, Message: "Rule daily limit is in USD, request is in MXN"}, limit)
}

func TestIncreaseFailsWhenRedisIsUnreachable(t *testing.T) {
	c := require.New(t)
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:1", MaxRetries: -1})
	t.Cleanup(func() {
		_ = rdb.Close()
	})

	_, err := increaseHold(NewLimiter(rdb, 50*time.Millisecond), "card_x", "a1", "inc1", 100, 400, newDailyPolicy(t, daily500))

	c.ErrorContains(err, "run script")
}
