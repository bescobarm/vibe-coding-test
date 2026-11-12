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
)

// DecideFunc produces the decision. The store calls it at most once per
// authorization_id, inside the claiming transaction.
type DecideFunc func(ctx context.Context) (domain.AuthResponse, error)

// Recorder claims an authorization_id, runs decide once and stores the result.
// If the ID was already decided it returns the stored response and replayed=true.
type Recorder interface {
	Decide(ctx context.Context, req domain.AuthRequest, decide DecideFunc) (resp domain.AuthResponse, replayed bool, err error)
}

type CardReader interface {
	GetCard(ctx context.Context, cardID string) (domain.Card, error)
}

type PolicyReader interface {
	LatestPolicyVersion(ctx context.Context, companyID string) (int, error)
}

type Service struct {
	recorder Recorder
	cards    CardReader
	policies PolicyReader
	pipeline *pipeline.Pipeline
	now      func() time.Time
}

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

	card, err := s.cards.GetCard(ctx, req.CardID)
	switch {
	case err == nil:
		e.Card, e.CardFound = card, true
	case !errors.Is(err, domain.ErrNotFound):
		return domain.AuthResponse{}, false, fmt.Errorf("load card: %w", err)
	}

	if e.CardFound {
		v, err := s.policies.LatestPolicyVersion(ctx, card.CompanyID)
		if err != nil {
			return domain.AuthResponse{}, false, fmt.Errorf("load policy: %w", err)
		}

		e.PolicyVersion = v
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

func roundMS(d time.Duration) float64 {
	return math.Round(float64(d.Microseconds())/10) / 100
}
