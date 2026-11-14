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
	Policy(ctx context.Context, companyID string) (policy.Document, error)
}

// Service decides authorizations: it loads the card and policy, then runs the pipeline once per authorization_id.
type Service struct {
	recorder Recorder
	cards    CardReader
	policies PolicyReader
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

// Authorize loads everything it needs from Postgres BEFORE the recorder opens
// the claiming transaction. Inside decide, no stage may use the Postgres pool:
// a transaction that holds one pooled connection and asks the same pool for a
// second one deadlocks as soon as concurrent requests exceed the pool size
// (found by TestConcurrentDistinctRequestsDoNotExhaustThePool).
func (s *Service) Authorize(ctx context.Context, req domain.AuthRequest) (domain.AuthResponse, bool, error) {
	start := s.now()
	e := pipeline.NewEvaluation(req)
	e.ReceivedAt = start

	card, err := s.cards.GetCard(ctx, req.CardID)
	switch {
	case err == nil:
		e.Card, e.CardFound = card, true
	case !errors.Is(err, domain.ErrNotFound):
		return domain.AuthResponse{}, false, fmt.Errorf("load card: %w", err)
	}

	e.Now = req.CreatedAt
	if e.Now.IsZero() {
		e.Now = start
	}

	if e.CardFound {
		err := s.loadPolicy(ctx, e)
		if err != nil {
			return domain.AuthResponse{}, false, err
		}
	}

	return s.recorder.Decide(ctx, req, func(ctx context.Context) (domain.AuthResponse, error) {
		err := s.pipeline.Run(ctx, e)
		if err != nil {
			return domain.AuthResponse{}, err
		}

		resp := e.Response()
		resp.LatencyMS = roundMS(s.now().Sub(start))

		return resp, nil
	})
}

// loadPolicy sets the company policy and the card's effective controls.
// A company without a policy has no controls and records policy version 0.
func (s *Service) loadPolicy(ctx context.Context, e *pipeline.Evaluation) error {
	doc, err := s.policies.Policy(ctx, e.Card.CompanyID)
	switch {
	case errors.Is(err, domain.ErrNotFound):
	case err != nil:
		return fmt.Errorf("load policy: %w", err)
	default:
		e.Policy, e.PolicyVersion = doc, doc.Version
	}

	controls, err := policy.Effective(e.Policy.Controls, e.Card.Overrides)
	if err != nil {
		return fmt.Errorf("card %s: %w", e.Card.ID, err)
	}

	e.Controls = controls
	return nil
}

const (
	microsPerHundredthMS = 10
	hundredthsPerMS      = 100
)

func roundMS(d time.Duration) float64 {
	return math.Round(float64(d.Microseconds())/microsPerHundredthMS) / hundredthsPerMS
}
