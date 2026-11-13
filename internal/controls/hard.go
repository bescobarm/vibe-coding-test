package controls

import (
	"context"
	"fmt"
	"slices"

	"github.com/bescobarm/card-auth-engine/internal/domain"
	"github.com/bescobarm/card-auth-engine/internal/pipeline"
)

// Hard returns every hard control in the order they run. The pipeline stops at
// the first decline (D-039), so this order decides which reason a request gets.
func Hard() []pipeline.Stage {
	return []pipeline.Stage{
		CardStatus{},
		EntryMode{},
		MCC{},
		Country{},
		TransactionLimit{},
		AllowedHours{},
	}
}

type EntryMode struct{}

func (EntryMode) Name() string { return "entry_mode" }

func (EntryMode) Evaluate(_ context.Context, e *pipeline.Evaluation) error {
	allowed := e.Controls.AllowedEntryModes
	if len(allowed) > 0 && !slices.Contains(allowed, e.Request.EntryMode) {
		e.Decline(domain.ReasonEntryModeNotAllowed,
			fmt.Sprintf("Entry mode %s is not allowed", e.Request.EntryMode))
	}
	return nil
}

type MCC struct{}

func (MCC) Name() string { return "mcc" }

func (MCC) Evaluate(_ context.Context, e *pipeline.Evaluation) error {
	if slices.Contains(e.Controls.BlockedMCCs, e.Request.Merchant.MCC) {
		e.Decline(domain.ReasonMCCBlocked,
			fmt.Sprintf("Merchant category %s is blocked", e.Request.Merchant.MCC))
	}
	return nil
}

type Country struct{}

func (Country) Name() string { return "country" }

func (Country) Evaluate(_ context.Context, e *pipeline.Evaluation) error {
	allowed := e.Controls.AllowedCountries
	if len(allowed) > 0 && !slices.Contains(allowed, e.Request.Merchant.Country) {
		e.Decline(domain.ReasonCountryNotAllowed,
			fmt.Sprintf("Merchant country %s is not allowed", e.Request.Merchant.Country))
	}
	return nil
}

// TransactionLimit declines amounts over the per-transaction limit. A request in
// a different currency than the limit is declined too: without FX conversion
// the engine can't tell whether it is over the limit (D-041).
type TransactionLimit struct{}

func (TransactionLimit) Name() string { return "transaction_limit" }

func (TransactionLimit) Evaluate(_ context.Context, e *pipeline.Evaluation) error {
	limit := e.Controls.MaxPerTransaction
	switch {
	case limit == nil:
	case e.Request.Currency != limit.Currency:
		e.Decline(domain.ReasonOverTransactionLimit,
			fmt.Sprintf("Limit is in %s, request is in %s", limit.Currency, e.Request.Currency))
	case e.Request.AmountMinor > limit.AmountMinor:
		e.Decline(domain.ReasonOverTransactionLimit,
			fmt.Sprintf("Amount %d is over the per-transaction limit of %d %s",
				e.Request.AmountMinor, limit.AmountMinor, limit.Currency))
	}
	return nil
}

// AllowedHours declines outside the company's daily window, in the company's
// time zone (D-026, D-038).
type AllowedHours struct{}

func (AllowedHours) Name() string { return "allowed_hours" }

func (AllowedHours) Evaluate(_ context.Context, e *pipeline.Evaluation) error {
	h := e.Controls.AllowedHours
	if h == nil {
		return nil
	}
	local := e.Now.In(e.Policy.Location())
	if !h.Contains(local) {
		e.Decline(domain.ReasonOutsideAllowedHours,
			fmt.Sprintf("%s is outside allowed hours %s–%s (%s)",
				local.Format("15:04"), h.Start, h.End, e.Policy.Location()))
	}
	return nil
}
