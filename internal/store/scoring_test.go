package store_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bescobarm/card-auth-engine/internal/domain"
	"github.com/bescobarm/card-auth-engine/internal/scoring"
)

const (
	riskPolicy = `"controls": {"max_per_transaction": {"amount_minor": 50000, "currency": "USD"}},
		"risk": {"flag_at": 40, "decline_at": 70}`
	cardTestingPolicy = `"velocity": [{"id": "card-testing", "window": "10m", "max_count": 5,
		"filter": {"max_amount_minor": 500}, "reason": "CARD_TESTING_PATTERN"}], "risk": {"flag_at": 40, "decline_at": 70}`
	budgetAndRisk = `"velocity": [{"id": "daily-spend", "window": "calendar_day",
		"max_amount": {"amount_minor": 1000000, "currency": "USD"}}], "risk": {"flag_at": 40, "decline_at": 70}`
)

// history gives the fixture's cardholder n approved purchases of about 4500
// at m_987 in the US, MCC 5734, so the novelty features apply (D-056).
func (f fixture) history(n int) {
	f.t.Helper()
	for i := range n {
		resp := f.authorize(fmt.Sprintf("hist%d", i), int64(4400+i*50))
		require.Equal(f.t, domain.Approve, resp.Decision)
	}
}

func atMerchant(id, country string) func(*domain.AuthRequest) {
	return func(r *domain.AuthRequest) {
		r.Merchant.ID, r.Merchant.Country = id, country
	}
}

func TestSignalsReadTheCardholderHistory(t *testing.T) {
	c := require.New(t)
	f := newFixtureWith(t, budgetAndRisk)
	f.history(6)
	f.authorizeWith("small", 300, atMerchant("m_other", "US"))
	now := time.Now()

	sig, err := f.st.Signals(f.ctx, domain.SignalQuery{
		CardholderID: "ch_velo", CardID: f.card, MerchantID: "m_987", Country: "BR", MCC: "5734", Currency: "USD",
		HistorySince: now.Add(-time.Hour), RecentSince: now.Add(-time.Minute), DayStart: now.Add(-time.Hour), SmallMaxMinor: 500,
	})

	c.NoError(err)
	c.Equal(7, sig.Approved)
	c.False(sig.CountrySeen)
	c.True(sig.MerchantSeen)
	c.Equal(7, sig.MCCCount)
	c.InDelta(27450.0/7, sig.MCCMeanMinor, 0.01)
	c.Positive(sig.MCCStdDevMinor)
	c.Equal(1, sig.SmallRecent)
	c.Equal([]int64{4400, 4450, 4500, 4550, 4600, 4650}, sig.SameMerchantToday)
}

// AC1 of SP04: 8 small online charges at different merchants within minutes.
// The score flags from the 4th; the card-testing rule declines from the 6th.
func TestCardTestingIsFlaggedThenDeclined(t *testing.T) {
	c := require.New(t)
	f := newFixtureWith(t, cardTestingPolicy)

	var got []string
	for i := range 8 {
		resp := f.authorizeWith(fmt.Sprintf("ct%d", i), int64(100+i*50), atMerchant(fmt.Sprintf("m_%d", i), "US"))
		got = append(got, fmt.Sprintf("%s flagged=%t %v", resp.Decision, resp.Flagged, codesOf(resp)))
	}

	c.Equal([]string{
		"APPROVE flagged=false []",
		"APPROVE flagged=false []",
		"APPROVE flagged=false []",
		"APPROVE flagged=true [CARD_TESTING_PATTERN]",
		"APPROVE flagged=true [CARD_TESTING_PATTERN]",
		"DECLINE flagged=false [CARD_TESTING_PATTERN]",
		"DECLINE flagged=false [CARD_TESTING_PATTERN]",
		"DECLINE flagged=false [CARD_TESTING_PATTERN]",
	}, got)
}

// AC2 of SP04: three purchases of 490 at the same merchant with a 500 limit.
func TestSplitPurchaseIsFlagged(t *testing.T) {
	c := require.New(t)
	f := newFixtureWith(t, riskPolicy)

	first := f.authorize("split1", 49000)
	second := f.authorize("split2", 49000)
	third := f.authorize("split3", 49000)

	c.False(first.Flagged)
	c.True(second.Flagged)
	c.True(third.Flagged)
	c.Equal([]string{domain.ReasonSplitTransaction}, codesOf(third))
	c.Equal(domain.Approve, third.Decision)
}

// AC3 of SP04 and D-023: a stolen card is declined, and its reservation does
// not use up the budget.
func TestStolenCardIsDeclinedAndReleasesTheBudget(t *testing.T) {
	c := require.New(t)
	f := newFixtureWith(t, budgetAndRisk)
	f.history(scoring.ColdStart + 1)

	resp := f.authorizeWith("stolen", 30123, atMerchant("m_new", "BR"))

	c.Equal(domain.Decline, resp.Decision)
	c.Equal(75, resp.RiskScore)
	c.Equal([]string{domain.ReasonNewCountry, domain.ReasonNewMerchantCNP, domain.ReasonAmountAnomaly}, codesOf(resp))
	c.False(f.rdb.HExists(f.ctx, "vel:{"+f.card+"}:daily-spend:amt", f.id("stolen")).Val())
	c.Zero(f.rdb.ZScore(f.ctx, "vel:{"+f.card+"}:daily-spend:ts", f.id("stolen")).Val())
	c.Zero(f.rdb.Exists(f.ctx, "hold:{"+f.card+"}:"+f.id("stolen")).Val())
}

// AC4 of SP04: normal traffic from a known cardholder is approved with score 0.
func TestNormalTrafficIsApproved(t *testing.T) {
	c := require.New(t)
	f := newFixtureWith(t, budgetAndRisk)
	f.history(scoring.ColdStart + 1)

	resp := f.authorize("normal", 4520)

	c.Equal(domain.Approve, resp.Decision)
	c.Zero(resp.RiskScore)
	c.Empty(resp.Reasons)
}

// D-056: the same stolen-card pattern on a brand-new cardholder is not declined.
func TestANewCardholderIsNotDeclinedForBeingNew(t *testing.T) {
	c := require.New(t)
	f := newFixtureWith(t, budgetAndRisk)

	resp := f.authorizeWith("first", 30123, atMerchant("m_new", "BR"))

	c.Equal(domain.Approve, resp.Decision)
	c.Zero(resp.RiskScore)
}

func codesOf(resp domain.AuthResponse) []string {
	out := []string{}
	for _, r := range resp.Reasons {
		out = append(out, r.Code)
	}

	return out
}
