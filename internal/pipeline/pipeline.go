// Package pipeline runs the decision stages in order, cheapest first.
// Each sub-project adds a Stage; the HTTP handler never changes.
package pipeline

import (
	"context"
	"fmt"

	"github.com/bescobarm/card-auth-engine/internal/domain"
)

// Stage is one step of the decision. It reads and updates the Evaluation.
// Returning an error means the stage could not decide (a dependency failed),
// which is different from deciding to decline.
type Stage interface {
	Name() string
	Evaluate(ctx context.Context, e *Evaluation) error
}

// Evaluation is the state that flows through the stages for one request.
type Evaluation struct {
	Request       domain.AuthRequest
	Card          domain.Card
	CardFound     bool
	PolicyVersion int
	RiskScore     int

	decision domain.Decision
	reasons  []domain.Reason
	halted   bool
}

func NewEvaluation(req domain.AuthRequest) *Evaluation {
	return &Evaluation{Request: req, reasons: []domain.Reason{}}
}

// Decline records a reason and stops the pipeline: later stages are skipped.
func (e *Evaluation) Decline(code, message string) {
	e.decision = domain.Decline
	e.reasons = append(e.reasons, domain.Reason{Code: code, Message: message})
	e.halted = true
}

// AddReason records a reason without stopping the pipeline (used by scoring).
func (e *Evaluation) AddReason(code, message string) {
	e.reasons = append(e.reasons, domain.Reason{Code: code, Message: message})
}

func (e *Evaluation) Halted() bool { return e.halted }

// Decision is APPROVE unless a stage declined.
func (e *Evaluation) Decision() domain.Decision {
	if e.decision == "" {
		return domain.Approve
	}
	return e.decision
}

func (e *Evaluation) Reasons() []domain.Reason { return e.reasons }

// Response builds the processor-facing answer. Latency is set by the caller.
func (e *Evaluation) Response() domain.AuthResponse {
	resp := domain.AuthResponse{
		AuthorizationID: e.Request.AuthorizationID,
		Decision:        e.Decision(),
		RiskScore:       e.RiskScore,
		Reasons:         e.reasons,
		PolicyVersion:   e.PolicyVersion,
	}
	if resp.Decision == domain.Approve {
		resp.ApprovedAmountMinor = e.Request.AmountMinor
	}
	return resp
}

type Pipeline struct {
	stages []Stage
}

func New(stages ...Stage) *Pipeline {
	return &Pipeline{stages: stages}
}

// Run evaluates the stages in order and stops at the first decline.
func (p *Pipeline) Run(ctx context.Context, e *Evaluation) error {
	for _, s := range p.stages {
		if err := s.Evaluate(ctx, e); err != nil {
			return fmt.Errorf("stage %s: %w", s.Name(), err)
		}
		if e.halted {
			return nil
		}
	}
	return nil
}
