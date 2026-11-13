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

const (
	defaultDatabaseURL = "postgres://auth:auth@localhost:55432/auth?sslmode=disable"
	defaultPort        = "8080"
	envTrue            = "true"

	readHeaderTimeout = 2 * time.Second
	readTimeout       = 5 * time.Second
	writeTimeout      = 5 * time.Second
	shutdownTimeout   = 5 * time.Second
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	err := run(log)
	if err != nil {
		log.Error("engine stopped", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, env("DATABASE_URL", defaultDatabaseURL))
	if err != nil {
		return err
	}

	defer pool.Close()

	err = store.Migrate(ctx, pool, migrations.FS)
	if err != nil {
		return err
	}

	st := store.New(pool)
	if env("SEED_DEMO", envTrue) == envTrue {
		err := st.SeedDemo(ctx)
		if err != nil {
			return err
		}
	}

	policies := policy.NewCache(st)

	// SP03: velocity and budget · SP04: anomaly score go after the hard controls.
	pl := pipeline.New(controls.Hard()...)
	svc := authz.NewService(st, st, policies, pl)

	srv := &http.Server{
		Addr:              ":" + env("PORT", defaultPort),
		Handler:           api.NewHandler(svc, st, policies, st, log),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
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
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}

	return nil
}

func env(key, def string) string {
	v := os.Getenv(key)
	if v != "" {
		return v
	}

	return def
}
