// Command simulator drives a running engine (SP06): `-mode report` plays the
// labeled scenarios and prints the detection report; `-mode load` measures
// latency at a fixed rate (D-035). Start the engine first (`make run`).
package main

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bescobarm/card-auth-engine/internal/simulator"
)

// policy is the simulated company's policy: the demo controls, card-testing
// and daily rules, risk thresholds and a fail-open fallback.
//
//go:embed policy.json
var policy []byte

const (
	defaultEngineURL   = "http://localhost:8080"
	defaultDatabaseURL = "postgres://auth:auth@localhost:55432/auth?sslmode=disable"
	modeReport         = "report"
	modeLoad           = "load"
	pathHealth         = "/healthz"
	runIDBytes         = 3
	requestTimeout     = 5 * time.Second
	readyTimeout       = 30 * time.Second
	readyPause         = 300 * time.Millisecond
	exitFailure        = 1

	defaultSeed        = 42
	defaultNormalCards = 20
	defaultFraudCards  = 3
	defaultRate        = 200
	defaultDuration    = 15 * time.Second
	defaultWorkers     = 32
	defaultLoadCards   = 200
)

var (
	errUnknownMode     = errors.New("mode must be report or load")
	errEngineNotReady  = errors.New("engine did not answer /healthz; start it with `make run`")
	errLoadMissedGoal  = errors.New("load test missed the p99 goal or had errors")
	errInvalidLoadArgs = errors.New("rate, workers and cards must be positive")
)

type options struct {
	engineURL, databaseURL, mode string
	seed                         uint64
	normalCards, fraudCards      int
	rate, workers, loadCards     int
	duration                     time.Duration
}

func main() {
	opts := options{}
	flag.StringVar(&opts.engineURL, "engine", defaultEngineURL, "engine base URL")
	flag.StringVar(&opts.databaseURL, "database", getEnv("DATABASE_URL", defaultDatabaseURL), "engine database, to create the run's cards")
	flag.StringVar(&opts.mode, "mode", modeReport, "report or load")
	flag.Uint64Var(&opts.seed, "seed", defaultSeed, "random seed: the same seed sends the same traffic")
	flag.IntVar(&opts.normalCards, "normal-cards", defaultNormalCards, "cards with normal traffic")
	flag.IntVar(&opts.fraudCards, "fraud-cards", defaultFraudCards, "cards per fraud scenario")
	flag.IntVar(&opts.rate, "rate", defaultRate, "load: requests per second")
	flag.DurationVar(&opts.duration, "duration", defaultDuration, "load: how long to send")
	flag.IntVar(&opts.workers, "workers", defaultWorkers, "load: concurrent workers")
	flag.IntVar(&opts.loadCards, "load-cards", defaultLoadCards, "load: cards the traffic is spread over")
	flag.Parse()

	err := run(opts)
	if err != nil {
		fmt.Fprintln(os.Stderr, "simulator:", err)
		os.Exit(exitFailure)
	}
}

func run(opts options) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	client := &http.Client{Timeout: requestTimeout}

	err := waitForEngine(ctx, client, opts.engineURL)
	if err != nil {
		return err
	}

	pool, err := pgxpool.New(ctx, opts.databaseURL)
	if err != nil {
		return err
	}

	defer pool.Close()

	runID, err := newRunID()
	if err != nil {
		return err
	}

	sim := simulator.NewRun(runID, opts.seed, simulator.NewHTTPEngine(opts.engineURL, client))
	cards := simulator.NewPostgresCards(pool)

	switch opts.mode {
	case modeReport:
		return runReport(ctx, sim, cards, opts)
	case modeLoad:
		return runLoad(ctx, sim, cards, opts)
	default:
		return errUnknownMode
	}
}

func runReport(ctx context.Context, sim *simulator.Run, cards simulator.CardCreator, opts options) error {
	report, err := simulator.RunReport(ctx, sim, cards, policy, simulator.ListScenarios(opts.normalCards, opts.fraudCards))
	if err != nil {
		return err
	}

	return report.Render(os.Stdout)
}

func runLoad(ctx context.Context, sim *simulator.Run, cards simulator.CardCreator, opts options) error {
	if opts.rate <= 0 || opts.workers <= 0 || opts.loadCards <= 0 {
		return errInvalidLoadArgs
	}

	cardIDs, err := simulator.PrepareLoad(ctx, sim, cards, policy, opts.loadCards)
	if err != nil {
		return err
	}

	result := simulator.RunLoad(ctx, sim, simulator.LoadConfig{
		Rate: opts.rate, Duration: opts.duration, Workers: opts.workers, Cards: cardIDs,
	})

	err = result.Render(os.Stdout)
	if err != nil {
		return err
	}

	if !result.Passed() {
		return errLoadMissedGoal
	}

	return nil
}

// waitForEngine polls /healthz with a pause and a deadline, and fails loudly
// instead of sending traffic to an engine that isn't listening (I-015).
func waitForEngine(ctx context.Context, client *http.Client, engineURL string) error {
	ctx, cancel := context.WithTimeout(ctx, readyTimeout)
	defer cancel()

	for {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, engineURL+pathHealth, http.NoBody)
		if err != nil {
			return err
		}

		response, err := client.Do(request)
		if err == nil {
			closeErr := response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return closeErr
			}
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("%w (%s)", errEngineNotReady, engineURL)
		case <-time.After(readyPause):
		}
	}
}

func newRunID() (string, error) {
	random := make([]byte, runIDBytes)

	_, err := rand.Read(random)
	if err != nil {
		return "", err
	}

	return hex.EncodeToString(random), nil
}

func getEnv(key, fallback string) string {
	value := os.Getenv(key)
	if value != "" {
		return value
	}

	return fallback
}
