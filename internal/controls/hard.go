package controls

import (
	"context"
	"fmt"
	"slices"

	"github.com/bescobarm/card-auth-engine/internal/domain"
	"github.com/bescobarm/card-auth-engine/internal/pipeline"
)

const (
	fmtEntryMode        = "Entry mode %s is not allowed"
	fmtMCCBlocked       = "Merchant category %s is blocked"
	fmtCountry          = "Merchant country %s is not allowed"
	fmtCurrencyMismatch = "Limit is in %s, request is in %s"
	fmtOverLimit        = "Amount %d is over the per-transaction limit of %d %s"
	fmtOutsideHours     = "%s is outside allowed hours %s–%s (%s)"
	clockLayout         = "15:04"
)

// ListHardControls returns every hard control in the order they run. The pipeline stops at
// the first decline (D-039), so this order decides which reason a request gets.
func ListHardControls() []pipeline.Stage {
	return []pipeline.Stage{
		CardStatus{},
		EntryMode{},
		MCC{},
		Country{},
		TransactionLimit{},
		AllowedHours{},
	}
}

// EntryMode declines an entry mode the company does not allow (all are allowed when none are listed).
type EntryMode struct{}

// GetName implements pipeline.Stage.
func (EntryMode) GetName() string {
	return "entry_mode"
}

// Evaluate implements pipeline.Stage.
func (EntryMode) Evaluate(_ context.Context, e *pipeline.Evaluation) error {
	allowed := e.Controls.AllowedEntryModes
	if len(allowed) > 0 && !slices.Contains(allowed, e.Request.EntryMode) {
		e.Decline(domain.ReasonEntryModeNotAllowed,
			fmt.Sprintf(fmtEntryMode, e.Request.EntryMode))
	}

	return nil
}

// MCC declines a blocked merchant category.
type MCC struct{}

// GetName implements pipeline.Stage.
func (MCC) GetName() string {
	return "mcc"
}

// Evaluate implements pipeline.Stage.
func (MCC) Evaluate(_ context.Context, e *pipeline.Evaluation) error {
	if slices.Contains(e.Controls.BlockedMCCs, e.Request.Merchant.MCC) {
		e.Decline(domain.ReasonMCCBlocked,
			fmt.Sprintf(fmtMCCBlocked, e.Request.Merchant.MCC))
	}

	return nil
}

// Country declines a merchant country outside the allowed list (all are allowed when none are listed).
type Country struct{}

// GetName implements pipeline.Stage.
func (Country) GetName() string {
	return "country"
}

// Evaluate implements pipeline.Stage.
func (Country) Evaluate(_ context.Context, e *pipeline.Evaluation) error {
	allowed := e.Controls.AllowedCountries
	if len(allowed) > 0 && !slices.Contains(allowed, e.Request.Merchant.Country) {
		e.Decline(domain.ReasonCountryNotAllowed,
			fmt.Sprintf(fmtCountry, e.Request.Merchant.Country))
	}

	return nil
}

// TransactionLimit declines amounts over the per-transaction limit. A request in
// a different currency than the limit is declined too: without FX conversion
// the engine can't tell whether it is over the limit (D-041).
type TransactionLimit struct{}

// GetName implements pipeline.Stage.
func (TransactionLimit) GetName() string {
	return "transaction_limit"
}

// Evaluate implements pipeline.Stage.
func (TransactionLimit) Evaluate(_ context.Context, e *pipeline.Evaluation) error {
	limit := e.Controls.MaxPerTransaction
	switch {
	case limit == nil:
	case e.Request.Currency != limit.Currency:
		e.Decline(domain.ReasonOverTransactionLimit,
			fmt.Sprintf(fmtCurrencyMismatch, limit.Currency, e.Request.Currency))
	case e.Request.AmountMinor > limit.AmountMinor:
		e.Decline(domain.ReasonOverTransactionLimit,
			fmt.Sprintf(fmtOverLimit,
				e.Request.AmountMinor, limit.AmountMinor, limit.Currency))
	}

	return nil
}

// AllowedHours declines outside the company's daily window, in the company's
// time zone (D-026, D-038).
type AllowedHours struct{}

// GetName implements pipeline.Stage.
func (AllowedHours) GetName() string {
	return "allowed_hours"
}

// Evaluate implements pipeline.Stage.
func (AllowedHours) Evaluate(_ context.Context, e *pipeline.Evaluation) error {
	hours := e.Controls.AllowedHours
	if hours == nil {
		return nil
	}

	local := e.Now.In(e.Policy.GetLocation())
	if !hours.Contains(local) {
		e.Decline(domain.ReasonOutsideAllowedHours,
			fmt.Sprintf(fmtOutsideHours,
				local.Format(clockLayout), hours.Start, hours.End, e.Policy.GetLocation()))
	}

	return nil
}
