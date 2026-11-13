package velocity

import (
	"context"
	"fmt"

	"github.com/bescobarm/card-auth-engine/internal/domain"
	"github.com/bescobarm/card-auth-engine/internal/pipeline"
	"github.com/bescobarm/card-auth-engine/internal/policy"
)

const (
	stageName = "velocity"

	fmtCurrencyMismatch = "Rule %s limit is in %s, request is in %s"
	fmtOverCount        = "Rule %s allows %d authorizations per %s; %d already approved"
	fmtOverAmount       = "Rule %s: %d already spent plus %d is over the limit of %d %s per %s"
)

// Reserver holds an amount against a card's windows.
type Reserver interface {
	Reserve(ctx context.Context, r Reservation) (Result, error)
}

// Stage applies the company's velocity rules after the hard controls.
type Stage struct {
	reserver Reserver
}

// NewStage returns the velocity stage.
func NewStage(r Reserver) Stage {
	return Stage{reserver: r}
}

// Name implements pipeline.Stage.
func (Stage) Name() string { return stageName }

// Evaluate implements pipeline.Stage. A Redis failure is returned as an error,
// not a decline: deciding what to do then is the fallback's job (SP05).
func (s Stage) Evaluate(ctx context.Context, e *pipeline.Evaluation) error {
	rules := applicable(e)
	if len(rules) == 0 {
		return nil
	}

	for _, r := range rules {
		mismatch := r.MaxAmount != nil && r.MaxAmount.Currency != e.Request.Currency
		if mismatch {
			e.Decline(amountReason(r), fmt.Sprintf(fmtCurrencyMismatch, r.ID, r.MaxAmount.Currency, e.Request.Currency))
			return nil
		}
	}

	res, err := s.reserver.Reserve(ctx, reservation(e, rules))
	if err != nil {
		return err // the pipeline adds the stage name
	}

	if !res.Reserved {
		code, msg := limitHit(rules[res.Window], res, e.Request.AmountMinor)
		e.Decline(code, msg)
	}

	return nil
}

func applicable(e *pipeline.Evaluation) []policy.VelocityRule {
	var rules []policy.VelocityRule
	for _, r := range e.Policy.Velocity {
		if r.Applies(e.Request.AmountMinor, e.Request.Merchant.MCC) {
			rules = append(rules, r)
		}
	}

	return rules
}

// reservation uses the engine clock, never the request's created_at (D-045).
func reservation(e *pipeline.Evaluation, rules []policy.VelocityRule) Reservation {
	loc := e.Policy.Location()
	windows := make([]Window, len(rules))
	for i, r := range rules {
		windows[i] = Window{
			RuleID:    r.ID,
			Start:     r.WindowStart(e.ReceivedAt, loc),
			MaxCount:  NoLimit,
			MaxAmount: NoLimit,
			Retention: r.Retention(),
		}

		if r.MaxCount != nil {
			windows[i].MaxCount = int64(*r.MaxCount)
		}

		if r.MaxAmount != nil {
			windows[i].MaxAmount = r.MaxAmount.AmountMinor
		}
	}

	return Reservation{
		CardID:          e.Request.CardID,
		AuthorizationID: e.Request.AuthorizationID,
		AmountMinor:     e.Request.AmountMinor,
		Now:             e.ReceivedAt,
		Windows:         windows,
	}
}

// limitHit names the limit that was hit: the count when it is full, otherwise the amount.
func limitHit(r policy.VelocityRule, res Result, amountMinor int64) (string, string) {
	countFull := r.MaxCount != nil && res.Count >= int64(*r.MaxCount)
	if countFull || r.MaxAmount == nil {
		return domain.ReasonVelocityExceeded, fmt.Sprintf(fmtOverCount, r.ID, *r.MaxCount, r.Window, res.Count)
	}

	return amountReason(r), fmt.Sprintf(fmtOverAmount, r.ID, res.SpentMinor, amountMinor,
		r.MaxAmount.AmountMinor, r.MaxAmount.Currency, r.Window)
}

// amountReason maps an amount limit to the daily or monthly code (D-052).
func amountReason(r policy.VelocityRule) string {
	switch r.Window {
	case policy.CalendarDay:
		return domain.ReasonOverDailyLimit
	case policy.CalendarMonth:
		return domain.ReasonOverMonthlyLimit
	default:
		return domain.ReasonVelocityExceeded
	}
}
