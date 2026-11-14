package velocity

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/bescobarm/card-auth-engine/internal/lifecycle"
	"github.com/bescobarm/card-auth-engine/internal/testenv"
)

const testTimeout = 2 * time.Second

var t0 = time.Date(2026, 11, 13, 15, 0, 0, 0, time.UTC)

// newTestLimiter connects to the compose Redis (D-047) and gives the test its
// own card id, so tests never share keys. The keys are deleted at the end.
func newTestLimiter(t *testing.T) (*Limiter, *redis.Client, string) {
	t.Helper()
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}

	rdb := redis.NewClient(&redis.Options{Addr: addr})
	err := rdb.Ping(context.Background()).Err()
	if err != nil {
		testenv.SkipOrFail(t, "Redis", err)
	}

	random := make([]byte, 6)
	_, err = rand.Read(random)
	require.NoError(t, err)

	card := "card_" + hex.EncodeToString(random)
	t.Cleanup(func() {
		ctx := context.Background()
		for _, pattern := range []string{"vel:{" + card + "}:*", "hold:{" + card + "}:*"} {
			keys, _ := rdb.Keys(ctx, pattern).Result()
			if len(keys) > 0 {
				rdb.Del(ctx, keys...)
			}
		}

		_ = rdb.Close()
	})

	return NewLimiter(rdb, testTimeout), rdb, card
}

func newWindow(id string, maxCount, maxAmount int64) Window {
	return Window{RuleID: id, Start: t0.Add(-time.Hour), MaxCount: maxCount, MaxAmount: maxAmount, Retention: time.Hour}
}

func reserveAmount(c *require.Assertions, l *Limiter, card, id string, amount int64, ws ...Window) Result {
	res, err := l.Reserve(context.Background(), Reservation{
		CardID: card, AuthorizationID: id, AmountMinor: amount, Now: t0, Windows: ws,
	})
	c.NoError(err)
	return res
}

func TestReserveRecordsTheAmountInEveryWindow(t *testing.T) {
	c := require.New(t)
	limiter, rdb, card := newTestLimiter(t)
	ctx := context.Background()

	res := reserveAmount(c, limiter, card, "a1", 700, newWindow("count", 5, NoLimit), newWindow("daily", NoLimit, 1000))

	c.True(res.Reserved)
	c.Equal("700", rdb.HGet(ctx, "vel:{"+card+"}:daily:amt", "a1").Val())
	c.Equal(int64(1), rdb.ZCard(ctx, "vel:{"+card+"}:count:ts").Val())
	c.Equal("700", rdb.HGet(ctx, "hold:{"+card+"}:a1", "amount").Val())
	c.Positive(rdb.PTTL(ctx, "vel:{"+card+"}:daily:ts").Val())
}

func TestReserveDeclinesWhenTheCountIsFull(t *testing.T) {
	c := require.New(t)
	limiter, _, card := newTestLimiter(t)
	window := newWindow("count", 2, NoLimit)
	reserveAmount(c, limiter, card, "a1", 100, window)
	reserveAmount(c, limiter, card, "a2", 200, window)

	res := reserveAmount(c, limiter, card, "a3", 100, window)

	c.Equal(Result{Reserved: false, Window: 0, Count: 2, SpentMinor: 300}, res)
}

func TestReserveDeclinesWhenTheAmountWouldGoOverTheLimit(t *testing.T) {
	c := require.New(t)
	limiter, _, card := newTestLimiter(t)
	window := newWindow("daily", NoLimit, 1000)
	reserveAmount(c, limiter, card, "a1", 600, window)

	over := reserveAmount(c, limiter, card, "a2", 500, window)
	exact := reserveAmount(c, limiter, card, "a3", 400, window)

	c.Equal(Result{Reserved: false, Window: 0, Count: 1, SpentMinor: 600}, over)
	c.True(exact.Reserved)
}

func TestReserveReportsTheFirstFullWindowAndWritesNothing(t *testing.T) {
	c := require.New(t)
	limiter, rdb, card := newTestLimiter(t)
	roomy, full := newWindow("roomy", 10, NoLimit), newWindow("full", 1, NoLimit)
	reserveAmount(c, limiter, card, "a1", 100, full)

	res := reserveAmount(c, limiter, card, "a2", 100, roomy, full)

	c.False(res.Reserved)
	c.Equal(1, res.Window)
	c.Zero(rdb.ZCard(context.Background(), "vel:{"+card+"}:roomy:ts").Val())
	c.Zero(rdb.Exists(context.Background(), "hold:{"+card+"}:a2").Val())
}

func TestReserveIsIdempotentPerAuthorizationID(t *testing.T) {
	c := require.New(t)
	limiter, _, card := newTestLimiter(t)
	window := newWindow("daily", NoLimit, 1000)

	first := reserveAmount(c, limiter, card, "a1", 600, window)
	retry := reserveAmount(c, limiter, card, "a1", 600, window)
	other := reserveAmount(c, limiter, card, "a2", 500, window)

	c.True(first.Reserved)
	c.True(retry.Reserved)
	c.Equal(int64(600), other.SpentMinor, "the retry must not count twice")
}

func TestReserveForgetsEntriesOlderThanTheWindow(t *testing.T) {
	c := require.New(t)
	limiter, rdb, card := newTestLimiter(t)
	reserveAmount(c, limiter, card, "a1", 100, newWindow("count", 1, NoLimit))

	later := Window{RuleID: "count", Start: t0.Add(time.Minute), MaxCount: 1, MaxAmount: NoLimit, Retention: time.Hour}
	res, err := limiter.Reserve(context.Background(), Reservation{
		CardID: card, AuthorizationID: "a2", AmountMinor: 100, Now: t0.Add(11 * time.Minute), Windows: []Window{later},
	})

	c.NoError(err)
	c.True(res.Reserved)
	c.Equal([]string{"a2"}, rdb.HKeys(context.Background(), "vel:{"+card+"}:count:amt").Val())
}

// AC1 of SP03: 100 parallel requests on a card whose limit allows 10.
func TestConcurrentReservesNeverExceedTheLimit(t *testing.T) {
	c := require.New(t)
	limiter, _, card := newTestLimiter(t)
	window := newWindow("daily", NoLimit, 1000)

	const parallel = 100
	var wg sync.WaitGroup
	results := make([]Result, parallel)
	errs := make([]error, parallel)
	for i := range parallel {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = limiter.Reserve(context.Background(), Reservation{
				CardID: card, AuthorizationID: hex.EncodeToString([]byte{byte(i)}), AmountMinor: 100, Now: t0, Windows: []Window{window},
			})
		}()
	}

	wg.Wait()

	reserved := 0
	for i := range parallel {
		c.NoError(errs[i], "request %d", i)
		if results[i].Reserved {
			reserved++
		}
	}

	c.Equal(10, reserved)
}

func TestReserveFailsFastWhenRedisIsUnreachable(t *testing.T) {
	c := require.New(t)
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:1", MaxRetries: -1})
	t.Cleanup(func() {
		_ = rdb.Close()
	})
	limiter := NewLimiter(rdb, 50*time.Millisecond)

	_, err := limiter.Reserve(context.Background(), Reservation{CardID: "card_x", AuthorizationID: "a1", AmountMinor: 1, Now: t0,
		Windows: []Window{newWindow("count", 1, NoLimit)}})

	c.ErrorContains(err, "run script")
}

// shortReplyScripter answers every script call with a reply of the wrong length.
type shortReplyScripter struct{ redis.Scripter }

func (shortReplyScripter) EvalSha(ctx context.Context, _ string, _ []string, _ ...any) *redis.Cmd {
	return redis.NewCmdResult([]any{int64(1)}, nil)
}

func TestReserveRejectsAnUnexpectedReply(t *testing.T) {
	c := require.New(t)
	limiter := NewLimiter(shortReplyScripter{}, time.Second)

	_, err := limiter.Reserve(context.Background(), Reservation{CardID: "card_x", AuthorizationID: "a1", AmountMinor: 1, Now: t0,
		Windows: []Window{newWindow("count", 1, NoLimit)}})

	c.ErrorIs(err, ErrUnexpectedReply)
}

func releaseAmount(c *require.Assertions, l *Limiter, card, id, event string, amount int64) {
	c.NoError(l.Adjust(context.Background(), lifecycle.Adjust{CardID: card, AuthorizationID: id, EventID: event, DeltaMinor: -amount}))
}

// D-051: a full release frees the budget, but the authorization still counts.
func TestReleaseFreesTheAmountButKeepsTheCount(t *testing.T) {
	c := require.New(t)
	limiter, rdb, card := newTestLimiter(t)
	count, daily := newWindow("count", 5, NoLimit), newWindow("daily", NoLimit, 500)
	reserveAmount(c, limiter, card, "a1", 400, count, daily)

	releaseAmount(c, limiter, card, "a1", "e1", 400)

	c.Zero(rdb.HLen(context.Background(), "vel:{"+card+"}:daily:amt").Val())
	c.Equal(int64(1), rdb.ZCard(context.Background(), "vel:{"+card+"}:count:ts").Val())
	c.Equal("0", rdb.HGet(context.Background(), "hold:{"+card+"}:a1", "amount").Val())
	c.True(reserveAmount(c, limiter, card, "a2", 450, count, daily).Reserved, "AC2: the released budget is available again")
}

func TestPartialReleaseLowersTheHeldAmount(t *testing.T) {
	c := require.New(t)
	limiter, rdb, card := newTestLimiter(t)
	daily := newWindow("daily", NoLimit, 500)
	reserveAmount(c, limiter, card, "a1", 400, daily)

	releaseAmount(c, limiter, card, "a1", "e1", 150)

	c.Equal("250", rdb.HGet(context.Background(), "vel:{"+card+"}:daily:amt", "a1").Val())
	c.Equal(int64(250), reserveAmount(c, limiter, card, "a2", 300, daily).SpentMinor)
}

func TestReleaseAppliesAnEventOnlyOnce(t *testing.T) {
	c := require.New(t)
	limiter, rdb, card := newTestLimiter(t)
	reserveAmount(c, limiter, card, "a1", 400, newWindow("daily", NoLimit, 1000))

	releaseAmount(c, limiter, card, "a1", "e1", 100)
	releaseAmount(c, limiter, card, "a1", "e1", 100)

	c.Equal("300", rdb.HGet(context.Background(), "hold:{"+card+"}:a1", "amount").Val())
}

func TestReleaseWithoutAHoldDoesNothing(t *testing.T) {
	c := require.New(t)
	limiter, rdb, card := newTestLimiter(t)

	releaseAmount(c, limiter, card, "never-reserved", "e1", 100)

	c.Zero(rdb.Exists(context.Background(), "hold:{"+card+"}:never-reserved").Val())
}

func TestReleaseDoesNotBringBackAnEntryThatLeftTheWindow(t *testing.T) {
	c := require.New(t)
	limiter, rdb, card := newTestLimiter(t)
	ctx := context.Background()
	reserveAmount(c, limiter, card, "a1", 400, newWindow("daily", NoLimit, 1000))
	rdb.HDel(ctx, "vel:{"+card+"}:daily:amt", "a1")

	releaseAmount(c, limiter, card, "a1", "e1", 100)

	c.False(rdb.HExists(ctx, "vel:{"+card+"}:daily:amt", "a1").Val())
}

// D-053: a capture above the hold raises what the budget counts.
func TestAdjustUpCountsACaptureAboveTheHold(t *testing.T) {
	c := require.New(t)
	limiter, rdb, card := newTestLimiter(t)
	reserveAmount(c, limiter, card, "a1", 400, newWindow("daily", NoLimit, 1000))

	c.NoError(limiter.Adjust(context.Background(), lifecycle.Adjust{CardID: card, AuthorizationID: "a1", EventID: "cap", DeltaMinor: 60}))

	c.Equal("460", rdb.HGet(context.Background(), "vel:{"+card+"}:daily:amt", "a1").Val())
	c.Equal("460", rdb.HGet(context.Background(), "hold:{"+card+"}:a1", "amount").Val())
}

func TestCancelRemovesTheReservationFromEveryWindow(t *testing.T) {
	c := require.New(t)
	limiter, rdb, card := newTestLimiter(t)
	ctx := context.Background()
	count, daily := newWindow("count", 5, NoLimit), newWindow("daily", NoLimit, 1000)
	reserveAmount(c, limiter, card, "a1", 400, count, daily)
	reserveAmount(c, limiter, card, "a2", 100, count, daily)

	c.NoError(limiter.Cancel(ctx, card, "a1"))
	c.NoError(limiter.Cancel(ctx, card, "a1"), "cancel is safe to repeat")

	c.Equal([]string{"a2"}, rdb.ZRange(ctx, "vel:{"+card+"}:count:ts", 0, -1).Val())
	c.Equal(map[string]string{"a2": "100"}, rdb.HGetAll(ctx, "vel:{"+card+"}:daily:amt").Val())
	c.Zero(rdb.Exists(ctx, "hold:{"+card+"}:a1").Val())
}

func TestCancelFailsFastWhenRedisIsUnreachable(t *testing.T) {
	c := require.New(t)
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:1", MaxRetries: -1})
	t.Cleanup(func() {
		_ = rdb.Close()
	})

	err := NewLimiter(rdb, 50*time.Millisecond).Cancel(context.Background(), "card_x", "a1")

	c.ErrorContains(err, "cancel")
}

func TestReleaseFailsFastWhenRedisIsUnreachable(t *testing.T) {
	c := require.New(t)
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:1", MaxRetries: -1})
	t.Cleanup(func() {
		_ = rdb.Close()
	})

	err := NewLimiter(rdb, 50*time.Millisecond).Adjust(context.Background(),
		lifecycle.Adjust{CardID: "card_x", AuthorizationID: "a1", EventID: "e1", DeltaMinor: -1})

	c.ErrorContains(err, "adjust")
}
