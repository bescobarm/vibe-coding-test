package controls

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bescobarm/card-auth-engine/internal/domain"
	"github.com/bescobarm/card-auth-engine/internal/pipeline"
	"github.com/bescobarm/card-auth-engine/internal/policy"
)

// newAcmePolicy is the README example policy, validated so hours and time zone are resolved.
func newAcmePolicy(t *testing.T) policy.Document {
	t.Helper()
	doc := policy.Document{
		CompanyID: "acme-mx",
		Version:   1,
		Timezone:  "America/Mexico_City",
		Controls: policy.Controls{
			BlockedMCCs: []string{"7995", "5933"},
			AllowedCountries: []string{
				"MX",
				"US",
				"CO",
			},
			MaxPerTransaction: &policy.Money{AmountMinor: 50000, Currency: "USD"},
			AllowedEntryModes: []string{
				"chip",
				"contactless",
				"ecommerce",
				"wallet",
			},
			AllowedHours: &policy.AllowedHours{Start: "06:00", End: "23:00"},
		},
	}
	require.Empty(t, doc.Validate())
	return doc
}

// newEvaluation is an active card under the acme policy buying 45.00 USD of software
// online in the US at 09:04 Mexico City time: every hard control passes.
func newEvaluation(t *testing.T) *pipeline.Evaluation {
	t.Helper()
	evaluation := pipeline.NewEvaluation(domain.AuthRequest{
		AuthorizationID: "auth_1", CardID: "card_123", AmountMinor: 4500, Currency: "USD",
		Merchant:  domain.Merchant{ID: "m_1", MCC: "5734", Country: "US"},
		EntryMode: "ecommerce",
	})
	evaluation.CardFound = true
	evaluation.Card = domain.Card{ID: "card_123", CompanyID: "acme-mx", Status: domain.CardActive}
	evaluation.Policy = newAcmePolicy(t)
	evaluation.Controls = evaluation.Policy.Controls
	evaluation.Now = time.Date(2026, 11, 12, 15, 4, 0, 0, time.UTC)
	return evaluation
}

func run(c *require.Assertions, e *pipeline.Evaluation, stages ...pipeline.Stage) {
	c.NoError(pipeline.New(stages...).Run(context.Background(), e))
}

func TestHardControlsApproveAnOrdinaryPurchase(t *testing.T) {
	c := require.New(t)
	evaluation := newEvaluation(t)

	run(c, evaluation, ListHardControls()...)

	c.Equal(domain.Approve, evaluation.GetDecision())
	c.Empty(evaluation.GetReasons())
}

func TestHardControlsRunInTheDecidedOrder(t *testing.T) {
	c := require.New(t)

	var names []string
	for _, stage := range ListHardControls() {
		names = append(names, stage.GetName())
	}

	c.Equal([]string{
		"card_status",
		"entry_mode",
		"mcc",
		"country",
		"transaction_limit",
		"allowed_hours",
	}, names)
}

func TestOnlyTheFirstFailingControlIsReported(t *testing.T) {
	c := require.New(t)
	evaluation := newEvaluation(t)
	evaluation.Request.Merchant.MCC = "7995"
	evaluation.Request.Merchant.Country = "BR"
	evaluation.Request.AmountMinor = 90000

	run(c, evaluation, ListHardControls()...)

	c.Equal(domain.Decline, evaluation.GetDecision())
	c.Equal([]domain.Reason{{Code: domain.ReasonMCCBlocked, Message: "Merchant category 7995 is blocked"}}, evaluation.GetReasons())
}

func TestAFrozenCardIsReportedBeforeOtherControls(t *testing.T) {
	c := require.New(t)
	evaluation := newEvaluation(t)
	evaluation.Card.Status = domain.CardFrozen
	evaluation.Request.Merchant.MCC = "7995"

	run(c, evaluation, ListHardControls()...)

	c.Equal(domain.ReasonCardInactive, evaluation.GetReasons()[0].Code)
	c.Len(evaluation.GetReasons(), 1)
}

func TestEntryModeDeclinesAModeTheCompanyDoesNotAllow(t *testing.T) {
	c := require.New(t)
	evaluation := newEvaluation(t)
	evaluation.Request.EntryMode = "manual"

	run(c, evaluation, EntryMode{})

	c.Equal([]domain.Reason{{Code: domain.ReasonEntryModeNotAllowed, Message: "Entry mode manual is not allowed"}}, evaluation.GetReasons())
}

func TestEntryModeAllowsEverythingWhenNotConfigured(t *testing.T) {
	c := require.New(t)
	evaluation := newEvaluation(t)
	evaluation.Controls.AllowedEntryModes = nil
	evaluation.Request.EntryMode = "manual"

	run(c, evaluation, EntryMode{})

	c.Equal(domain.Approve, evaluation.GetDecision())
}

func TestMCCDeclinesABlockedCategory(t *testing.T) {
	c := require.New(t)
	evaluation := newEvaluation(t)
	evaluation.Request.Merchant.MCC = "7995"

	run(c, evaluation, MCC{})

	c.Equal([]domain.Reason{{Code: domain.ReasonMCCBlocked, Message: "Merchant category 7995 is blocked"}}, evaluation.GetReasons())
}

func TestMCCAllowsACategoryThatIsNotBlocked(t *testing.T) {
	c := require.New(t)
	evaluation := newEvaluation(t)

	run(c, evaluation, MCC{})

	c.Equal(domain.Approve, evaluation.GetDecision())
}

func TestCountryDeclinesAMerchantOutsideTheAllowedList(t *testing.T) {
	c := require.New(t)
	evaluation := newEvaluation(t)
	evaluation.Request.Merchant.Country = "BR"

	run(c, evaluation, Country{})

	c.Equal([]domain.Reason{{Code: domain.ReasonCountryNotAllowed, Message: "Merchant country BR is not allowed"}}, evaluation.GetReasons())
}

func TestCountryAllowsEverythingWhenNotConfigured(t *testing.T) {
	c := require.New(t)
	evaluation := newEvaluation(t)
	evaluation.Controls.AllowedCountries = nil
	evaluation.Request.Merchant.Country = "BR"

	run(c, evaluation, Country{})

	c.Equal(domain.Approve, evaluation.GetDecision())
}

func TestTransactionLimitDeclinesAnAmountOverTheLimit(t *testing.T) {
	c := require.New(t)
	evaluation := newEvaluation(t)
	evaluation.Request.AmountMinor = 50001

	run(c, evaluation, TransactionLimit{})

	c.Equal([]domain.Reason{{
		Code:    domain.ReasonOverTransactionLimit,
		Message: "Amount 500.01 USD is over the per-transaction limit of 500.00 USD",
	}}, evaluation.GetReasons())
}

func TestTransactionLimitAllowsAnAmountExactlyAtTheLimit(t *testing.T) {
	c := require.New(t)
	evaluation := newEvaluation(t)
	evaluation.Request.AmountMinor = 50000

	run(c, evaluation, TransactionLimit{})

	c.Equal(domain.Approve, evaluation.GetDecision())
}

func TestTransactionLimitDeclinesADifferentCurrency(t *testing.T) {
	c := require.New(t)
	evaluation := newEvaluation(t)
	evaluation.Request.Currency = "MXN"

	run(c, evaluation, TransactionLimit{})

	c.Equal([]domain.Reason{{Code: domain.ReasonOverTransactionLimit, Message: "Limit is in USD, request is in MXN"}}, evaluation.GetReasons())
}

func TestTransactionLimitAllowsAnyAmountWhenNotConfigured(t *testing.T) {
	c := require.New(t)
	evaluation := newEvaluation(t)
	evaluation.Controls.MaxPerTransaction = nil
	evaluation.Request.AmountMinor = 10_000_000

	run(c, evaluation, TransactionLimit{})

	c.Equal(domain.Approve, evaluation.GetDecision())
}

func TestAllowedHoursDeclinesAtNightInTheCompanyTimezone(t *testing.T) {
	c := require.New(t)
	evaluation := newEvaluation(t)
	// 09:00 UTC is 03:00 in Mexico City: outside 06:00–23:00 there, inside it in UTC.
	evaluation.Now = time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)

	run(c, evaluation, AllowedHours{})

	c.Equal([]domain.Reason{{
		Code:    domain.ReasonOutsideAllowedHours,
		Message: "03:00 is outside allowed hours 06:00–23:00 (America/Mexico_City)",
	}}, evaluation.GetReasons())
}

func TestAllowedHoursAllowsDaytimeInTheCompanyTimezone(t *testing.T) {
	c := require.New(t)
	evaluation := newEvaluation(t)
	// 04:30 UTC is 22:30 the previous day in Mexico City: inside the window there.
	evaluation.Now = time.Date(2026, 11, 12, 4, 30, 0, 0, time.UTC)

	run(c, evaluation, AllowedHours{})

	c.Equal(domain.Approve, evaluation.GetDecision())
}

func TestAllowedHoursAllowsAnyTimeWhenNotConfigured(t *testing.T) {
	c := require.New(t)
	evaluation := newEvaluation(t)
	evaluation.Controls.AllowedHours = nil
	evaluation.Now = time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)

	run(c, evaluation, AllowedHours{})

	c.Equal(domain.Approve, evaluation.GetDecision())
}
