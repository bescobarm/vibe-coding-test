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

// acmePolicy is the README example policy, validated so hours and time zone are resolved.
func acmePolicy(t *testing.T) policy.Document {
	t.Helper()
	d := policy.Document{
		CompanyID: "acme-mx",
		Version:   1,
		Timezone:  "America/Mexico_City",
		Controls: policy.Controls{
			BlockedMCCs:       []string{"7995", "5933"},
			AllowedCountries:  []string{"MX", "US", "CO"},
			MaxPerTransaction: &policy.Money{AmountMinor: 50000, Currency: "USD"},
			AllowedEntryModes: []string{"chip", "contactless", "ecommerce", "wallet"},
			AllowedHours:      &policy.AllowedHours{Start: "06:00", End: "23:00"},
		},
	}
	require.Empty(t, d.Validate())
	return d
}

// evaluation is an active card under the acme policy buying 45.00 USD of software
// online in the US at 09:04 Mexico City time: every hard control passes.
func evaluation(t *testing.T) *pipeline.Evaluation {
	t.Helper()
	e := pipeline.NewEvaluation(domain.AuthRequest{
		AuthorizationID: "auth_1", CardID: "card_123", AmountMinor: 4500, Currency: "USD",
		Merchant:  domain.Merchant{ID: "m_1", MCC: "5734", Country: "US"},
		EntryMode: "ecommerce",
	})
	e.CardFound = true
	e.Card = domain.Card{ID: "card_123", CompanyID: "acme-mx", Status: domain.CardActive}
	e.Policy = acmePolicy(t)
	e.Controls = e.Policy.Controls
	e.Now = time.Date(2026, 11, 12, 15, 4, 0, 0, time.UTC)
	return e
}

func run(c *require.Assertions, e *pipeline.Evaluation, stages ...pipeline.Stage) {
	c.NoError(pipeline.New(stages...).Run(context.Background(), e))
}

func TestHardControlsApproveAnOrdinaryPurchase(t *testing.T) {
	c := require.New(t)
	e := evaluation(t)

	run(c, e, Hard()...)

	c.Equal(domain.Approve, e.Decision())
	c.Empty(e.Reasons())
}

func TestHardControlsRunInTheDecidedOrder(t *testing.T) {
	c := require.New(t)

	var names []string
	for _, s := range Hard() {
		names = append(names, s.Name())
	}

	c.Equal([]string{"card_status", "entry_mode", "mcc", "country", "transaction_limit", "allowed_hours"}, names)
}

func TestOnlyTheFirstFailingControlIsReported(t *testing.T) {
	c := require.New(t)
	e := evaluation(t)
	e.Request.Merchant.MCC = "7995"
	e.Request.Merchant.Country = "BR"
	e.Request.AmountMinor = 90000

	run(c, e, Hard()...)

	c.Equal(domain.Decline, e.Decision())
	c.Equal([]domain.Reason{{Code: domain.ReasonMCCBlocked, Message: "Merchant category 7995 is blocked"}}, e.Reasons())
}

func TestAFrozenCardIsReportedBeforeOtherControls(t *testing.T) {
	c := require.New(t)
	e := evaluation(t)
	e.Card.Status = domain.CardFrozen
	e.Request.Merchant.MCC = "7995"

	run(c, e, Hard()...)

	c.Equal(domain.ReasonCardInactive, e.Reasons()[0].Code)
	c.Len(e.Reasons(), 1)
}

func TestEntryModeDeclinesAModeTheCompanyDoesNotAllow(t *testing.T) {
	c := require.New(t)
	e := evaluation(t)
	e.Request.EntryMode = "manual"

	run(c, e, EntryMode{})

	c.Equal([]domain.Reason{{Code: domain.ReasonEntryModeNotAllowed, Message: "Entry mode manual is not allowed"}}, e.Reasons())
}

func TestEntryModeAllowsEverythingWhenNotConfigured(t *testing.T) {
	c := require.New(t)
	e := evaluation(t)
	e.Controls.AllowedEntryModes = nil
	e.Request.EntryMode = "manual"

	run(c, e, EntryMode{})

	c.Equal(domain.Approve, e.Decision())
}

func TestMCCDeclinesABlockedCategory(t *testing.T) {
	c := require.New(t)
	e := evaluation(t)
	e.Request.Merchant.MCC = "7995"

	run(c, e, MCC{})

	c.Equal([]domain.Reason{{Code: domain.ReasonMCCBlocked, Message: "Merchant category 7995 is blocked"}}, e.Reasons())
}

func TestMCCAllowsACategoryThatIsNotBlocked(t *testing.T) {
	c := require.New(t)
	e := evaluation(t)

	run(c, e, MCC{})

	c.Equal(domain.Approve, e.Decision())
}

func TestCountryDeclinesAMerchantOutsideTheAllowedList(t *testing.T) {
	c := require.New(t)
	e := evaluation(t)
	e.Request.Merchant.Country = "BR"

	run(c, e, Country{})

	c.Equal([]domain.Reason{{Code: domain.ReasonCountryNotAllowed, Message: "Merchant country BR is not allowed"}}, e.Reasons())
}

func TestCountryAllowsEverythingWhenNotConfigured(t *testing.T) {
	c := require.New(t)
	e := evaluation(t)
	e.Controls.AllowedCountries = nil
	e.Request.Merchant.Country = "BR"

	run(c, e, Country{})

	c.Equal(domain.Approve, e.Decision())
}

func TestTransactionLimitDeclinesAnAmountOverTheLimit(t *testing.T) {
	c := require.New(t)
	e := evaluation(t)
	e.Request.AmountMinor = 50001

	run(c, e, TransactionLimit{})

	c.Equal([]domain.Reason{{
		Code:    domain.ReasonOverTransactionLimit,
		Message: "Amount 50001 is over the per-transaction limit of 50000 USD",
	}}, e.Reasons())
}

func TestTransactionLimitAllowsAnAmountExactlyAtTheLimit(t *testing.T) {
	c := require.New(t)
	e := evaluation(t)
	e.Request.AmountMinor = 50000

	run(c, e, TransactionLimit{})

	c.Equal(domain.Approve, e.Decision())
}

func TestTransactionLimitDeclinesADifferentCurrency(t *testing.T) {
	c := require.New(t)
	e := evaluation(t)
	e.Request.Currency = "MXN"

	run(c, e, TransactionLimit{})

	c.Equal([]domain.Reason{{Code: domain.ReasonOverTransactionLimit, Message: "Limit is in USD, request is in MXN"}}, e.Reasons())
}

func TestTransactionLimitAllowsAnyAmountWhenNotConfigured(t *testing.T) {
	c := require.New(t)
	e := evaluation(t)
	e.Controls.MaxPerTransaction = nil
	e.Request.AmountMinor = 10_000_000

	run(c, e, TransactionLimit{})

	c.Equal(domain.Approve, e.Decision())
}

func TestAllowedHoursDeclinesAtNightInTheCompanyTimezone(t *testing.T) {
	c := require.New(t)
	e := evaluation(t)
	// 09:00 UTC is 03:00 in Mexico City: outside 06:00–23:00 there, inside it in UTC.
	e.Now = time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)

	run(c, e, AllowedHours{})

	c.Equal([]domain.Reason{{
		Code:    domain.ReasonOutsideAllowedHours,
		Message: "03:00 is outside allowed hours 06:00–23:00 (America/Mexico_City)",
	}}, e.Reasons())
}

func TestAllowedHoursAllowsDaytimeInTheCompanyTimezone(t *testing.T) {
	c := require.New(t)
	e := evaluation(t)
	// 04:30 UTC is 22:30 the previous day in Mexico City: inside the window there.
	e.Now = time.Date(2026, 11, 12, 4, 30, 0, 0, time.UTC)

	run(c, e, AllowedHours{})

	c.Equal(domain.Approve, e.Decision())
}

func TestAllowedHoursAllowsAnyTimeWhenNotConfigured(t *testing.T) {
	c := require.New(t)
	e := evaluation(t)
	e.Controls.AllowedHours = nil
	e.Now = time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)

	run(c, e, AllowedHours{})

	c.Equal(domain.Approve, e.Decision())
}
