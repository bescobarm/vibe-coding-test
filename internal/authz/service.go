// Package authz orchestrates one authorization: load context, claim the ID,
// run the pipeline and record the outcome, all within an internal deadline.
// When a dependency fails or the deadline passes, the company's fallback
// answers instead (D-006, D-031, D-064).
package authz

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/bescobarm/card-auth-engine/internal/domain"
	"github.com/bescobarm/card-auth-engine/internal/pipeline"
	"github.com/bescobarm/card-auth-engine/internal/policy"
	"github.com/bescobarm/card-auth-engine/internal/scoring"
)

// DecideFunc produces the decision. The store calls it at most once per
// authorization_id, inside the claiming transaction.
type DecideFunc func(ctx context.Context) (domain.AuthResponse, error)

// Recorder claims an authorization_id, runs decide once and stores the result.
// If the ID was already decided it returns the stored response and replayed=true.
type Recorder interface {
	Decide(ctx context.Context, req domain.AuthRequest, decide DecideFunc) (resp domain.AuthResponse, replayed bool, err error)
}

// CardReader loads a card, or returns domain.ErrNotFound.
type CardReader interface {
	GetCard(ctx context.Context, cardID string) (domain.Card, error)
}

// PolicyReader returns the company's latest policy, or domain.ErrNotFound if it has none.
type PolicyReader interface {
	GetPolicy(ctx context.Context, companyID string) (policy.Document, error)
}

// SignalReader loads the history the anomaly features read (D-058, D-059).
type SignalReader interface {
	GetSignals(ctx context.Context, q domain.SignalQuery) (domain.Signals, error)
}

const (
	// DefaultDeadline is the internal deadline per decision (INTERNAL_DEADLINE_MS).
	DefaultDeadline = 500 * time.Millisecond

	// fallbackWriteTimeout bounds storing a fallback once the request's own
	// context has expired (D-031).
	fallbackWriteTimeout = 200 * time.Millisecond

	// slowDecisionWait bounds how long a background job waits for a decision
	// that missed the deadline before it stores the fallback.
	slowDecisionWait = 5 * time.Second

	logKeyAuthorizationID = "authorization_id"
	msgNoSignals          = "anomaly signals are not configured: the score only sees the request"
	logKeyDecision        = "decision"
	logKeyCompanyID       = "company_id"
)

// Service decides authorizations: it loads the card and policy, then runs the pipeline once per authorization_id.
type Service struct {
	recorder Recorder
	cards    CardReader
	policies PolicyReader
	signals  SignalReader
	pipeline *pipeline.Pipeline
	deadline time.Duration
	log      *slog.Logger
	now      func() time.Time

	background sync.WaitGroup // decisions and fallback writes still running (D-033)
	noSignals  sync.Once      // warns once when no signal reader is configured
	doorbell   func()         // tells the explainer a decision was stored (D-034)
}

// NewService returns a Service with the default deadline, the real clock and no logging.
func NewService(r Recorder, c CardReader, p PolicyReader, pl *pipeline.Pipeline) *Service {
	return &Service{
		recorder: r,
		cards:    c,
		policies: p,
		pipeline: pl,
		deadline: DefaultDeadline,
		log:      slog.New(slog.DiscardHandler),
		now:      time.Now,
		doorbell: func() {},
	}
}

// WithDoorbell calls ring after every stored decision. ring must not block.
func (s *Service) WithDoorbell(ring func()) *Service {
	s.doorbell = ring
	return s
}

// WithSignals makes the service load the anomaly signals before each claim.
func (s *Service) WithSignals(r SignalReader) *Service {
	s.signals = r
	return s
}

// WithDeadline sets the internal deadline per decision.
func (s *Service) WithDeadline(d time.Duration) *Service {
	s.deadline = d
	return s
}

// WithLogger logs fallbacks, including those that could not be stored (D-062).
func (s *Service) WithLogger(l *slog.Logger) *Service {
	s.log = l
	return s
}

// Wait blocks until background decisions and fallback writes finish, or ctx ends.
func (s *Service) Wait(ctx context.Context) error {
	done := make(chan struct{})

	go func() {
		s.background.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("background work still running: %w", ctx.Err())
	}
}

type outcome struct {
	resp     domain.AuthResponse
	replayed bool
	err      error
}

// Authorize loads everything it needs from Postgres BEFORE the recorder opens
// the claiming transaction (D-029), then decides within the deadline. It
// always answers: when something fails, the answer is the fallback.
func (s *Service) Authorize(ctx context.Context, req domain.AuthRequest) (domain.AuthResponse, bool, error) {
	start := s.now()

	ctx, cancel := context.WithTimeout(ctx, s.deadline)
	defer cancel()

	evaluation := pipeline.NewEvaluation(req)
	evaluation.ReceivedAt = start

	evaluation.Now = req.CreatedAt
	if evaluation.Now.IsZero() {
		evaluation.Now = start
	}

	err := s.load(ctx, evaluation)
	if err != nil {
		return s.answerFallback(ctx, req, evaluation, start, err), false, nil
	}

	done := make(chan outcome, 1)

	s.background.Go(func() {
		resp, replayed, err := s.recorder.Decide(ctx, req, s.decide(evaluation, start))
		done <- outcome{resp: resp, replayed: replayed, err: err}
	})

	select {
	case result := <-done:
		if result.err != nil {
			return s.answerFallback(ctx, req, evaluation, start, result.err), false, nil
		}

		if !result.replayed {
			s.doorbell()
		}

		s.logDecision(evaluation, result.resp, result.replayed)

		return result.resp, result.replayed, nil
	case <-ctx.Done():
		return s.answerLateFallback(ctx, req, evaluation, start, done), false, nil
	}
}

func (s *Service) decide(e *pipeline.Evaluation, start time.Time) DecideFunc {
	return func(ctx context.Context) (domain.AuthResponse, error) {
		err := s.pipeline.Run(ctx, e)
		if err != nil {
			return domain.AuthResponse{}, err
		}

		resp := e.BuildResponse()
		resp.LatencyMS = roundToMS(s.now().Sub(start))

		return resp, nil
	}
}

// logDecision writes the structured decision line (README §15): who, what
// and why, so a decline can be traced from the logs alone.
func (s *Service) logDecision(evaluation *pipeline.Evaluation, resp domain.AuthResponse, replayed bool) {
	codes := make([]string, len(resp.Reasons))
	for index, reason := range resp.Reasons {
		codes[index] = reason.Code
	}

	s.log.Info("decision",
		logKeyAuthorizationID, resp.AuthorizationID,
		logKeyCompanyID, evaluation.Card.CompanyID,
		"card_id", evaluation.Request.CardID,
		logKeyDecision, resp.Decision,
		"flagged", resp.Flagged,
		"risk_score", resp.RiskScore,
		"reasons", codes,
		"latency_ms", resp.LatencyMS,
		"fallback_used", resp.FallbackUsed,
		"replayed", replayed,
	)
}

// load reads the card, policy and signals. An unknown card is not an error:
// the card-status control declines it.
func (s *Service) load(ctx context.Context, e *pipeline.Evaluation) error {
	card, err := s.cards.GetCard(ctx, e.Request.CardID)
	switch {
	case err == nil:
		e.Card, e.CardFound = card, true
	case errors.Is(err, domain.ErrNotFound):
		return nil
	default:
		return fmt.Errorf("load card: %w", err)
	}

	err = s.loadPolicy(ctx, e)
	if err != nil {
		return err
	}

	return s.loadSignals(ctx, e)
}

// loadPolicy sets the company policy and the card's effective controls. A
// company without a policy gets the embedded default (D-042, D-063).
func (s *Service) loadPolicy(ctx context.Context, e *pipeline.Evaluation) error {
	doc, err := s.policies.GetPolicy(ctx, e.Card.CompanyID)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		e.Policy = policy.GetDefault()
		e.AddReason(domain.ReasonDefaultPolicy, msgDefaultPolicy)
	case err != nil:
		return fmt.Errorf("load policy: %w", err)
	default:
		e.Policy, e.PolicyVersion = doc, doc.Version
	}

	controls, err := policy.MergeOverrides(e.Policy.Controls, e.Card.Overrides)
	if err != nil {
		return fmt.Errorf("card %s: %w", e.Card.ID, err)
	}

	e.Controls = controls
	return nil
}

// loadSignals reads the anomaly signals. Without a signal reader the score
// only sees the request (only ROUND_AMOUNT can fire), so that is logged once.
func (s *Service) loadSignals(ctx context.Context, e *pipeline.Evaluation) error {
	if s.signals == nil {
		s.noSignals.Do(func() {
			s.log.Warn(msgNoSignals)
		})

		return nil
	}

	sig, err := s.signals.GetSignals(ctx, scoring.BuildSignalQuery(e))
	if err != nil {
		return fmt.Errorf("load signals: %w", err)
	}

	e.Signals = sig
	return nil
}

const (
	microsPerHundredthMS = 10
	hundredthsPerMS      = 100
)

func roundToMS(d time.Duration) float64 {
	return math.Round(float64(d.Microseconds())/microsPerHundredthMS) / hundredthsPerMS
}
