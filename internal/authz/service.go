// Package authz orchestrates one authorization: claim the ID, load context,
// run the pipeline and record the outcome.
package authz

import (
	"context"
	"errors"
	"fmt"
	"math"
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

// Service decides authorizations: it loads the card and policy, then runs the pipeline once per authorization_id.
type Service struct {
	recorder Recorder
	cards    CardReader
	policies PolicyReader
	signals  SignalReader
	pipeline *pipeline.Pipeline
	now      func() time.Time
}

// NewService returns a Service that uses the real clock.
func NewService(r Recorder, c CardReader, p PolicyReader, pl *pipeline.Pipeline) *Service {
	return &Service{
		recorder: r,
		cards:    c,
		policies: p,
		pipeline: pl,
		now:      time.Now,
	}
}

// WithSignals makes the service load the anomaly signals before each claim.
func (s *Service) WithSignals(r SignalReader) *Service {
	s.signals = r
	return s
}

// Authorize loads everything it needs from Postgres BEFORE the recorder opens
// the claiming transaction. Inside decide, no stage may use the Postgres pool:
// a transaction that holds one pooled connection and asks the same pool for a
// second one deadlocks as soon as concurrent requests exceed the pool size
// (found by TestConcurrentDistinctRequestsDoNotExhaustThePool).
func (s *Service) Authorize(ctx context.Context, req domain.AuthRequest) (domain.AuthResponse, bool, error) {
	start := s.now()
	evaluation := pipeline.NewEvaluation(req)
	evaluation.ReceivedAt = start

	card, err := s.cards.GetCard(ctx, req.CardID)
	switch {
	case err == nil:
		evaluation.Card, evaluation.CardFound = card, true
	case !errors.Is(err, domain.ErrNotFound):
		return domain.AuthResponse{}, false, fmt.Errorf("load card: %w", err)
	}

	evaluation.Now = req.CreatedAt
	if evaluation.Now.IsZero() {
		evaluation.Now = start
	}

	if evaluation.CardFound {
		err := s.loadPolicy(ctx, evaluation)
		if err != nil {
			return domain.AuthResponse{}, false, err
		}

		err = s.loadSignals(ctx, evaluation)
		if err != nil {
			return domain.AuthResponse{}, false, err
		}
	}

	return s.recorder.Decide(ctx, req, func(ctx context.Context) (domain.AuthResponse, error) {
		err := s.pipeline.Run(ctx, evaluation)
		if err != nil {
			return domain.AuthResponse{}, err
		}

		resp := evaluation.BuildResponse()
		resp.LatencyMS = roundToMS(s.now().Sub(start))

		return resp, nil
	})
}

// loadPolicy sets the company policy and the card's effective controls.
// A company without a policy has no controls and records policy version 0.
func (s *Service) loadPolicy(ctx context.Context, e *pipeline.Evaluation) error {
	doc, err := s.policies.GetPolicy(ctx, e.Card.CompanyID)
	switch {
	case errors.Is(err, domain.ErrNotFound):
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

func (s *Service) loadSignals(ctx context.Context, e *pipeline.Evaluation) error {
	if s.signals == nil {
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
