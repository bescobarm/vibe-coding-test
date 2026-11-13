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

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/bescobarm/card-auth-engine/internal/authz"
	"github.com/bescobarm/card-auth-engine/internal/controls"
	"github.com/bescobarm/card-auth-engine/internal/domain"
	"github.com/bescobarm/card-auth-engine/internal/pipeline"
	"github.com/bescobarm/card-auth-engine/internal/policy"
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

// AC1 of SP03 end to end: 100 parallel requests on a card whose daily budget
// allows exactly 10 of them. Exactly 10 are approved, and the audit agrees.
func TestConcurrentRequestsNeverExceedTheDailyBudget(t *testing.T) {
	c := require.New(t)
	st, pool := newTestStore(t, 8)
	rdb := newTestRedis(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

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

	doc, err := policy.Parse([]byte(`{"company_id": "` + company + `", "velocity": [{"id": "daily-spend", "window": "calendar_day",
		"max_amount": {"amount_minor": 45000, "currency": "USD"}}]}`))
	c.NoError(err)
	c.Empty(doc.Validate())

	policies := policy.NewCache(st)
	_, err = policies.Publish(ctx, doc)
	c.NoError(err)

	stages := append(controls.Hard(), velocity.NewStage(velocity.NewLimiter(rdb, time.Second)))
	svc := authz.NewService(st, st, policies, pipeline.New(stages...))

	const n = 100
	var wg sync.WaitGroup
	results := make([]domain.AuthResponse, n)
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], _, errs[i] = svc.Authorize(ctx, request(fmt.Sprintf("auth_%s_%d", suffix, i), card))
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
	err = pool.QueryRow(ctx, `SELECT count(*) FROM authorizations WHERE card_id = $1 AND decision = 'APPROVE'`, card).Scan(&stored)
	c.NoError(err)
	c.Equal(10, approved)
	c.Equal(10, stored)
}
