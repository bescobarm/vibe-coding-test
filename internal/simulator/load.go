package simulator

import (
	"context"
	"fmt"
	"io"
	"math"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bescobarm/card-auth-engine/internal/domain"
)

const (
	// TargetP99 is the engine's latency goal under load (parent AC5).
	TargetP99     = 100 * time.Millisecond
	millisPerSec  = 1000
	percentile50  = 0.50
	percentile95  = 0.95
	percentile99  = 0.99
	verdictPass   = "PASS"
	verdictFail   = "FAIL"
	fmtLoadResult = "Load test · %d req/s for %s · %d workers\n" +
		"Sent %d · ok %d · errors %d · achieved %.0f req/s · peak in flight %d\n" +
		"Client latency: p50 %.1f ms · p95 %.1f ms · p99 %.1f ms\n" +
		"Engine latency: p50 %.1f ms · p95 %.1f ms · p99 %.1f ms → %s (p99 < %d ms)\n"
)

// LoadConfig paces requests to Rate per second for Duration, sent by Workers
// goroutines over Cards (D-035).
type LoadConfig struct {
	Rate     int
	Duration time.Duration
	Workers  int
	Cards    []string
}

// LoadResult is what a load test measured. Client latency includes HTTP;
// engine latency is the engine's own processing time.
type LoadResult struct {
	Config       LoadConfig
	Sent         int
	OK           int
	Errors       int
	Elapsed      time.Duration
	PeakInFlight int64
	Client       domain.LatencyPercentiles
	Engine       domain.LatencyPercentiles
}

type job struct {
	index   int
	request domain.AuthRequest
}

// RunLoad sends normal purchases at the configured rate and measures latency.
// Each request writes its latency into its own slot, so workers share no lock.
func RunLoad(ctx context.Context, run *Run, config LoadConfig) LoadResult {
	total := int(float64(config.Rate) * config.Duration.Seconds())
	clientMS := make([]float64, total)
	engineMS := make([]float64, total)
	failed := make([]bool, total)
	jobs := make(chan job)

	var (
		workers  sync.WaitGroup
		inFlight atomic.Int64
		peak     atomic.Int64
	)

	start := time.Now()

	for range config.Workers {
		workers.Go(func() {
			for next := range jobs {
				trackPeak(&peak, inFlight.Add(1))
				sent := time.Now()
				response, err := run.engine.Authorize(ctx, next.request)
				clientMS[next.index] = toMillis(time.Since(sent))
				failed[next.index] = err != nil
				engineMS[next.index] = response.LatencyMS
				inFlight.Add(-1)
			}
		})
	}

	sent := pace(ctx, run, config, total, jobs)
	workers.Wait()

	return summarizeLoad(config, sent, time.Since(start), peak.Load(), clientMS[:sent], engineMS[:sent], failed[:sent])
}

// pace builds each request and hands it to a worker on every tick. When all
// workers are busy the hand-off waits, so the achieved rate shows saturation.
func pace(ctx context.Context, run *Run, config LoadConfig, total int, jobs chan<- job) int {
	defer close(jobs)

	ticker := time.NewTicker(time.Second / time.Duration(config.Rate))
	defer ticker.Stop()

	for index := range total {
		select {
		case <-ctx.Done():
			return index
		case <-ticker.C:
		}

		cardID := config.Cards[index%len(config.Cards)]
		item := run.pickNormal(cardID)
		request := domain.AuthRequest{
			AuthorizationID: run.nextID("load"), CardID: cardID, AmountMinor: item.amountMinor, Currency: currency,
			Merchant:  domain.Merchant{ID: item.merchantID, Name: item.merchantID, MCC: item.mcc, Country: item.country},
			EntryMode: item.entryMode, CreatedAt: time.Now().UTC(),
		}

		jobs <- job{index: index, request: request}
	}

	return total
}

func trackPeak(peak *atomic.Int64, current int64) {
	for {
		seen := peak.Load()
		if current <= seen || peak.CompareAndSwap(seen, current) {
			return
		}
	}
}

func summarizeLoad(config LoadConfig, sent int, elapsed time.Duration, peak int64, clientMS, engineMS []float64, failed []bool) LoadResult {
	result := LoadResult{Config: config, Sent: sent, Elapsed: elapsed, PeakInFlight: peak}
	okEngine := make([]float64, 0, sent)

	for index := range sent {
		if failed[index] {
			result.Errors++
			continue
		}

		result.OK++
		okEngine = append(okEngine, engineMS[index])
	}

	result.Client = computePercentiles(clientMS)
	result.Engine = computePercentiles(okEngine)

	return result
}

// computePercentiles uses the nearest-rank method on a sorted copy.
func computePercentiles(values []float64) domain.LatencyPercentiles {
	if len(values) == 0 {
		return domain.LatencyPercentiles{}
	}

	sorted := slices.Clone(values)
	slices.Sort(sorted)

	rank := func(quantile float64) float64 {
		position := int(math.Ceil(quantile*float64(len(sorted)))) - 1
		return sorted[max(position, 0)]
	}

	return domain.LatencyPercentiles{P50: rank(percentile50), P95: rank(percentile95), P99: rank(percentile99)}
}

func toMillis(duration time.Duration) float64 {
	return float64(duration.Microseconds()) / millisPerSec
}

// Passed reports whether the engine met the p99 goal with no errors.
func (l LoadResult) Passed() bool {
	return l.Errors == 0 && l.OK > 0 && l.Engine.P99 < toMillis(TargetP99)
}

// Render prints the load result, with PASS or FAIL against the p99 goal (SP06 AC4).
func (l LoadResult) Render(out io.Writer) error {
	verdict := verdictFail
	if l.Passed() {
		verdict = verdictPass
	}

	achieved := float64(l.Sent) / l.Elapsed.Seconds()

	_, err := fmt.Fprintf(out, fmtLoadResult,
		l.Config.Rate, l.Config.Duration, l.Config.Workers,
		l.Sent, l.OK, l.Errors, achieved, l.PeakInFlight,
		l.Client.P50, l.Client.P95, l.Client.P99,
		l.Engine.P50, l.Engine.P95, l.Engine.P99, verdict, TargetP99.Milliseconds())

	return err
}

// PrepareLoad publishes the run's policy and creates its cards, and returns the card IDs.
func PrepareLoad(ctx context.Context, run *Run, cards CardCreator, policy []byte, count int) ([]string, error) {
	companyID := "sim-" + run.ID

	err := run.engine.PublishPolicy(ctx, companyID, policy)
	if err != nil {
		return nil, fmt.Errorf("publish policy: %w", err)
	}

	cardIDs := make([]string, count)
	for index := range count {
		cardIDs[index] = fmt.Sprintf("%s-load-%d", companyID, index)
	}

	err = cards.CreateCards(ctx, companyID, cardIDs)
	if err != nil {
		return nil, fmt.Errorf("create cards: %w", err)
	}

	return cardIDs, nil
}
