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
		t.Skipf("Redis not reachable (run `make up`): %v", err)
	}

	b := make([]byte, 6)
	_, err = rand.Read(b)
	require.NoError(t, err)

	card := "card_" + hex.EncodeToString(b)
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

func window(id string, maxCount, maxAmount int64) Window {
	return Window{RuleID: id, Start: t0.Add(-time.Hour), MaxCount: maxCount, MaxAmount: maxAmount, Retention: time.Hour}
}

func reserve(c *require.Assertions, l *Limiter, card, id string, amount int64, ws ...Window) Result {
	res, err := l.Reserve(context.Background(), Reservation{
		CardID: card, AuthorizationID: id, AmountMinor: amount, Now: t0, Windows: ws,
	})
	c.NoError(err)
	return res
}

func TestReserveRecordsTheAmountInEveryWindow(t *testing.T) {
	c := require.New(t)
	l, rdb, card := newTestLimiter(t)
	ctx := context.Background()

	res := reserve(c, l, card, "a1", 700, window("count", 5, NoLimit), window("daily", NoLimit, 1000))

	c.True(res.Reserved)
	c.Equal("700", rdb.HGet(ctx, "vel:{"+card+"}:daily:amt", "a1").Val())
	c.Equal(int64(1), rdb.ZCard(ctx, "vel:{"+card+"}:count:ts").Val())
	c.Equal("700", rdb.HGet(ctx, "hold:{"+card+"}:a1", "amount").Val())
	c.Positive(rdb.PTTL(ctx, "vel:{"+card+"}:daily:ts").Val())
}

func TestReserveDeclinesWhenTheCountIsFull(t *testing.T) {
	c := require.New(t)
	l, _, card := newTestLimiter(t)
	w := window("count", 2, NoLimit)
	reserve(c, l, card, "a1", 100, w)
	reserve(c, l, card, "a2", 200, w)

	res := reserve(c, l, card, "a3", 100, w)

	c.Equal(Result{Reserved: false, Window: 0, Count: 2, SpentMinor: 300}, res)
}

func TestReserveDeclinesWhenTheAmountWouldGoOverTheLimit(t *testing.T) {
	c := require.New(t)
	l, _, card := newTestLimiter(t)
	w := window("daily", NoLimit, 1000)
	reserve(c, l, card, "a1", 600, w)

	over := reserve(c, l, card, "a2", 500, w)
	exact := reserve(c, l, card, "a3", 400, w)

	c.Equal(Result{Reserved: false, Window: 0, Count: 1, SpentMinor: 600}, over)
	c.True(exact.Reserved)
}

func TestReserveReportsTheFirstFullWindowAndWritesNothing(t *testing.T) {
	c := require.New(t)
	l, rdb, card := newTestLimiter(t)
	roomy, full := window("roomy", 10, NoLimit), window("full", 1, NoLimit)
	reserve(c, l, card, "a1", 100, full)

	res := reserve(c, l, card, "a2", 100, roomy, full)

	c.False(res.Reserved)
	c.Equal(1, res.Window)
	c.Zero(rdb.ZCard(context.Background(), "vel:{"+card+"}:roomy:ts").Val())
	c.Zero(rdb.Exists(context.Background(), "hold:{"+card+"}:a2").Val())
}

func TestReserveIsIdempotentPerAuthorizationID(t *testing.T) {
	c := require.New(t)
	l, _, card := newTestLimiter(t)
	w := window("daily", NoLimit, 1000)

	first := reserve(c, l, card, "a1", 600, w)
	retry := reserve(c, l, card, "a1", 600, w)
	other := reserve(c, l, card, "a2", 500, w)

	c.True(first.Reserved)
	c.True(retry.Reserved)
	c.Equal(int64(600), other.SpentMinor, "the retry must not count twice")
}

func TestReserveForgetsEntriesOlderThanTheWindow(t *testing.T) {
	c := require.New(t)
	l, rdb, card := newTestLimiter(t)
	reserve(c, l, card, "a1", 100, window("count", 1, NoLimit))

	later := Window{RuleID: "count", Start: t0.Add(time.Minute), MaxCount: 1, MaxAmount: NoLimit, Retention: time.Hour}
	res, err := l.Reserve(context.Background(), Reservation{
		CardID: card, AuthorizationID: "a2", AmountMinor: 100, Now: t0.Add(11 * time.Minute), Windows: []Window{later},
	})

	c.NoError(err)
	c.True(res.Reserved)
	c.Equal([]string{"a2"}, rdb.HKeys(context.Background(), "vel:{"+card+"}:count:amt").Val())
}

// AC1 of SP03: 100 parallel requests on a card whose limit allows 10.
func TestConcurrentReservesNeverExceedTheLimit(t *testing.T) {
	c := require.New(t)
	l, _, card := newTestLimiter(t)
	w := window("daily", NoLimit, 1000)

	const n = 100
	var wg sync.WaitGroup
	results := make([]Result, n)
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = l.Reserve(context.Background(), Reservation{
				CardID: card, AuthorizationID: hex.EncodeToString([]byte{byte(i)}), AmountMinor: 100, Now: t0, Windows: []Window{w},
			})
		}()
	}

	wg.Wait()

	reserved := 0
	for i := range n {
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
	t.Cleanup(func() { _ = rdb.Close() })
	l := NewLimiter(rdb, 50*time.Millisecond)

	_, err := l.Reserve(context.Background(), Reservation{CardID: "card_x", AuthorizationID: "a1", AmountMinor: 1, Now: t0,
		Windows: []Window{window("count", 1, NoLimit)}})

	c.ErrorContains(err, "run script")
}

// shortReplyScripter answers every script call with a reply of the wrong length.
type shortReplyScripter struct{ redis.Scripter }

func (shortReplyScripter) EvalSha(ctx context.Context, _ string, _ []string, _ ...any) *redis.Cmd {
	return redis.NewCmdResult([]any{int64(1)}, nil)
}

func TestReserveRejectsAnUnexpectedReply(t *testing.T) {
	c := require.New(t)
	l := NewLimiter(shortReplyScripter{}, time.Second)

	_, err := l.Reserve(context.Background(), Reservation{CardID: "card_x", AuthorizationID: "a1", AmountMinor: 1, Now: t0,
		Windows: []Window{window("count", 1, NoLimit)}})

	c.ErrorIs(err, ErrUnexpectedReply)
}

func release(c *require.Assertions, l *Limiter, card, id, event string, amount int64) {
	c.NoError(l.Adjust(context.Background(), lifecycle.Adjust{CardID: card, AuthorizationID: id, EventID: event, DeltaMinor: -amount}))
}

// D-051: a full release frees the budget, but the authorization still counts.
func TestReleaseFreesTheAmountButKeepsTheCount(t *testing.T) {
	c := require.New(t)
	l, rdb, card := newTestLimiter(t)
	count, daily := window("count", 5, NoLimit), window("daily", NoLimit, 500)
	reserve(c, l, card, "a1", 400, count, daily)

	release(c, l, card, "a1", "e1", 400)

	c.Zero(rdb.HLen(context.Background(), "vel:{"+card+"}:daily:amt").Val())
	c.Equal(int64(1), rdb.ZCard(context.Background(), "vel:{"+card+"}:count:ts").Val())
	c.Equal("0", rdb.HGet(context.Background(), "hold:{"+card+"}:a1", "amount").Val())
	c.True(reserve(c, l, card, "a2", 450, count, daily).Reserved, "AC2: the released budget is available again")
}

func TestPartialReleaseLowersTheHeldAmount(t *testing.T) {
	c := require.New(t)
	l, rdb, card := newTestLimiter(t)
	daily := window("daily", NoLimit, 500)
	reserve(c, l, card, "a1", 400, daily)

	release(c, l, card, "a1", "e1", 150)

	c.Equal("250", rdb.HGet(context.Background(), "vel:{"+card+"}:daily:amt", "a1").Val())
	c.Equal(int64(250), reserve(c, l, card, "a2", 300, daily).SpentMinor)
}

func TestReleaseAppliesAnEventOnlyOnce(t *testing.T) {
	c := require.New(t)
	l, rdb, card := newTestLimiter(t)
	reserve(c, l, card, "a1", 400, window("daily", NoLimit, 1000))

	release(c, l, card, "a1", "e1", 100)
	release(c, l, card, "a1", "e1", 100)

	c.Equal("300", rdb.HGet(context.Background(), "hold:{"+card+"}:a1", "amount").Val())
}

func TestReleaseWithoutAHoldDoesNothing(t *testing.T) {
	c := require.New(t)
	l, rdb, card := newTestLimiter(t)

	release(c, l, card, "never-reserved", "e1", 100)

	c.Zero(rdb.Exists(context.Background(), "hold:{"+card+"}:never-reserved").Val())
}

func TestReleaseDoesNotBringBackAnEntryThatLeftTheWindow(t *testing.T) {
	c := require.New(t)
	l, rdb, card := newTestLimiter(t)
	ctx := context.Background()
	reserve(c, l, card, "a1", 400, window("daily", NoLimit, 1000))
	rdb.HDel(ctx, "vel:{"+card+"}:daily:amt", "a1")

	release(c, l, card, "a1", "e1", 100)

	c.False(rdb.HExists(ctx, "vel:{"+card+"}:daily:amt", "a1").Val())
}

// D-053: a capture above the hold raises what the budget counts.
func TestAdjustUpCountsACaptureAboveTheHold(t *testing.T) {
	c := require.New(t)
	l, rdb, card := newTestLimiter(t)
	reserve(c, l, card, "a1", 400, window("daily", NoLimit, 1000))

	c.NoError(l.Adjust(context.Background(), lifecycle.Adjust{CardID: card, AuthorizationID: "a1", EventID: "cap", DeltaMinor: 60}))

	c.Equal("460", rdb.HGet(context.Background(), "vel:{"+card+"}:daily:amt", "a1").Val())
	c.Equal("460", rdb.HGet(context.Background(), "hold:{"+card+"}:a1", "amount").Val())
}

func TestReleaseFailsFastWhenRedisIsUnreachable(t *testing.T) {
	c := require.New(t)
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:1", MaxRetries: -1})
	t.Cleanup(func() { _ = rdb.Close() })

	err := NewLimiter(rdb, 50*time.Millisecond).Adjust(context.Background(),
		lifecycle.Adjust{CardID: "card_x", AuthorizationID: "a1", EventID: "e1", DeltaMinor: -1})

	c.ErrorContains(err, "adjust")
}
