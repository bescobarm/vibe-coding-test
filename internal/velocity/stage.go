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

// Reserver holds an amount against a card's windows, and cancels it when a
// later stage declines.
type Reserver interface {
	Reserve(ctx context.Context, r Reservation) (Result, error)
	Cancel(ctx context.Context, cardID, authorizationID string) error
}

// Stage applies the company's velocity rules after the hard controls.
type Stage struct {
	reserver Reserver
}

// NewStage returns the velocity stage.
func NewStage(r Reserver) Stage {
	return Stage{reserver: r}
}

// GetName implements pipeline.Stage.
func (Stage) GetName() string {
	return stageName
}

// Evaluate implements pipeline.Stage. A Redis failure is returned as an error,
// not a decline: deciding what to do then is the fallback's job (SP05).
func (s Stage) Evaluate(ctx context.Context, e *pipeline.Evaluation) error {
	rules := listApplicableRules(e)
	if len(rules) == 0 {
		return nil
	}

	for _, rule := range rules {
		mismatch := rule.MaxAmount != nil && rule.MaxAmount.Currency != e.Request.Currency
		if mismatch {
			e.Decline(getAmountReason(rule), fmt.Sprintf(fmtCurrencyMismatch, rule.ID, rule.MaxAmount.Currency, e.Request.Currency))
			return nil
		}
	}

	res, err := s.reserver.Reserve(ctx, buildReservation(e, rules))
	if err != nil {
		return err // the pipeline adds the stage name
	}

	if !res.Reserved {
		code, msg := describeLimitHit(rules[res.Window], res, e.Request.AmountMinor)
		e.Decline(code, msg)

		return nil
	}

	cardID, id := e.Request.CardID, e.Request.AuthorizationID
	e.OnDecline(func(ctx context.Context) error {
		return s.reserver.Cancel(ctx, cardID, id)
	})

	return nil
}

func listApplicableRules(e *pipeline.Evaluation) []policy.VelocityRule {
	var rules []policy.VelocityRule
	for _, rule := range e.Policy.Velocity {
		if rule.Applies(e.Request.AmountMinor, e.Request.Merchant.MCC) {
			rules = append(rules, rule)
		}
	}

	return rules
}

// buildReservation uses the engine clock, never the request's created_at (D-045).
func buildReservation(e *pipeline.Evaluation, rules []policy.VelocityRule) Reservation {
	loc := e.Policy.GetLocation()
	windows := make([]Window, len(rules))
	for i, rule := range rules {
		windows[i] = Window{
			RuleID:    rule.ID,
			Start:     rule.GetWindowStart(e.ReceivedAt, loc),
			MaxCount:  NoLimit,
			MaxAmount: NoLimit,
			Retention: rule.GetRetention(),
		}

		if rule.MaxCount != nil {
			windows[i].MaxCount = int64(*rule.MaxCount)
		}

		if rule.MaxAmount != nil {
			windows[i].MaxAmount = rule.MaxAmount.AmountMinor
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

// describeLimitHit names the limit that was hit: the count when it is full, otherwise the amount.
func describeLimitHit(r policy.VelocityRule, res Result, amountMinor int64) (string, string) {
	countFull := r.MaxCount != nil && res.Count >= int64(*r.MaxCount)
	if countFull || r.MaxAmount == nil {
		return getCountReason(r), fmt.Sprintf(fmtOverCount, r.ID, *r.MaxCount, r.Window, res.Count)
	}

	return getAmountReason(r), fmt.Sprintf(fmtOverAmount, r.ID, res.SpentMinor, amountMinor,
		r.MaxAmount.AmountMinor, r.MaxAmount.Currency, r.Window)
}

// getCountReason lets a rule name its own reason, e.g. CARD_TESTING_PATTERN (D-025, D-060).
func getCountReason(r policy.VelocityRule) string {
	if r.Reason != "" {
		return r.Reason
	}

	return domain.ReasonVelocityExceeded
}

// getAmountReason maps an amount limit to the daily or monthly code (D-052).
func getAmountReason(r policy.VelocityRule) string {
	switch r.Window {
	case policy.CalendarDay:
		return domain.ReasonOverDailyLimit
	case policy.CalendarMonth:
		return domain.ReasonOverMonthlyLimit
	default:
		return domain.ReasonVelocityExceeded
	}
}
