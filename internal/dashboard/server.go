// Package dashboard serves a small web page to send random or specific
// transactions to the engine and watch its metrics live (D-073, D-074). It is
// a separate process: the engine stays a pure authorization service.
package dashboard

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/bescobarm/card-auth-engine/internal/domain"
	"github.com/bescobarm/card-auth-engine/internal/simulator"
)

//go:embed index.html
var indexHTML []byte

// The page's font (Poppins, one file per weight), embedded so the dashboard
// still works offline (D-077).
//
//go:embed fonts/*.woff2
var fontFiles embed.FS

const (
	maxBodyBytes      = 16 << 10
	maxBatch          = 500
	maxCards          = 50
	scenarioAll       = "all"
	sourceManual      = "manual"
	pathEngineMetrics = "/v1/metrics"
	pathEngineSeries  = "/v1/metrics/series"
	headerType        = "Content-Type"
	contentTypeJSON   = "application/json"
	contentTypeHTML   = "text/html; charset=utf-8"
	contentTypeFont   = "font/woff2"
	headerCache       = "Cache-Control"
	cacheOneDay       = "public, max-age=86400"
	fmtManualID       = "dash-%s-%d"
)

// Errors shown to the page.
var (
	errBadRequest      = errors.New("request is not valid JSON")
	errBadBatch        = errors.New("count must be between 1 and 500")
	errBadCards        = errors.New("cards must be between 0 and 50 per scenario")
	errUnknownScenario = errors.New("unknown scenario")
	errRunInProgress   = errors.New("a scenario run is already in progress")
	errEngine          = errors.New("the engine did not answer")
)

// Config wires a dashboard to an engine.
type Config struct {
	Engine    simulator.Engine      // where transactions go
	Cards     simulator.CardCreator // creates each scenario run's cards (D-066)
	EngineURL string                // for the metrics pass-through
	Client    *http.Client
	Log       *slog.Logger
}

// Server serves the dashboard page and its API.
type Server struct {
	config  Config
	recent  *Recent
	engine  simulator.Engine // records every decision it sends
	running sync.Mutex       // one scenario run at a time
	seq     int64
	seqMu   sync.Mutex
	runID   string
}

// NewServer returns a dashboard that sends traffic to config.Engine.
func NewServer(config Config) (*Server, error) {
	runID, err := simulator.NewRunID()
	if err != nil {
		return nil, err
	}

	recent := NewRecent(recentSize)

	return &Server{config: config, recent: recent, engine: &recordingEngine{inner: config.Engine, recent: recent}, runID: runID}, nil
}

// Handler returns the page and its API routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.servePage)
	mux.HandleFunc("GET /fonts/{file}", s.serveFont)
	mux.HandleFunc("GET /api/metrics", s.proxyGet(pathEngineMetrics))
	mux.HandleFunc("GET /api/metrics/series", s.proxyGet(pathEngineSeries))
	mux.HandleFunc("GET /api/recent", s.listRecent)
	mux.HandleFunc("POST /api/transactions", s.sendTransactions)
	mux.HandleFunc("POST /api/scenarios", s.runScenarios)

	return mux
}

func (s *Server) servePage(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set(headerType, contentTypeHTML)

	_, err := w.Write(indexHTML)
	if err != nil {
		s.config.Log.Error("write page failed", "err", err)
	}
}

// serveFont serves one embedded font file, e.g. /fonts/poppins-400.woff2.
func (s *Server) serveFont(w http.ResponseWriter, r *http.Request) {
	font, err := fontFiles.ReadFile("fonts/" + r.PathValue("file"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	w.Header().Set(headerType, contentTypeFont)
	w.Header().Set(headerCache, cacheOneDay)

	_, err = w.Write(font)
	if err != nil {
		s.config.Log.Error("write font failed", "err", err)
	}
}

// proxyGet passes a GET and its query through to the engine's path, so the
// page needs one origin only.
func (s *Server) proxyGet(path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		target := s.config.EngineURL + path + "?" + r.URL.Query().Encode()

		request, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target, http.NoBody)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}

		response, err := s.config.Client.Do(request)
		if err != nil {
			s.writeError(w, http.StatusBadGateway, errEngine)
			return
		}

		defer response.Body.Close() //nolint:errcheck // read below; nothing to do on close

		w.Header().Set(headerType, contentTypeJSON)
		w.WriteHeader(response.StatusCode)

		_, err = io.Copy(w, response.Body)
		if err != nil {
			s.config.Log.Error("copy metrics failed", "err", err)
		}
	}
}

func (s *Server) listRecent(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, s.recent.List())
}

// TransactionRequest is a specific transaction, sent Count times as a batch.
type TransactionRequest struct {
	CardID      string `json:"card_id"`
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
	MCC         string `json:"mcc"`
	MerchantID  string `json:"merchant_id"`
	Country     string `json:"country"`
	EntryMode   string `json:"entry_mode"`
	Count       int    `json:"count"`
}

// sendTransactions sends a specific transaction, or a batch of the same one.
// Each gets its own authorization_id; the engine validates the fields.
func (s *Server) sendTransactions(w http.ResponseWriter, r *http.Request) {
	var request TransactionRequest

	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes)).Decode(&request)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}

	if request.Count < 1 || request.Count > maxBatch {
		s.writeError(w, http.StatusBadRequest, errBadBatch)
		return
	}

	responses := make([]domain.AuthResponse, 0, request.Count)

	for range request.Count {
		response, err := s.engine.Authorize(withSource(r.Context(), sourceManual), s.buildAuthRequest(request))
		if err != nil {
			s.writeError(w, http.StatusBadGateway, fmt.Errorf("%w: %w", errEngine, err))
			return
		}

		responses = append(responses, response)
	}

	s.writeJSON(w, http.StatusOK, responses)
}

func (s *Server) buildAuthRequest(request TransactionRequest) domain.AuthRequest {
	s.seqMu.Lock()
	s.seq++
	id := fmt.Sprintf(fmtManualID, s.runID, s.seq)
	s.seqMu.Unlock()

	return domain.AuthRequest{
		AuthorizationID: id,
		CardID:          request.CardID,
		AmountMinor:     request.AmountMinor,
		Currency:        request.Currency,
		Merchant:        domain.Merchant{ID: request.MerchantID, Name: request.MerchantID, MCC: request.MCC, Country: request.Country},
		EntryMode:       request.EntryMode,
		CreatedAt:       time.Now().UTC(),
	}
}

// ScenarioRequest runs the simulator's scenarios: all of them, or one.
type ScenarioRequest struct {
	Scenario    string `json:"scenario"`
	NormalCards int    `json:"normal_cards"`
	FraudCards  int    `json:"fraud_cards"`
	Seed        uint64 `json:"seed,string"` // a string: JavaScript numbers lose precision past 2^53 (I-036); "0" draws a new one
}

// ScenarioResult is what a scenario run shows on the page.
type ScenarioResult struct {
	Seed   uint64           `json:"seed,string"`
	RunID  string           `json:"run_id"`
	Rows   []simulator.Row  `json:"rows"`
	Totals simulator.Totals `json:"totals"`
}

// runScenarios plays random cases on fresh cards, like `make simulate`.
func (s *Server) runScenarios(w http.ResponseWriter, r *http.Request) {
	var request ScenarioRequest

	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes)).Decode(&request)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}

	scenarios, err := selectScenarios(request)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}

	if !s.running.TryLock() {
		s.writeError(w, http.StatusConflict, errRunInProgress)
		return
	}

	defer s.running.Unlock()

	result, err := s.playScenarios(r.Context(), request.Seed, scenarios)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, fmt.Errorf("%w: %w", errEngine, err))
		return
	}

	s.writeJSON(w, http.StatusOK, result)
}

func (s *Server) playScenarios(ctx context.Context, given uint64, scenarios []simulator.Scenario) (ScenarioResult, error) {
	seed, err := simulator.PickSeed(given)
	if err != nil {
		return ScenarioResult{}, err
	}

	runID, err := simulator.NewRunID()
	if err != nil {
		return ScenarioResult{}, err
	}

	run := simulator.NewRun(runID, seed, s.engine)

	report, err := simulator.RunReport(withSource(ctx, scenarioSource(scenarios)), run, s.config.Cards, simulator.DefaultPolicy, scenarios)
	if err != nil {
		return ScenarioResult{}, err
	}

	return ScenarioResult{Seed: seed, RunID: runID, Rows: report.ListRows(), Totals: report.GetTotals()}, nil
}

func selectScenarios(request ScenarioRequest) ([]simulator.Scenario, error) {
	if request.NormalCards < 0 || request.NormalCards > maxCards || request.FraudCards < 0 || request.FraudCards > maxCards {
		return nil, errBadCards
	}

	all := simulator.ListScenarios(request.NormalCards, request.FraudCards)
	if request.Scenario == "" || request.Scenario == scenarioAll {
		return all, nil
	}

	index := slices.IndexFunc(all, func(scenario simulator.Scenario) bool {
		return scenario.Name == request.Scenario
	})
	if index < 0 {
		return nil, fmt.Errorf("%w: %q", errUnknownScenario, request.Scenario)
	}

	return all[index : index+1], nil
}

func scenarioSource(scenarios []simulator.Scenario) string {
	if len(scenarios) == 1 {
		return scenarios[0].Name
	}

	return "scenarios"
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set(headerType, contentTypeJSON)
	w.WriteHeader(status)

	err := json.NewEncoder(w).Encode(value)
	if err != nil {
		s.config.Log.Error("write response failed", "err", err)
	}
}

func (s *Server) writeError(w http.ResponseWriter, status int, err error) {
	s.writeJSON(w, status, map[string]string{"error": err.Error()})
}
