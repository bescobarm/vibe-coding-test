package simulator

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/bescobarm/card-auth-engine/internal/testenv"
)

// newTestPool connects to the compose Postgres and makes a cards table in an
// isolated schema, dropped when the test ends.
func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	c := require.New(t)
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = "postgres://auth:auth@localhost:55432/auth?sslmode=disable"
	}

	ctx := context.Background()

	admin, err := pgxpool.New(ctx, url)
	c.NoError(err)

	err = admin.Ping(ctx)
	if err != nil {
		admin.Close()
		testenv.SkipOrFail(t, "Postgres", err)
	}

	random := make([]byte, 6)
	_, err = rand.Read(random)
	c.NoError(err)

	schema := "sim_" + hex.EncodeToString(random)
	_, err = admin.Exec(ctx, "CREATE SCHEMA "+schema)
	c.NoError(err)
	t.Cleanup(func() {
		_, dropErr := admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		c.NoError(dropErr)
		admin.Close()
	})

	config, err := pgxpool.ParseConfig(url)
	c.NoError(err)
	config.ConnConfig.RuntimeParams["search_path"] = schema

	pool, err := pgxpool.NewWithConfig(ctx, config)
	c.NoError(err)
	t.Cleanup(pool.Close)

	_, err = pool.Exec(ctx, `CREATE TABLE cards (card_id TEXT PRIMARY KEY, company_id TEXT NOT NULL,
		cardholder_id TEXT NOT NULL, status TEXT NOT NULL, overrides JSONB)`)
	c.NoError(err)

	return pool
}

func TestPostgresCardsCreatesActiveCards(t *testing.T) {
	c := require.New(t)
	pool := newTestPool(t)

	err := NewPostgresCards(pool).CreateCards(context.Background(), "sim-r1", []string{"sim-r1-a", "sim-r1-b"})

	c.NoError(err)
	var count int
	c.NoError(pool.QueryRow(context.Background(),
		`SELECT count(*) FROM cards WHERE company_id = 'sim-r1' AND status = 'active' AND cardholder_id = 'holder-' || card_id`).Scan(&count))
	c.Equal(2, count)
}

func TestPostgresCardsReportsADuplicate(t *testing.T) {
	c := require.New(t)
	cards := NewPostgresCards(newTestPool(t))
	c.NoError(cards.CreateCards(context.Background(), "sim-r1", []string{"sim-r1-a"}))

	err := cards.CreateCards(context.Background(), "sim-r1", []string{"sim-r1-a"})

	c.ErrorContains(err, "insert cards")
}
