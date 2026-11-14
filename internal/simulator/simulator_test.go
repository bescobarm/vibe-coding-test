package simulator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bescobarm/card-auth-engine/internal/domain"
	"github.com/bescobarm/card-auth-engine/internal/lifecycle"
)

var errEngineDown = errors.New("engine down")

// scriptedEngine answers like a simplified engine: it declines gambling, the
// 6th small charge on a card and a purchase abroad; it flags the 2nd large
// purchase at the same merchant. That is enough to exercise every scenario.
type scriptedEngine struct {
	mu        sync.Mutex
	small     map[string]int
	large     map[string]int
	rooms     map[string]int64
	published []string
	authErr   error
	eventErr  error
	policyErr error
}

func newScriptedEngine() *scriptedEngine {
	return &scriptedEngine{small: map[string]int{}, large: map[string]int{}, rooms: map[string]int64{}}
}

func (s *scriptedEngine) Authorize(_ context.Context, request domain.AuthRequest) (domain.AuthResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.authErr != nil {
		return domain.AuthResponse{}, s.authErr
	}

	response := domain.AuthResponse{AuthorizationID: request.AuthorizationID, Decision: domain.Approve, LatencyMS: 2}
	s.rooms[request.AuthorizationID] = request.AmountMinor

	switch {
	case slices.Contains(blockedMCCs, request.Merchant.MCC), request.Merchant.Country != countryHome:
		response.Decision = domain.Decline
	case request.AmountMinor <= 500:
		s.small[request.CardID]++
		response.Flagged = s.small[request.CardID] >= 4
		if s.small[request.CardID] >= 6 {
			response.Decision, response.Flagged = domain.Decline, false
		}
	case request.AmountMinor >= splitMinMinor:
		s.large[request.CardID]++
		response.Flagged = s.large[request.CardID] >= 2
	}

	return response, nil
}

func (s *scriptedEngine) ApplyEvent(_ context.Context, authorizationID, _ string, body EventBody) (lifecycle.Result, error) {
	if s.eventErr != nil {
		return lifecycle.Result{}, s.eventErr
	}

	return lifecycle.Result{
		AuthorizationID: authorizationID, Decision: domain.Approve, Status: lifecycle.StatusPending,
		HeldMinor: s.rooms[authorizationID] + *body.AmountMinor,
	}, nil
}

func (s *scriptedEngine) PublishPolicy(_ context.Context, companyID string, _ json.RawMessage) error {
	s.published = append(s.published, companyID)
	return s.policyErr
}

type fakeCards struct {
	err     error
	company string
	created []string
}

func (f *fakeCards) CreateCards(_ context.Context, companyID string, cardIDs []string) error {
	f.company, f.created = companyID, cardIDs
	return f.err
}

func runReport(t *testing.T, engine Engine, cards CardCreator) (Report, error) {
	t.Helper()
	return RunReport(context.Background(), NewRun("r1", 42, engine), cards, json.RawMessage(`{}`), ListScenarios(4, 2))
}

// AC1 of SP06: every scenario is played and summarized.
func TestTheReportCoversEveryScenario(t *testing.T) {
	c := require.New(t)
	engine, cards := newScriptedEngine(), &fakeCards{}

	report, err := runReport(t, engine, cards)

	c.NoError(err)
	c.Equal([]string{"sim-r1"}, engine.published)
	c.Equal("sim-r1", cards.company)
	c.Len(cards.created, 4+5*2)
	c.Len(report.Outcomes, 6)

	normal, cardTesting, split, stolen, blocked, hotel := report.Outcomes[0], report.Outcomes[1], report.Outcomes[2],
		report.Outcomes[3], report.Outcomes[4], report.Outcomes[5]
	c.Equal(normal.Requests, normal.Approved)
	c.Zero(normal.Declined)
	c.GreaterOrEqual(normal.Requests, 4*normalMinRequests)
	c.LessOrEqual(normal.Requests, 4*normalMaxRequests)
	c.Equal(2, cardTesting.Caught)
	c.Equal([2]int{4, 4}, cardTesting.FlaggedFrom)
	c.Equal([2]int{6, 6}, cardTesting.DeclinedFrom, "the 6th small charge, however many a case sends")
	c.Equal(2, split.Caught)
	c.Equal(2, stolen.Caught)
	c.GreaterOrEqual(stolen.DeclinedFrom[0], stolenMinHistory+1)
	c.LessOrEqual(stolen.DeclinedFrom[1], stolenMaxHistory+1)
	c.Equal(2, blocked.Caught)
	c.Equal(2, hotel.Caught)
}

// The dashboard words results itself, so each row carries the counts too.
func TestARowCarriesTheCountsBehindItsResult(t *testing.T) {
	c := require.New(t)

	report, err := runReport(t, newScriptedEngine(), &fakeCards{})

	c.NoError(err)
	rows := report.ListRows()
	cardTesting, hotel := rows[1], rows[5]
	c.True(cardTesting.Fraud)
	c.False(cardTesting.Check)
	c.Equal(2, cardTesting.Caught)
	c.Equal([2]int{4, 4}, cardTesting.FlaggedFrom)
	c.Equal([2]int{6, 6}, cardTesting.DeclinedFrom)
	c.Equal(report.Outcomes[1].Declined, cardTesting.Declined)
	c.True(hotel.Check)
	c.Equal(2, hotel.Caught)
	c.Equal(report.Outcomes[0].Approved, rows[0].Approved)
}

// AC2 of SP06: the same seed sends the same traffic.
func TestTheSameSeedSendsTheSameTraffic(t *testing.T) {
	c := require.New(t)
	first, second := NewRun("r1", 7, newScriptedEngine()), NewRun("r2", 7, newScriptedEngine())

	var amountsFirst, amountsSecond []int64
	for range 20 {
		amountsFirst = append(amountsFirst, first.pickNormal("card-1").amountMinor)
		amountsSecond = append(amountsSecond, second.pickNormal("card-1").amountMinor)
	}

	c.Equal(amountsFirst, amountsSecond)
}

// D-066: each seed plays its own cases; the same seed replays the same report.
func TestDifferentSeedsPlayDifferentCases(t *testing.T) {
	c := require.New(t)
	scenarios := ListScenarios(4, 2)

	first, err := RunReport(context.Background(), NewRun("r1", 1, newScriptedEngine()), &fakeCards{}, json.RawMessage(`{}`), scenarios)
	c.NoError(err)
	replay, err := RunReport(context.Background(), NewRun("r1", 1, newScriptedEngine()), &fakeCards{}, json.RawMessage(`{}`), scenarios)
	c.NoError(err)
	other, err := RunReport(context.Background(), NewRun("r1", 2, newScriptedEngine()), &fakeCards{}, json.RawMessage(`{}`), scenarios)
	c.NoError(err)

	c.Equal(renderReport(c, first), renderReport(c, replay))
	c.NotEqual(renderReport(c, first), renderReport(c, other))
}

// renderReport compares reports as text: an Outcome holds its scenario's play
// function, and functions are never equal in Go.
func renderReport(c *require.Assertions, report Report) string {
	var out bytes.Buffer
	c.NoError(report.Render(&out))
	return strings.ReplaceAll(out.String(), fmt.Sprintf("seed %d (replay with -seed %d)", report.Seed, report.Seed), "seed")
}

func TestEveryCaseStaysInsideItsRange(t *testing.T) {
	c := require.New(t)
	run := NewRun("r1", 3, newScriptedEngine())

	for range 500 {
		value := run.between(splitMinMinor, splitMaxMinor)
		c.GreaterOrEqual(value, int64(splitMinMinor))
		c.LessOrEqual(value, int64(splitMaxMinor))
		c.Contains(countriesAbroad, run.pickFrom(countriesAbroad))
	}
}

func TestNormalAmountsAreNeverRound(t *testing.T) {
	c := require.New(t)
	run := NewRun("r1", 1, newScriptedEngine())

	for range 200 {
		c.NotZero(run.pickNormal("card-1").amountMinor%10000, "a round amount would trip ROUND_AMOUNT on normal traffic")
	}
}

func TestTheReportRendersOnOneScreen(t *testing.T) {
	c := require.New(t)
	report, err := runReport(t, newScriptedEngine(), &fakeCards{})
	c.NoError(err)
	var out bytes.Buffer

	c.NoError(report.Render(&out))

	text := out.String()
	c.Contains(text, "Simulator report · seed 42 (replay with -seed 42) · run r1")
	c.Contains(text, "2/2 caught · flagged from #4 · declined from #6")
	c.Contains(text, "approve, hold raised  2/2 passed")
	c.Contains(text, "blocked MCC")
	c.Contains(text, "Detection: 8/8 fraud cards caught (100.0%) · False declines: 0/")
	c.Contains(text, "Engine latency: p50 2.0 ms · p95 2.0 ms · p99 2.0 ms ·")
	c.Less(strings.Count(text, "\n"), 15)
}

func TestEngineErrorsAreCountedNotHidden(t *testing.T) {
	c := require.New(t)
	engine := newScriptedEngine()
	engine.authErr = errEngineDown
	report, err := runReport(t, engine, &fakeCards{})
	c.NoError(err)
	var out bytes.Buffer

	c.NoError(report.Render(&out))

	c.Equal(32, report.Outcomes[0].Errors)
	c.Contains(out.String(), "0 approved, 0 declined, 0 flagged · 32 errors")
}

func TestAFailedIncrementFailsTheHotelCheck(t *testing.T) {
	c := require.New(t)
	engine := newScriptedEngine()
	engine.eventErr = errEngineDown

	report, err := runReport(t, engine, &fakeCards{})

	c.NoError(err)
	c.Zero(report.Outcomes[5].Caught)
	c.Equal(2, report.Outcomes[5].Errors)
}

func TestTheReportStopsWhenThePolicyCannotBePublished(t *testing.T) {
	c := require.New(t)
	engine := newScriptedEngine()
	engine.policyErr = errEngineDown

	_, err := runReport(t, engine, &fakeCards{})

	c.ErrorIs(err, errEngineDown)
}

func TestTheReportStopsWhenCardsCannotBeCreated(t *testing.T) {
	c := require.New(t)

	_, err := runReport(t, newScriptedEngine(), &fakeCards{err: errEngineDown})

	c.ErrorIs(err, errEngineDown)
}

func TestARangeShowsEarliestAndLatestAttempts(t *testing.T) {
	c := require.New(t)
	span := widenRange(widenRange(widenRange([2]int{}, 5), 0), 3)

	c.Equal([2]int{3, 5}, span)
	c.Equal(" · declined from #3–#5", describeRange(" · declined from", span))
	c.Empty(describeRange(" · declined from", [2]int{}))
}

func TestPercentOfNothingIsNotAvailable(t *testing.T) {
	c := require.New(t)

	c.Equal("n/a", percentOf(0, 0))
	c.Equal("25.0%", percentOf(1, 4))
}

func TestPercentilesUseTheNearestRank(t *testing.T) {
	c := require.New(t)
	values := make([]float64, 100)
	for index := range values {
		values[index] = float64(100 - index)
	}

	c.Equal(domain.LatencyPercentiles{P50: 50, P95: 95, P99: 99}, computePercentiles(values))
	c.Equal(domain.LatencyPercentiles{}, computePercentiles(nil))
}

// AC6 of SP06 (D-035): a fixed pool paced to the rate; under synctest the
// pacing is exact.
func TestLoadIsPacedToTheRate(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := require.New(t)
		run := NewRun("r1", 1, newScriptedEngine())
		start := time.Now()

		result := RunLoad(context.Background(), run, LoadConfig{Rate: 50, Duration: 2 * time.Second, Workers: 4, Cards: []string{"a", "b"}})

		c.Equal(100, result.Sent)
		c.Equal(100, result.OK)
		c.Zero(result.Errors)
		c.Equal(2*time.Second, time.Since(start))
		c.Equal(2.0, result.Engine.P99)
		c.True(result.Passed())
		c.Positive(result.PeakInFlight)
	})
}

func TestLoadWithErrorsFails(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := require.New(t)
		engine := newScriptedEngine()
		engine.authErr = errEngineDown
		var out bytes.Buffer

		result := RunLoad(context.Background(), NewRun("r1", 1, engine), LoadConfig{Rate: 10, Duration: time.Second, Workers: 2, Cards: []string{"a"}})

		c.Equal(10, result.Errors)
		c.False(result.Passed())
		c.NoError(result.Render(&out))
		c.Contains(out.String(), "→ FAIL (p99 < 100 ms)")
	})
}

func TestLoadStopsWhenItsContextEnds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := require.New(t)
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()

		result := RunLoad(ctx, NewRun("r1", 1, newScriptedEngine()), LoadConfig{Rate: 10, Duration: 10 * time.Second, Workers: 2, Cards: []string{"a"}})

		c.Equal(5, result.Sent)
	})
}

func TestARenderedLoadResultSaysPass(t *testing.T) {
	c := require.New(t)
	result := LoadResult{
		Config: LoadConfig{Rate: 200, Duration: 10 * time.Second, Workers: 32}, Sent: 2000, OK: 2000,
		Elapsed: 10 * time.Second, PeakInFlight: 6, Engine: domain.LatencyPercentiles{P50: 1, P95: 2, P99: 4.3},
	}
	var out bytes.Buffer

	c.NoError(result.Render(&out))

	c.Contains(out.String(), "Sent 2000 · ok 2000 · errors 0 · achieved 200 req/s · peak in flight 6")
	c.Contains(out.String(), "p99 4.3 ms → PASS (p99 < 100 ms)")
}

func TestPrepareLoadCreatesTheCards(t *testing.T) {
	c := require.New(t)
	engine, cards := newScriptedEngine(), &fakeCards{}

	cardIDs, err := PrepareLoad(context.Background(), NewRun("r1", 1, engine), cards, []byte(`{}`), 3)

	c.NoError(err)
	c.Equal([]string{
		"sim-r1-load-0",
		"sim-r1-load-1",
		"sim-r1-load-2",
	}, cardIDs)
	c.Equal(cardIDs, cards.created)
}

func TestPrepareLoadStopsOnAPolicyError(t *testing.T) {
	c := require.New(t)
	engine := newScriptedEngine()
	engine.policyErr = errEngineDown

	_, err := PrepareLoad(context.Background(), NewRun("r1", 1, engine), &fakeCards{}, []byte(`{}`), 3)

	c.ErrorIs(err, errEngineDown)
}

func TestPrepareLoadStopsOnACardError(t *testing.T) {
	c := require.New(t)

	_, err := PrepareLoad(context.Background(), NewRun("r1", 1, newScriptedEngine()), &fakeCards{err: errEngineDown}, []byte(`{}`), 3)

	c.ErrorIs(err, errEngineDown)
}

// The HTTP engine against a fake server: paths, bodies and status handling.
func TestTheHTTPEngineCallsTheAPI(t *testing.T) {
	c := require.New(t)
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		paths = append(paths, request.Method+" "+request.URL.Path)
		switch request.URL.Path {
		case "/v1/companies/sim-r1/policy":
			writer.WriteHeader(http.StatusCreated)
		case "/v1/authorizations":
			c.NoError(json.NewEncoder(writer).Encode(domain.AuthResponse{AuthorizationID: "a1", Decision: domain.Approve}))
		default:
			c.NoError(json.NewEncoder(writer).Encode(lifecycle.Result{AuthorizationID: "a1", HeldMinor: 45000}))
		}
	}))
	defer server.Close()
	engine := NewHTTPEngine(server.URL, server.Client())
	extra := int64(15000)

	c.NoError(engine.PublishPolicy(context.Background(), "sim-r1", json.RawMessage(`{}`)))
	response, err := engine.Authorize(context.Background(), domain.AuthRequest{AuthorizationID: "a1"})
	c.NoError(err)
	result, err := engine.ApplyEvent(context.Background(), "a1", "increment", EventBody{EventID: "e1", AmountMinor: &extra})
	c.NoError(err)

	c.Equal(domain.Approve, response.Decision)
	c.Equal(int64(45000), result.HeldMinor)
	c.Equal([]string{
		"PUT /v1/companies/sim-r1/policy",
		"POST /v1/authorizations",
		"POST /v1/authorizations/a1/increment",
	}, paths)
}

func TestTheHTTPEngineReportsAnUnexpectedStatus(t *testing.T) {
	c := require.New(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusInternalServerError)
		_, err := writer.Write([]byte(`{"error": "authorization could not be decided"}`))
		c.NoError(err)
	}))
	defer server.Close()

	_, err := NewHTTPEngine(server.URL, server.Client()).Authorize(context.Background(), domain.AuthRequest{})

	c.ErrorIs(err, ErrUnexpectedStatus)
	c.ErrorContains(err, "500")
	c.ErrorContains(err, "authorization could not be decided")
}

func TestTheHTTPEngineReportsAnUnreachableEngine(t *testing.T) {
	c := require.New(t)

	err := NewHTTPEngine("http://localhost:1", http.DefaultClient).PublishPolicy(context.Background(), "x", json.RawMessage(`{}`))

	c.ErrorContains(err, "PUT /v1/companies/x/policy")
}

func TestTheHTTPEngineRejectsABodyItCannotEncode(t *testing.T) {
	c := require.New(t)

	err := NewHTTPEngine("http://localhost:1", http.DefaultClient).PublishPolicy(context.Background(), "x", json.RawMessage(`{`))

	c.ErrorContains(err, "encode")
}

func TestTheHTTPEngineRejectsABadURL(t *testing.T) {
	c := require.New(t)

	_, err := NewHTTPEngine("http://bad host", http.DefaultClient).Authorize(context.Background(), domain.AuthRequest{})

	c.ErrorContains(err, "build")
}
