package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/bescobarm/card-auth-engine/internal/authz"
	"github.com/bescobarm/card-auth-engine/internal/controls"
	"github.com/bescobarm/card-auth-engine/internal/domain"
	"github.com/bescobarm/card-auth-engine/internal/pipeline"
	"github.com/bescobarm/card-auth-engine/internal/policy"
	"github.com/bescobarm/card-auth-engine/internal/scoring"
	"github.com/bescobarm/card-auth-engine/internal/store"
	"github.com/bescobarm/card-auth-engine/internal/velocity"
)

// newRedisDownService is the real engine on the seeded demo data, with Redis
// pointed at a port where nothing listens (parent brief AC4).
func newRedisDownService(t *testing.T) (*authz.Service, *store.Store, func(string) bool) {
	t.Helper()
	st, pool := newTestStore(t, 8)
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:1", MaxRetries: -1})
	t.Cleanup(func() {
		_ = rdb.Close()
	})

	limiter := velocity.NewLimiter(rdb, 50*time.Millisecond)
	stages := append(controls.ListHardControls(), velocity.NewStage(limiter), scoring.NewStage(scoring.GetDefaultWeights()))
	svc := authz.NewService(st, st, policy.NewCache(st), pipeline.New(stages...)).WithSignals(st)

	fallbackStored := func(id string) bool {
		var used bool
		err := pool.QueryRow(context.Background(), `SELECT fallback_used FROM authorizations WHERE authorization_id = $1`, id).Scan(&used)
		require.NoError(t, err)
		return used
	}

	return svc, st, fallbackStored
}

// AC1 of SP05: Redis is down; 100.00 with fail_open_limited up to 200.00 is
// approved with FALLBACK_APPLIED, within the deadline, and stored.
func TestRedisDownApprovesUnderTheFailOpenCap(t *testing.T) {
	c := require.New(t)
	svc, _, fallbackStored := newRedisDownService(t)
	request := newRequest("fb_small", "card_123")
	request.AmountMinor = 10000
	start := time.Now()

	resp, _, err := svc.Authorize(context.Background(), request)

	c.NoError(err)
	c.Less(time.Since(start), authz.DefaultDeadline)
	c.Equal(domain.Approve, resp.Decision)
	c.True(resp.FallbackUsed)
	c.Equal(domain.ReasonFallbackApplied, resp.Reasons[0].Code)
	c.True(fallbackStored("fb_small"))
}

// AC2 of SP05: the same with 300.00 is declined.
func TestRedisDownDeclinesOverTheFailOpenCap(t *testing.T) {
	c := require.New(t)
	svc, _, _ := newRedisDownService(t)
	request := newRequest("fb_big", "card_123")
	request.AmountMinor = 30000

	resp, _, err := svc.Authorize(context.Background(), request)

	c.NoError(err)
	c.Equal(domain.Decline, resp.Decision)
	c.Equal("Fallback fail_open_limited: amount 30000 is over 20000 USD", resp.Reasons[0].Message)
}

// AC6 of SP05 (D-031): a retry replays the stored fallback.
func TestARetryReplaysTheStoredFallback(t *testing.T) {
	c := require.New(t)
	svc, _, _ := newRedisDownService(t)
	request := newRequest("fb_retry", "card_123")
	request.AmountMinor = 10000
	first, _, err := svc.Authorize(context.Background(), request)
	c.NoError(err)

	again, replayed, err := svc.Authorize(context.Background(), request)

	c.NoError(err)
	c.True(replayed)
	c.Equal(first, again)
}

// Hard controls still run before velocity: a blocked MCC is declined normally
// even with Redis down, because the decline happens before Redis is called.
func TestABlockedMCCIsDeclinedBeforeRedisIsNeeded(t *testing.T) {
	c := require.New(t)
	svc, _, _ := newRedisDownService(t)
	request := newRequest("fb_gambling", "card_123")
	request.Merchant.MCC = "7995"

	resp, _, err := svc.Authorize(context.Background(), request)

	c.NoError(err)
	c.False(resp.FallbackUsed)
	c.Equal(domain.ReasonMCCBlocked, resp.Reasons[0].Code)
}
