package store

import (
	"context"
	"fmt"
	"io/fs"
	"sort"

	"github.com/jackc/pgx/v5/pgxpool"
)

// migrateLockID is an arbitrary constant so two engines starting at the same
// time don't apply migrations twice.
const migrateLockID = 7331001

const (
	migrationsGlob = "*.sql"

	lockSQL   = `SELECT pg_advisory_lock($1)`
	unlockSQL = `SELECT pg_advisory_unlock($1)`

	createMigrationsSQL = `CREATE TABLE IF NOT EXISTS schema_migrations (
	version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`
	migrationAppliedSQL = `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`
	recordMigrationSQL  = `INSERT INTO schema_migrations (version) VALUES ($1)`
)

// Migrate applies every *.sql file in files that has not been applied yet,
// in name order, each in its own transaction (D-022).
func Migrate(ctx context.Context, pool *pgxpool.Pool, files fs.FS) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire: %w", err)
	}

	defer conn.Release()

	_, err = conn.Exec(ctx, lockSQL, migrateLockID)
	if err != nil {
		return fmt.Errorf("lock: %w", err)
	}

	defer conn.Exec(context.Background(), unlockSQL, migrateLockID) //nolint:errcheck

	_, err = conn.Exec(ctx, createMigrationsSQL)
	if err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	names, err := fs.Glob(files, migrationsGlob)
	if err != nil {
		return err
	}

	sort.Strings(names)

	for _, name := range names {
		err = applyIfNew(ctx, conn, files, name)
		if err != nil {
			return err
		}
	}

	return nil
}

func applyIfNew(ctx context.Context, conn *pgxpool.Conn, files fs.FS, name string) error {
	var applied bool

	err := conn.QueryRow(ctx, migrationAppliedSQL, name).Scan(&applied)
	if err != nil {
		return fmt.Errorf("check %s: %w", name, err)
	}

	if applied {
		return nil
	}

	sql, err := fs.ReadFile(files, name)
	if err != nil {
		return err
	}

	return apply(ctx, conn, name, string(sql))
}

// apply runs one migration and records it in the same transaction.
func apply(ctx context.Context, conn *pgxpool.Conn, name, sql string) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}

	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit

	_, err = tx.Exec(ctx, sql)
	if err != nil {
		return fmt.Errorf("apply %s: %w", name, err)
	}

	_, err = tx.Exec(ctx, recordMigrationSQL, name)
	if err != nil {
		return fmt.Errorf("record %s: %w", name, err)
	}

	err = tx.Commit(ctx)
	if err != nil {
		return fmt.Errorf("commit %s: %w", name, err)
	}

	return nil
}
