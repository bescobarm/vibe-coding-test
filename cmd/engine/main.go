// Command engine runs the card authorization engine.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/bescobarm/card-auth-engine/internal/api"
	"github.com/bescobarm/card-auth-engine/internal/authz"
	"github.com/bescobarm/card-auth-engine/internal/controls"
	"github.com/bescobarm/card-auth-engine/internal/lifecycle"
	"github.com/bescobarm/card-auth-engine/internal/pipeline"
	"github.com/bescobarm/card-auth-engine/internal/policy"
	"github.com/bescobarm/card-auth-engine/internal/scoring"
	"github.com/bescobarm/card-auth-engine/internal/store"
	"github.com/bescobarm/card-auth-engine/internal/velocity"
	"github.com/bescobarm/card-auth-engine/migrations"
)

const (
	defaultDatabaseURL = "postgres://auth:auth@localhost:55432/auth?sslmode=disable"
	defaultPort        = "8080"
	defaultRedisAddr   = "localhost:6379"
	defaultRedisMS     = 50                        // D-048
	countersMarkerKey  = "velocity:counters_ready" // D-055
	rebuildTimeout     = 30 * time.Second
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

	pool, err := pgxpool.New(ctx, getEnv("DATABASE_URL", defaultDatabaseURL))
	if err != nil {
		return err
	}

	defer pool.Close()

	err = store.Migrate(ctx, pool, migrations.FS)
	if err != nil {
		return err
	}

	st := store.New(pool)
	if getEnv("SEED_DEMO", envTrue) == envTrue {
		err := st.SeedDemo(ctx)
		if err != nil {
			return err
		}
	}

	policies := policy.NewCache(st)

	redisTimeout, err := getEnvMillis("REDIS_TIMEOUT_MS", defaultRedisMS)
	if err != nil {
		return err
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:         getEnv("REDIS_ADDR", defaultRedisAddr),
		DialTimeout:  redisTimeout,
		ReadTimeout:  redisTimeout,
		WriteTimeout: redisTimeout,
	})
	defer rdb.Close() //nolint:errcheck // nothing to do on shutdown

	rebuildCounters(ctx, log, velocity.NewRebuilder(st, policies, rdb, countersMarkerKey))

	limiter := velocity.NewLimiter(rdb, redisTimeout)
	stages := append(controls.ListHardControls(), velocity.NewStage(limiter), scoring.Stage{})
	pl := pipeline.New(stages...)
	svc := authz.NewService(st, st, policies, pl).WithSignals(st)
	events := lifecycle.NewService(st, st, limiter, st, policies)

	srv := &http.Server{
		Addr:              ":" + getEnv("PORT", defaultPort),
		Handler:           api.NewHandler(api.Deps{Auth: svc, Audit: st, Policies: policies, Lifecycle: events, DB: st}, log),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
	}

	errc := make(chan error, 1)

	go func() {
		errc <- srv.ListenAndServe()
	}()

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

func getEnv(key, def string) string {
	value := os.Getenv(key)
	if value != "" {
		return value
	}

	return def
}

func getEnvMillis(key string, def int) (time.Duration, error) {
	ms, err := strconv.Atoi(getEnv(key, strconv.Itoa(def)))
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}

	return time.Duration(ms) * time.Millisecond, nil
}

// rebuildCounters restores the Redis counters from Postgres when Redis lost
// them, before the engine serves (D-055). A failure is logged, not fatal: the
// marker stays unset, so the next start tries again.
func rebuildCounters(ctx context.Context, log *slog.Logger, r *velocity.Rebuilder) {
	ctx, cancel := context.WithTimeout(ctx, rebuildTimeout)
	defer cancel()

	restored, err := r.RebuildIfLost(ctx)
	if err != nil {
		log.Warn("velocity counters not rebuilt", "err", err)
		return
	}

	log.Info("velocity counters ready", "rebuilt_authorizations", restored)
}
