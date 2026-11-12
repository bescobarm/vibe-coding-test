package store_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/bescobarm/card-auth-engine/internal/authz"
	"github.com/bescobarm/card-auth-engine/internal/controls"
	"github.com/bescobarm/card-auth-engine/internal/domain"
	"github.com/bescobarm/card-auth-engine/internal/pipeline"
	"github.com/bescobarm/card-auth-engine/internal/store"
	"github.com/bescobarm/card-auth-engine/migrations"
)

// newTestStore connects to the compose Postgres, creates an isolated schema,
// migrates and seeds it, and drops it when the test ends.
func newTestStore(t *testing.T, maxConns int32) (*store.Store, *pgxpool.Pool) {
	t.Helper()
	c := require.New(t)
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = "postgres://auth:auth@localhost:55432/auth?sslmode=disable"
	}
	ctx := context.Background()

	admin, err := pgxpool.New(ctx, url)
	c.NoError(err, "connect")
	if err := admin.Ping(ctx); err != nil {
		admin.Close()
		t.Skipf("Postgres not reachable (run `make up`): %v", err)
	}
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	schema := "test_" + hex.EncodeToString(b)
	_, err = admin.Exec(ctx, "CREATE SCHEMA "+schema)
	c.NoError(err, "create schema")
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		admin.Close()
	})

	cfg, err := pgxpool.ParseConfig(url)
	c.NoError(err)
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	if maxConns > 0 {
		cfg.MaxConns = maxConns
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	c.NoError(err)
	t.Cleanup(pool.Close)

	c.NoError(store.Migrate(ctx, pool, migrations.FS), "migrate")
	st := store.New(pool)
	c.NoError(st.SeedDemo(ctx), "seed")
	return st, pool
}

func request(id, card string) domain.AuthRequest {
	return domain.AuthRequest{
		AuthorizationID: id, CardID: card, AmountMinor: 4500, Currency: "USD",
		Merchant:  domain.Merchant{ID: "m_987", Name: "ACME SOFTWARE", MCC: "5734", Country: "US"},
		EntryMode: "ecommerce", CreatedAt: time.Date(2026, 11, 12, 15, 4, 5, 0, time.UTC),
	}
}

func newService(st *store.Store) *authz.Service {
	return authz.NewService(st, st, st, pipeline.New(controls.CardStatus{}))
}

func TestMigrateIsIdempotent(t *testing.T) {
	c := require.New(t)
	_, pool := newTestStore(t, 0)

	err := store.Migrate(context.Background(), pool, migrations.FS)

	c.NoError(err, "second migrate")
}

func TestGetCardReturnsTheSeededCard(t *testing.T) {
	c := require.New(t)
	st, _ := newTestStore(t, 0)

	card, err := st.GetCard(context.Background(), "card_frozen")

	c.NoError(err)
	c.Equal(domain.CardFrozen, card.Status)
	c.Equal("acme-mx", card.CompanyID)
	c.Equal("ch_marta", card.CardholderID)
}

func TestGetCardReturnsNotFoundForAnUnknownCard(t *testing.T) {
	c := require.New(t)
	st, _ := newTestStore(t, 0)

	_, err := st.GetCard(context.Background(), "card_nope")

	c.ErrorIs(err, domain.ErrNotFound)
}

func TestLatestPolicyVersionReturnsTheSeededVersion(t *testing.T) {
	c := require.New(t)
	st, _ := newTestStore(t, 0)

	v, err := st.LatestPolicyVersion(context.Background(), "acme-mx")

	c.NoError(err)
	c.Equal(1, v)
}

func TestLatestPolicyVersionIsZeroForAnUnknownCompany(t *testing.T) {
	c := require.New(t)
	st, _ := newTestStore(t, 0)

	v, err := st.LatestPolicyVersion(context.Background(), "nobody")

	c.NoError(err)
	c.Zero(v)
}

func TestAuthorizeRecordsAuditAndOutbox(t *testing.T) {
	c := require.New(t)
	st, pool := newTestStore(t, 0)
	ctx := context.Background()

	resp, replayed, err := newService(st).Authorize(ctx, request("auth_1", "card_123"))

	c.NoError(err)
	c.False(replayed)
	c.Equal(domain.Approve, resp.Decision)
	c.Equal(1, resp.PolicyVersion)

	rec, err := st.GetAuthorization(ctx, "auth_1")
	c.NoError(err)
	c.Equal("pending", rec.Status)
	c.NotEmpty(rec.Request)
	c.NotEmpty(rec.Response)
	c.Nil(rec.Explanation)

	var outbox int
	c.NoError(pool.QueryRow(ctx, `SELECT count(*) FROM outbox`).Scan(&outbox))
	c.Equal(1, outbox)
}

func TestDeclineIsStoredAsDeclined(t *testing.T) {
	c := require.New(t)
	st, _ := newTestStore(t, 0)
	ctx := context.Background()

	resp, _, err := newService(st).Authorize(ctx, request("auth_frozen", "card_frozen"))

	c.NoError(err)
	c.Equal(domain.Decline, resp.Decision)
	c.Equal(domain.ReasonCardInactive, resp.Reasons[0].Code)
	rec, err := st.GetAuthorization(ctx, "auth_frozen")
	c.NoError(err)
	c.Equal("declined", rec.Status)
}

func TestReplayReturnsTheStoredResponse(t *testing.T) {
	c := require.New(t)
	st, _ := newTestStore(t, 0)
	svc := newService(st)
	ctx := context.Background()
	first, _, err := svc.Authorize(ctx, request("auth_2", "card_123"))
	c.NoError(err)

	// A retry with a different body must still get the original decision (D-008).
	second, replayed, err := svc.Authorize(ctx, request("auth_2", "card_frozen"))

	c.NoError(err)
	c.True(replayed)
	c.Equal(first, second)
}

func TestDecideErrorRollsBackSoARetryCanDecide(t *testing.T) {
	c := require.New(t)
	st, _ := newTestStore(t, 0)
	ctx := context.Background()
	req := request("auth_3", "card_123")
	boom := errors.New("boom")

	_, _, err := st.Decide(ctx, req, func(context.Context) (domain.AuthResponse, error) {
		return domain.AuthResponse{}, boom
	})

	c.ErrorIs(err, boom)
	_, err = st.GetAuthorization(ctx, "auth_3")
	c.ErrorIs(err, domain.ErrNotFound, "claim was not rolled back")
	_, replayed, err := newService(st).Authorize(ctx, req)
	c.NoError(err, "retry")
	c.False(replayed)
}

// AC3 of SP01: simultaneous duplicates are evaluated exactly once and all get
// the same answer. The pool is deliberately smaller than the number of
// concurrent requests: holding a transaction while asking the same pool for a
// second connection would deadlock here.
func TestConcurrentDuplicatesAreDecidedOnce(t *testing.T) {
	c := require.New(t)
	st, _ := newTestStore(t, 4)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var evaluations atomic.Int32
	svc := authz.NewService(st, st, st, pipeline.New(countingStage{&evaluations}, controls.CardStatus{}))

	const n = 20
	var wg sync.WaitGroup
	results := make([]domain.AuthResponse, n)
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], _, errs[i] = svc.Authorize(ctx, request("auth_dup", "card_123"))
		}()
	}
	wg.Wait()

	for i := range n {
		c.NoError(errs[i], "request %d", i)
		c.Equal(results[0], results[i], "request %d got a different answer", i)
	}
	c.Equal(int32(1), evaluations.Load(), "pipeline must run exactly once")
}

// Many distinct requests at once, more than the pool size: must not deadlock.
func TestConcurrentDistinctRequestsDoNotExhaustThePool(t *testing.T) {
	c := require.New(t)
	st, _ := newTestStore(t, 4)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	svc := newService(st)

	const n = 40
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, errs[i] = svc.Authorize(ctx, request(fmt.Sprintf("auth_many_%d", i), "card_123"))
		}()
	}
	wg.Wait()

	for i := range n {
		c.NoError(errs[i], "request %d", i)
	}
}

func TestGetAuthorizationNotFound(t *testing.T) {
	c := require.New(t)
	st, _ := newTestStore(t, 0)

	_, err := st.GetAuthorization(context.Background(), "missing")

	c.ErrorIs(err, domain.ErrNotFound)
}

func TestPing(t *testing.T) {
	c := require.New(t)
	st, _ := newTestStore(t, 0)

	c.NoError(st.Ping(context.Background()))
}

type countingStage struct{ n *atomic.Int32 }

func (countingStage) Name() string { return "counting" }

func (c countingStage) Evaluate(context.Context, *pipeline.Evaluation) error {
	c.n.Add(1)
	time.Sleep(50 * time.Millisecond) // keep the claim open so duplicates really overlap
	return nil
}
