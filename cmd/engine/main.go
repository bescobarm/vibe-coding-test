// Command engine runs the card authorization engine.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/bescobarm/card-auth-engine/internal/api"
	"github.com/bescobarm/card-auth-engine/internal/authz"
	"github.com/bescobarm/card-auth-engine/internal/controls"
	"github.com/bescobarm/card-auth-engine/internal/explain"
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
	defaultDeadlineMS  = 500                       // D-031
	countersMarkerKey  = "velocity:counters_ready" // D-055
	rebuildTimeout     = 30 * time.Second
	envTrue            = "true"
	defaultMaxConns    = 32 // I-035: the pgx default, max(4, CPUs), queued requests at 1,000 req/s
	explainerMock      = "mock"
	explainerTemplate  = "template"

	readHeaderTimeout = 2 * time.Second
	readTimeout       = 5 * time.Second
	writeTimeout      = 5 * time.Second
	shutdownTimeout   = 5 * time.Second
)

var (
	errUnknownExplainer   = errors.New("EXPLAINER must be mock or template")
	errInvalidFailureRate = errors.New("must be a number from 0 to 1")
	errInvalidMaxConns    = errors.New("must be a positive number of connections")
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

	pool, err := newPool(ctx)
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
	weights, err := loadWeights()
	if err != nil {
		return err
	}

	stages := append(controls.ListHardControls(), velocity.NewStage(limiter), scoring.NewStage(weights))
	pl := pipeline.New(stages...)
	deadline, err := getEnvMillis("INTERNAL_DEADLINE_MS", defaultDeadlineMS)
	if err != nil {
		return err
	}

	explainer, err := newExplainer()
	if err != nil {
		return err
	}

	// D-028: the explainer runs inside the engine, on its own context, so
	// shutdown can stop it after the HTTP server and before the pool (D-033).
	worker := explain.NewWorker(st, explainer, log, explain.DefaultPoll)
	workerCtx, stopWorker := context.WithCancel(context.Background())

	var workerDone sync.WaitGroup

	workerDone.Go(func() {
		worker.Run(workerCtx)
	})

	stopExplainer := func() {
		stopWorker()
		workerDone.Wait()
	}

	svc := authz.NewService(st, st, policies, pl).WithSignals(st).WithDeadline(deadline).WithLogger(log).WithDoorbell(worker.Ring)
	events := lifecycle.NewService(st, st, limiter, st, policies)

	srv := &http.Server{
		Addr:              ":" + getEnv("PORT", defaultPort),
		Handler:           api.NewHandler(api.Deps{Auth: svc, Audit: st, Policies: policies, Lifecycle: events, Metrics: st, DB: st}, log),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
	}

	defer stopExplainer()

	return serve(ctx, log, srv, svc, stopExplainer)
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

// loadWeights reads each feature's score weight from its env var, with the
// defaults in code (D-057, D-065). An invalid value stops the engine.
func loadWeights() (scoring.Weights, error) {
	weights := scoring.GetDefaultWeights()
	fields := []struct {
		key    string
		target *int
	}{
		{"SCORE_WEIGHT_CARD_TESTING", &weights.CardTesting},
		{"SCORE_WEIGHT_SPLIT_PURCHASE", &weights.SplitPurchase},
		{"SCORE_WEIGHT_NEW_COUNTRY", &weights.NewCountry},
		{"SCORE_WEIGHT_NEW_MERCHANT_CNP", &weights.NewMerchantCNP},
		{"SCORE_WEIGHT_AMOUNT_ANOMALY", &weights.AmountAnomaly},
		{"SCORE_WEIGHT_ROUND_AMOUNT", &weights.RoundAmount},
	}

	for _, field := range fields {
		value, err := strconv.Atoi(getEnv(field.key, strconv.Itoa(*field.target)))
		if err != nil {
			return scoring.Weights{}, fmt.Errorf("%s: %w", field.key, err)
		}

		*field.target = value
	}

	return weights, weights.Validate()
}

// serve runs the HTTP server until it fails or ctx ends, then shuts down in
// order (D-033): stop taking requests, then let late fallbacks finish writing
// before the deferred pool and Redis closes run.
func serve(ctx context.Context, log *slog.Logger, srv *http.Server, svc *authz.Service, stopExplainer func()) error {
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

		return nil
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		err := srv.Shutdown(shutdownCtx)
		if err != nil {
			return err
		}

		err = svc.Wait(shutdownCtx)
		stopExplainer()

		return err
	}
}

// newExplainer picks the explainer from EXPLAINER: "mock" (default, a local
// mocked LLM, D-013) or "template". MOCK_LLM_FAILURE_RATE (0 to 1) makes the
// mock invent a reason code, to show the validator and the fallback.
func newExplainer() (explain.Explainer, error) {
	mode := getEnv("EXPLAINER", explainerMock)
	if mode == explainerTemplate {
		return explain.TemplateExplainer{}, nil
	}

	if mode != explainerMock {
		return nil, fmt.Errorf("%w: %q", errUnknownExplainer, mode)
	}

	rate, err := strconv.ParseFloat(getEnv("MOCK_LLM_FAILURE_RATE", "0"), 64)
	if err != nil || rate < 0 || rate > 1 {
		return nil, fmt.Errorf("%w: MOCK_LLM_FAILURE_RATE", errInvalidFailureRate)
	}

	return explain.NewLLMExplainer(explain.NewMockLLM(rate)), nil
}

// newPool connects to Postgres with DATABASE_MAX_CONNS connections (default
// 32). The pgx default, max(4, CPUs), was 12 here: at 1,000 req/s any pause
// queued requests for a connection and p99 spiked past 100 ms (I-035).
func newPool(ctx context.Context) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(getEnv("DATABASE_URL", defaultDatabaseURL))
	if err != nil {
		return nil, err
	}

	maxConns, err := strconv.Atoi(getEnv("DATABASE_MAX_CONNS", strconv.Itoa(defaultMaxConns)))
	if err != nil || maxConns <= 0 || maxConns > math.MaxInt32 {
		return nil, fmt.Errorf("%w: DATABASE_MAX_CONNS", errInvalidMaxConns)
	}

	config.MaxConns = int32(maxConns)

	return pgxpool.NewWithConfig(ctx, config)
}
