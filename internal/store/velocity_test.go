package store_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/bescobarm/card-auth-engine/internal/authz"
	"github.com/bescobarm/card-auth-engine/internal/controls"
	"github.com/bescobarm/card-auth-engine/internal/domain"
	"github.com/bescobarm/card-auth-engine/internal/lifecycle"
	"github.com/bescobarm/card-auth-engine/internal/pipeline"
	"github.com/bescobarm/card-auth-engine/internal/policy"
	"github.com/bescobarm/card-auth-engine/internal/scoring"
	"github.com/bescobarm/card-auth-engine/internal/store"
	"github.com/bescobarm/card-auth-engine/internal/velocity"
)

func newTestRedis(t *testing.T) *redis.Client {
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

	t.Cleanup(func() { _ = rdb.Close() })
	return rdb
}

// fixture is a fresh company and card with the given velocity rules, wired to
// the real Postgres and Redis. Redis keys use the card id, so tests never share them.
type fixture struct {
	t      *testing.T
	ctx    context.Context
	st     *store.Store
	pool   *pgxpool.Pool
	rdb    *redis.Client
	card   string
	suffix string
	auth   *authz.Service
	events *lifecycle.Service
}

func newFixture(t *testing.T, rules string) fixture {
	t.Helper()
	return newFixtureWith(t, `"velocity": `+rules)
}

// newFixtureWith takes the policy's fields after company_id, e.g. `"velocity": [...], "risk": {...}`.
func newFixtureWith(t *testing.T, fields string) fixture {
	t.Helper()
	c := require.New(t)
	st, pool := newTestStore(t, 8)
	rdb := newTestRedis(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)

	b := make([]byte, 6)
	_, err := rand.Read(b)
	c.NoError(err)

	suffix := hex.EncodeToString(b)
	company, card := "velo-"+suffix, "card_velo_"+suffix
	t.Cleanup(func() {
		keys, _ := rdb.Keys(context.Background(), "*{"+card+"}*").Result()
		if len(keys) > 0 {
			rdb.Del(context.Background(), keys...)
		}
	})

	_, err = pool.Exec(ctx, `INSERT INTO cards (card_id, company_id, cardholder_id, status) VALUES ($1, $2, 'ch_velo', 'active')`, card, company)
	c.NoError(err)

	doc, err := policy.Parse([]byte(`{"company_id": "` + company + `", ` + fields + `}`))
	c.NoError(err)
	c.Empty(doc.Validate())

	policies := policy.NewCache(st)
	_, err = policies.Publish(ctx, doc)
	c.NoError(err)

	limiter := velocity.NewLimiter(rdb, time.Second)
	stages := append(controls.Hard(), velocity.NewStage(limiter), scoring.Stage{})

	return fixture{
		t: t, ctx: ctx, st: st, pool: pool, rdb: rdb, card: card, suffix: suffix,
		auth:   authz.NewService(st, st, policies, pipeline.New(stages...)).WithSignals(st),
		events: lifecycle.NewService(st, st, limiter, st, policies),
	}
}

func (f fixture) id(name string) string { return name + "_" + f.suffix }

func (f fixture) authorize(name string, amount int64) domain.AuthResponse {
	f.t.Helper()
	return f.authorizeWith(name, amount, func(*domain.AuthRequest) {})
}

// authorizeWith lets a test change the merchant, country or entry mode.
func (f fixture) authorizeWith(name string, amount int64, change func(*domain.AuthRequest)) domain.AuthResponse {
	f.t.Helper()
	req := request(f.id(name), f.card)
	req.AmountMinor = amount
	change(&req)

	resp, _, err := f.auth.Authorize(f.ctx, req)
	require.NoError(f.t, err)
	return resp
}

const dailyBudget = `[{"id": "daily-spend", "window": "calendar_day", "max_amount": {"amount_minor": 45000, "currency": "USD"}}]`

// AC1 of SP03 end to end: 100 parallel requests on a card whose daily budget
// allows exactly 10 of them. Exactly 10 are approved, and the audit agrees.
func TestConcurrentRequestsNeverExceedTheDailyBudget(t *testing.T) {
	c := require.New(t)
	f := newFixture(t, dailyBudget)

	const n = 100
	var wg sync.WaitGroup
	results := make([]domain.AuthResponse, n)
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], _, errs[i] = f.auth.Authorize(f.ctx, request(f.id(fmt.Sprintf("auth_%d", i)), f.card))
		}()
	}

	wg.Wait()

	approved := 0
	for i := range n {
		c.NoError(errs[i], "request %d", i)
		if results[i].Decision == domain.Approve {
			approved++
			continue
		}

		c.Equal(domain.ReasonOverDailyLimit, results[i].Reasons[0].Code)
	}

	var stored int
	err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM authorizations WHERE card_id = $1 AND decision = 'APPROVE'`, f.card).Scan(&stored)
	c.NoError(err)
	c.Equal(10, approved)
	c.Equal(10, stored)
}
