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
	GetName() string
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
	Now           time.Time      // transaction time: the request's created_at, or the engine clock
	ReceivedAt    time.Time      // engine clock when the request arrived; velocity windows use it (D-045)
	Signals       domain.Signals // history for the anomaly features, loaded before the claim
	RiskScore     int
	Flagged       bool // approved, but the score passed the company's flag_at

	decision domain.Decision
	reasons  []domain.Reason
	halted   bool
	undo     []func(context.Context) error
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

// DeclineWithReasons declines with the reasons already added (used by scoring).
func (e *Evaluation) DeclineWithReasons() {
	e.decision = domain.Decline
	e.halted = true
}

// OnDecline registers what to undo if a later stage declines, such as a
// reservation made before the score (D-023). Undo runs in reverse order.
func (e *Evaluation) OnDecline(undo func(context.Context) error) {
	e.undo = append(e.undo, undo)
}

// AddReason records a reason without stopping the pipeline (used by scoring).
func (e *Evaluation) AddReason(code, message string) {
	e.reasons = append(e.reasons, domain.Reason{Code: code, Message: message})
}

// IsHalted reports whether a stage declined.
func (e *Evaluation) IsHalted() bool {
	return e.halted
}

// GetDecision is APPROVE unless a stage declined.
func (e *Evaluation) GetDecision() domain.Decision {
	if e.decision == "" {
		return domain.Approve
	}

	return e.decision
}

// GetReasons returns the reasons recorded so far.
func (e *Evaluation) GetReasons() []domain.Reason {
	return e.reasons
}

// BuildResponse builds the processor-facing answer. Latency is set by the caller.
func (e *Evaluation) BuildResponse() domain.AuthResponse {
	resp := domain.AuthResponse{
		AuthorizationID: e.Request.AuthorizationID,
		Decision:        e.GetDecision(),
		RiskScore:       e.RiskScore,
		Reasons:         e.reasons,
		PolicyVersion:   e.PolicyVersion,
		Flagged:         e.Flagged,
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

// Run evaluates the stages in order and stops at the first decline. On a
// decline it undoes what earlier stages registered with OnDecline.
func (p *Pipeline) Run(ctx context.Context, e *Evaluation) error {
	for _, stage := range p.stages {
		err := stage.Evaluate(ctx, e)
		if err != nil {
			return fmt.Errorf("stage %s: %w", stage.GetName(), err)
		}

		if e.halted {
			return e.runUndo(ctx)
		}
	}

	return nil
}

func (e *Evaluation) runUndo(ctx context.Context) error {
	for i := len(e.undo) - 1; i >= 0; i-- {
		err := e.undo[i](ctx)
		if err != nil {
			return fmt.Errorf("undo after decline: %w", err)
		}
	}

	return nil
}
