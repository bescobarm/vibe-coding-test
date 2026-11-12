// Command engine runs the card authorization engine.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bescobarm/card-auth-engine/internal/api"
	"github.com/bescobarm/card-auth-engine/internal/authz"
	"github.com/bescobarm/card-auth-engine/internal/controls"
	"github.com/bescobarm/card-auth-engine/internal/pipeline"
	"github.com/bescobarm/card-auth-engine/internal/policy"
	"github.com/bescobarm/card-auth-engine/internal/store"
	"github.com/bescobarm/card-auth-engine/migrations"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("engine stopped", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, env("DATABASE_URL", "postgres://auth:auth@localhost:55432/auth?sslmode=disable"))
	if err != nil {
		return err
	}

	defer pool.Close()

	if err := store.Migrate(ctx, pool, migrations.FS); err != nil {
		return err
	}

	st := store.New(pool)
	if env("SEED_DEMO", "true") == "true" {
		if err := st.SeedDemo(ctx); err != nil {
			return err
		}
	}

	policies := policy.NewCache(st)

	// SP03: velocity and budget · SP04: anomaly score go after the hard controls.
	pl := pipeline.New(controls.Hard()...)
	svc := authz.NewService(st, st, policies, pl)

	srv := &http.Server{
		Addr:              ":" + env("PORT", "8080"),
		Handler:           api.NewHandler(svc, st, st, log),
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
	}

	errc := make(chan error, 1)

	go func() { errc <- srv.ListenAndServe() }()

	log.Info("engine listening", "addr", srv.Addr)

	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
	return nil
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}

	return def
}
