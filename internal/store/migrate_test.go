package store_test

import (
	"context"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"github.com/bescobarm/card-auth-engine/internal/store"
)

func TestMigrateFailsOnInvalidSQL(t *testing.T) {
	c := require.New(t)
	_, pool := newTestStore(t, 0)

	err := store.Migrate(context.Background(), pool, fstest.MapFS{"9001_bad.sql": {Data: []byte("NOT VALID SQL")}})

	c.ErrorContains(err, "apply 9001_bad.sql")
}

func TestMigrateFailsWhenTheMigrationCannotBeRecorded(t *testing.T) {
	c := require.New(t)
	_, pool := newTestStore(t, 0)
	// The migration itself removes the bookkeeping table, so recording it fails.
	files := fstest.MapFS{"9001_drop.sql": {Data: []byte("DROP TABLE schema_migrations")}}

	err := store.Migrate(context.Background(), pool, files)

	c.ErrorContains(err, "record 9001_drop.sql")
}

func TestMigrateFailsOnAnUnreadableFile(t *testing.T) {
	c := require.New(t)
	_, pool := newTestStore(t, 0)

	err := store.Migrate(context.Background(), pool, fstest.MapFS{"9001_dir.sql": {Mode: fs.ModeDir}})

	c.ErrorContains(err, "9001_dir.sql")
}

func TestMigrateFailedFileIsNotRecorded(t *testing.T) {
	c := require.New(t)
	_, pool := newTestStore(t, 0)
	ctx := context.Background()

	c.Error(store.Migrate(ctx, pool, fstest.MapFS{"9001_bad.sql": {Data: []byte("NOT VALID SQL")}}))

	var recorded bool
	c.NoError(pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = '9001_bad.sql')`,
	).Scan(&recorded))
	c.False(recorded, "a failed migration must not be recorded as applied")
}

func TestMigrateOnClosedPool(t *testing.T) {
	c := require.New(t)
	_, pool := newTestStore(t, 0)
	pool.Close()

	err := store.Migrate(context.Background(), pool, fstest.MapFS{})

	c.ErrorContains(err, "acquire")
}
