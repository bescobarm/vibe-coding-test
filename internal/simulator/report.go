package simulator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// CardCreator creates the run's cards (D-066): the API can't create cards,
// and fresh cards keep one run's counters out of the next.
type CardCreator interface {
	CreateCards(ctx context.Context, companyID string, cardIDs []string) error
}

// Outcome is how the engine decided one scenario, all cards together.
type Outcome struct {
	Scenario     Scenario
	Requests     int
	Approved     int
	Declined     int
	Flagged      int
	Errors       int
	Fallbacks    int
	LatencyMS    []float64 // the engine's processing time per answer
	Caught       int       // fraud cards caught, or checks passed
	FlaggedFrom  [2]int
	DeclinedFrom [2]int // first attempt declined: earliest and latest across cards
}

// Report is the result of a run. Latency comes from the run's own answers,
// so traffic from anything else hitting the engine can't skew it.
type Report struct {
	Seed     uint64
	RunID    string
	Outcomes []Outcome
}

const (
	pctScale     = 100
	rangeFrom    = 0
	rangeTo      = 1
	reportHeader = "Scenario\tCards\tRequests\tExpected\tResult"
	columnPad    = 2
)

// RunReport plays every scenario on fresh cards of a fresh company and
// summarizes the decisions (SP06 AC1). Scenarios run one after another, so the
// same seed sends the same traffic (AC2).
func RunReport(ctx context.Context, run *Run, cards CardCreator, policy json.RawMessage, scenarios []Scenario) (Report, error) {
	companyID := "sim-" + run.ID

	err := run.engine.PublishPolicy(ctx, companyID, policy)
	if err != nil {
		return Report{}, fmt.Errorf("publish policy: %w", err)
	}

	cardIDs := make([][]string, len(scenarios))

	var all []string

	for index, scenario := range scenarios {
		for number := range scenario.Cards {
			cardID := fmt.Sprintf("%s-%s-%d", companyID, strings.ReplaceAll(scenario.Name, " ", "-"), number)
			cardIDs[index] = append(cardIDs[index], cardID)
			all = append(all, cardID)
		}
	}

	err = cards.CreateCards(ctx, companyID, all)
	if err != nil {
		return Report{}, fmt.Errorf("create cards: %w", err)
	}

	report := Report{Seed: run.Seed, RunID: run.ID}

	for index, scenario := range scenarios {
		report.Outcomes = append(report.Outcomes, playScenario(ctx, run, scenario, cardIDs[index]))
	}

	return report, nil
}

func playScenario(ctx context.Context, run *Run, scenario Scenario, cardIDs []string) Outcome {
	outcome := Outcome{Scenario: scenario}

	for _, cardID := range cardIDs {
		result := scenario.play(ctx, run, cardID)
		outcome.Requests += result.requests
		outcome.Approved += result.approved
		outcome.Declined += result.declined
		outcome.Flagged += result.flagged
		outcome.Errors += result.errors
		outcome.Fallbacks += result.fallbacks
		outcome.LatencyMS = append(outcome.LatencyMS, result.latencyMS...)

		if result.caught || result.passed {
			outcome.Caught++
		}

		outcome.FlaggedFrom = widenRange(outcome.FlaggedFrom, result.firstFlagged)
		outcome.DeclinedFrom = widenRange(outcome.DeclinedFrom, result.firstDeclined)
	}

	return outcome
}

// widenRange keeps the earliest and latest attempt seen; 0 means "never".
func widenRange(span [2]int, attempt int) [2]int {
	if attempt == 0 {
		return span
	}

	if span[rangeFrom] == 0 || attempt < span[rangeFrom] {
		span[rangeFrom] = attempt
	}

	if attempt > span[rangeTo] {
		span[rangeTo] = attempt
	}

	return span
}

// errWriter keeps the first write error, so a report is many prints and one check.
type errWriter struct {
	out io.Writer
	err error
}

func (e *errWriter) printf(format string, args ...any) {
	if e.err != nil {
		return
	}

	_, e.err = fmt.Fprintf(e.out, format, args...)
}

// Render prints the report on one screen (SP06 R3).
func (r Report) Render(out io.Writer) error {
	table := tabwriter.NewWriter(out, 0, 0, columnPad, ' ', 0)
	printer := &errWriter{out: table}

	printer.printf("Simulator report · seed %d (replay with -seed %d) · run %s\n\n", r.Seed, r.Seed, r.RunID)
	printer.printf("%s\n", reportHeader)

	for _, outcome := range r.Outcomes {
		printer.printf("%s\t%d\t%d\t%s\t%s\n", outcome.Scenario.Name, outcome.Scenario.Cards, outcome.Requests,
			outcome.Scenario.Expected, describeOutcome(outcome))
	}

	if printer.err != nil {
		return printer.err
	}

	err := table.Flush()
	if err != nil {
		return err
	}

	caught, fraud, falseDeclines, normal := summarize(r.Outcomes)
	latencies, fallbacks := collectLatency(r.Outcomes)
	latency := computePercentiles(latencies)
	summary := &errWriter{out: out}

	summary.printf("\nDetection: %d/%d fraud cards caught (%s) · False declines: %d/%d normal requests (%s)\n"+
		"Engine latency: p50 %.1f ms · p95 %.1f ms · p99 %.1f ms · %d decisions · %d fallbacks\n",
		caught, fraud, percentOf(caught, fraud), falseDeclines, normal, percentOf(falseDeclines, normal),
		latency.P50, latency.P95, latency.P99, len(latencies), fallbacks)

	return summary.err
}

func describeOutcome(outcome Outcome) string {
	scenario := outcome.Scenario
	errors := ""

	if outcome.Errors > 0 {
		errors = fmt.Sprintf(" · %d errors", outcome.Errors)
	}

	switch {
	case scenario.Check:
		return fmt.Sprintf("%d/%d passed%s", outcome.Caught, scenario.Cards, errors)
	case !scenario.Fraud:
		return fmt.Sprintf("%d approved, %d declined, %d flagged%s", outcome.Approved, outcome.Declined, outcome.Flagged, errors)
	}

	detail := fmt.Sprintf("%d/%d caught", outcome.Caught, scenario.Cards)
	detail += describeRange(" · flagged from", outcome.FlaggedFrom)
	detail += describeRange(" · declined from", outcome.DeclinedFrom)

	return detail + errors
}

func describeRange(label string, span [2]int) string {
	if span[rangeFrom] == 0 {
		return ""
	}

	if span[rangeFrom] == span[rangeTo] {
		return fmt.Sprintf("%s #%d", label, span[rangeFrom])
	}

	return fmt.Sprintf("%s #%d–#%d", label, span[rangeFrom], span[rangeTo])
}

// summarize returns fraud cards caught, fraud cards, false declines and normal requests.
func summarize(outcomes []Outcome) (int, int, int, int) {
	var caught, fraud, falseDeclines, normal int

	for _, outcome := range outcomes {
		switch {
		case outcome.Scenario.Fraud:
			caught += outcome.Caught
			fraud += outcome.Scenario.Cards
		case !outcome.Scenario.Check:
			falseDeclines += outcome.Declined
			normal += outcome.Requests
		}
	}

	return caught, fraud, falseDeclines, normal
}

func collectLatency(outcomes []Outcome) ([]float64, int) {
	var (
		latencies []float64
		fallbacks int
	)

	for _, outcome := range outcomes {
		latencies = append(latencies, outcome.LatencyMS...)
		fallbacks += outcome.Fallbacks
	}

	return latencies, fallbacks
}

func percentOf(part, total int) string {
	if total == 0 {
		return "n/a"
	}

	return fmt.Sprintf("%.1f%%", float64(part)*pctScale/float64(total))
}
