// Package pipeline runs the decision stages in order, cheapest first.
// Each sub-project adds a Stage; the HTTP handler never changes.
package pipeline

import (
	"context"
	"fmt"
	"time"

	"github.com/bescobarm/card-auth-engine/internal/domain"
	"github.com/bescobarm/card-auth-engine/internal/policy"
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
	Policy        policy.Document
	Controls      policy.Controls // company controls with the card's overrides applied
	PolicyVersion int
	Now           time.Time // transaction time: the request's created_at, or the engine clock
	ReceivedAt    time.Time // engine clock when the request arrived; velocity windows use it (D-045)
	RiskScore     int

	decision domain.Decision
	reasons  []domain.Reason
	halted   bool
}

// NewEvaluation starts an evaluation with no reasons and an APPROVE decision.
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

// Halted reports whether a stage declined.
func (e *Evaluation) Halted() bool { return e.halted }

// Decision is APPROVE unless a stage declined.
func (e *Evaluation) Decision() domain.Decision {
	if e.decision == "" {
		return domain.Approve
	}

	return e.decision
}

// Reasons returns the reasons recorded so far.
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

// Pipeline is an ordered list of stages.
type Pipeline struct {
	stages []Stage
}

// New returns a pipeline that runs stages in the given order.
func New(stages ...Stage) *Pipeline {
	return &Pipeline{stages: stages}
}

// Run evaluates the stages in order and stops at the first decline.
func (p *Pipeline) Run(ctx context.Context, e *Evaluation) error {
	for _, s := range p.stages {
		err := s.Evaluate(ctx, e)
		if err != nil {
			return fmt.Errorf("stage %s: %w", s.Name(), err)
		}

		if e.halted {
			return nil
		}
	}

	return nil
}
