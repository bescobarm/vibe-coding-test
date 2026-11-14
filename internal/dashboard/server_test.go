package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bescobarm/card-auth-engine/internal/domain"
	"github.com/bescobarm/card-auth-engine/internal/lifecycle"
	"github.com/bescobarm/card-auth-engine/internal/simulator"
)

var errEngineDown = errors.New("engine down")

// fakeEngine approves everything except gambling, like a tiny engine.
type fakeEngine struct {
	mu        sync.Mutex
	authErr   error
	policyErr error
	block     chan struct{} // when set, Authorize waits on it
	entered   chan struct{} // when set, Authorize signals it has started
	sent      []domain.AuthRequest
}

func (f *fakeEngine) Authorize(_ context.Context, request domain.AuthRequest) (domain.AuthResponse, error) {
	if f.entered != nil {
		select {
		case f.entered <- struct{}{}:
		default:
		}
	}

	if f.block != nil {
		<-f.block
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	if f.authErr != nil {
		return domain.AuthResponse{}, f.authErr
	}

	f.sent = append(f.sent, request)
	response := domain.AuthResponse{AuthorizationID: request.AuthorizationID, Decision: domain.Approve, LatencyMS: 1.5}

	if request.Merchant.MCC == "7995" {
		response.Decision = domain.Decline
		response.Reasons = []domain.Reason{{Code: domain.ReasonMCCBlocked, Message: "blocked"}}
	}

	return response, nil
}

func (f *fakeEngine) ApplyEvent(_ context.Context, authorizationID, _ string, body simulator.EventBody) (lifecycle.Result, error) {
	return lifecycle.Result{AuthorizationID: authorizationID, Decision: domain.Approve, Status: lifecycle.StatusPending,
		HeldMinor: *body.AmountMinor}, nil
}

func (f *fakeEngine) PublishPolicy(context.Context, string, json.RawMessage) error {
	return f.policyErr
}

type fakeCards struct{}

func (fakeCards) CreateCards(context.Context, string, []string) error {
	return nil
}

func newTestServer(t *testing.T, engine simulator.Engine, engineURL string) http.Handler {
	t.Helper()
	server, err := NewServer(Config{
		Engine: engine, Cards: fakeCards{}, EngineURL: engineURL, Client: http.DefaultClient,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	require.NoError(t, err)
	return server.Handler()
}

func doRequest(handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(method, path, strings.NewReader(body)))
	return recorder
}

func decodeError(c *require.Assertions, recorder *httptest.ResponseRecorder) string {
	var body map[string]string
	c.NoError(json.Unmarshal(recorder.Body.Bytes(), &body))
	return body["error"]
}

const gamblingBatch = `{"card_id": "card_123", "amount_minor": 5000, "currency": "USD", "mcc": "7995",
	"merchant_id": "m_casino", "country": "US", "entry_mode": "ecommerce", "count": 3}`

func TestThePageIsServed(t *testing.T) {
	c := require.New(t)

	recorder := doRequest(newTestServer(t, &fakeEngine{}, ""), http.MethodGet, "/", "")

	c.Equal(http.StatusOK, recorder.Code)
	c.Equal("text/html; charset=utf-8", recorder.Header().Get("Content-Type"))
	c.Contains(recorder.Body.String(), `id="tab-overview"`)
	c.Contains(recorder.Body.String(), `id="tab-activity"`)
}

// A batch sends the same transaction N times, each with its own ID, and the
// feed shows them newest first, tagged "manual".
func TestABatchIsSentAndShownInTheFeed(t *testing.T) {
	c := require.New(t)
	engine := &fakeEngine{}
	handler := newTestServer(t, engine, "")

	recorder := doRequest(handler, http.MethodPost, "/api/transactions", gamblingBatch)

	c.Equal(http.StatusOK, recorder.Code)
	var responses []domain.AuthResponse
	c.NoError(json.Unmarshal(recorder.Body.Bytes(), &responses))
	c.Len(responses, 3)
	c.Equal(domain.Decline, responses[0].Decision)
	c.NotEqual(engine.sent[0].AuthorizationID, engine.sent[1].AuthorizationID)

	var feed []Decision
	c.NoError(json.Unmarshal(doRequest(handler, http.MethodGet, "/api/recent", "").Body.Bytes(), &feed))
	c.Len(feed, 3)
	c.Equal("manual", feed[0].Source)
	c.Equal([]string{domain.ReasonMCCBlocked}, feed[0].Reasons)
	c.Equal(engine.sent[2].AuthorizationID, feed[0].AuthorizationID, "newest first")
}

func TestABatchMustHaveACount(t *testing.T) {
	c := require.New(t)

	recorder := doRequest(newTestServer(t, &fakeEngine{}, ""), http.MethodPost, "/api/transactions", `{"count": 0}`)

	c.Equal(http.StatusBadRequest, recorder.Code)
	c.Equal(errBadBatch.Error(), decodeError(c, recorder))
}

func TestABatchMustBeJSON(t *testing.T) {
	c := require.New(t)

	recorder := doRequest(newTestServer(t, &fakeEngine{}, ""), http.MethodPost, "/api/transactions", `{`)

	c.Equal(http.StatusBadRequest, recorder.Code)
	c.Equal(errBadRequest.Error(), decodeError(c, recorder))
}

func TestABatchReportsAnEngineError(t *testing.T) {
	c := require.New(t)

	recorder := doRequest(newTestServer(t, &fakeEngine{authErr: errEngineDown}, ""), http.MethodPost, "/api/transactions", gamblingBatch)

	c.Equal(http.StatusBadGateway, recorder.Code)
	c.Contains(decodeError(c, recorder), "engine down")
}

// A scenario run returns the report's rows and totals, and its seed as a
// string, so the page can replay it exactly (I-036).
func TestAScenarioRunReturnsTheReport(t *testing.T) {
	c := require.New(t)
	handler := newTestServer(t, &fakeEngine{}, "")

	recorder := doRequest(handler, http.MethodPost, "/api/scenarios", `{"scenario": "blocked MCC", "fraud_cards": 2, "seed": "18446744073709551615"}`)

	c.Equal(http.StatusOK, recorder.Code)
	c.Contains(recorder.Body.String(), `"seed":"18446744073709551615"`)
	var result ScenarioResult
	c.NoError(json.Unmarshal(recorder.Body.Bytes(), &result))
	c.Equal(uint64(18446744073709551615), result.Seed)
	c.Len(result.Rows, 1)
	c.Equal("blocked MCC", result.Rows[0].Scenario)
	c.Equal(2, result.Totals.FraudCards)

	var feed []Decision
	c.NoError(json.Unmarshal(doRequest(handler, http.MethodGet, "/api/recent", "").Body.Bytes(), &feed))
	c.Equal("blocked MCC", feed[0].Source)
}

func TestAllScenariosRunByDefault(t *testing.T) {
	c := require.New(t)

	recorder := doRequest(newTestServer(t, &fakeEngine{}, ""), http.MethodPost, "/api/scenarios", `{"normal_cards": 1, "fraud_cards": 1}`)

	c.Equal(http.StatusOK, recorder.Code)
	var result ScenarioResult
	c.NoError(json.Unmarshal(recorder.Body.Bytes(), &result))
	c.Len(result.Rows, 6)
	c.NotZero(result.Seed, "a random seed was drawn")
}

func TestAnUnknownScenarioIsRejected(t *testing.T) {
	c := require.New(t)

	recorder := doRequest(newTestServer(t, &fakeEngine{}, ""), http.MethodPost, "/api/scenarios", `{"scenario": "nope"}`)

	c.Equal(http.StatusBadRequest, recorder.Code)
	c.Contains(decodeError(c, recorder), errUnknownScenario.Error())
}

func TestTooManyCardsAreRejected(t *testing.T) {
	c := require.New(t)

	recorder := doRequest(newTestServer(t, &fakeEngine{}, ""), http.MethodPost, "/api/scenarios", `{"normal_cards": 51}`)

	c.Equal(http.StatusBadRequest, recorder.Code)
	c.Equal(errBadCards.Error(), decodeError(c, recorder))
}

func TestAScenarioRequestMustBeJSON(t *testing.T) {
	c := require.New(t)

	recorder := doRequest(newTestServer(t, &fakeEngine{}, ""), http.MethodPost, "/api/scenarios", `{"seed": 5}`)

	c.Equal(http.StatusBadRequest, recorder.Code, "the seed must be a string")
}

func TestAScenarioRunReportsAnEngineError(t *testing.T) {
	c := require.New(t)

	recorder := doRequest(newTestServer(t, &fakeEngine{policyErr: errEngineDown}, ""), http.MethodPost, "/api/scenarios", `{}`)

	c.Equal(http.StatusBadGateway, recorder.Code)
	c.Contains(decodeError(c, recorder), "engine down")
}

// One scenario run at a time: a second click while one runs gets 409.
func TestOnlyOneScenarioRunAtATime(t *testing.T) {
	c := require.New(t)
	engine := &fakeEngine{block: make(chan struct{}), entered: make(chan struct{}, 1)}
	handler := newTestServer(t, engine, "")
	first := make(chan int)

	go func() {
		first <- doRequest(handler, http.MethodPost, "/api/scenarios", `{"scenario": "blocked MCC", "fraud_cards": 1}`).Code
	}()

	<-engine.entered // the first run holds the lock and is inside the engine

	second := doRequest(handler, http.MethodPost, "/api/scenarios", `{"scenario": "blocked MCC", "fraud_cards": 1}`)

	close(engine.block)
	c.Equal(http.StatusConflict, second.Code)
	c.Equal(errRunInProgress.Error(), decodeError(c, second))
	c.Equal(http.StatusOK, <-first)
}

func TestMetricsArePassedThrough(t *testing.T) {
	c := require.New(t)
	engine := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		c.Equal("/v1/metrics", request.URL.Path)
		c.Equal("2026-11-14T15:00:00Z", request.URL.Query().Get("since"))
		_, err := writer.Write([]byte(`{"decisions": 7}`))
		c.NoError(err)
	}))
	defer engine.Close()

	recorder := doRequest(newTestServer(t, &fakeEngine{}, engine.URL), http.MethodGet, "/api/metrics?since=2026-11-14T15:00:00Z", "")

	c.Equal(http.StatusOK, recorder.Code)
	c.JSONEq(`{"decisions": 7}`, recorder.Body.String())
}

func TestMetricsReportAnUnreachableEngine(t *testing.T) {
	c := require.New(t)

	recorder := doRequest(newTestServer(t, &fakeEngine{}, "http://localhost:1"), http.MethodGet, "/api/metrics", "")

	c.Equal(http.StatusBadGateway, recorder.Code)
	c.Equal(errEngine.Error(), decodeError(c, recorder))
}

func TestMetricsReportABadEngineURL(t *testing.T) {
	c := require.New(t)

	recorder := doRequest(newTestServer(t, &fakeEngine{}, "http://bad host"), http.MethodGet, "/api/metrics", "")

	c.Equal(http.StatusInternalServerError, recorder.Code)
}

func TestTheFeedKeepsTheNewestDecisions(t *testing.T) {
	c := require.New(t)
	recent := NewRecent(2)

	recent.Add(Decision{AuthorizationID: "a1"})
	recent.Add(Decision{AuthorizationID: "a2"})
	recent.Add(Decision{AuthorizationID: "a3"})

	c.Equal([]Decision{{AuthorizationID: "a3"}, {AuthorizationID: "a2"}}, recent.List())
}
