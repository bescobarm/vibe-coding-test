package scoring

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bescobarm/card-auth-engine/internal/domain"
	"github.com/bescobarm/card-auth-engine/internal/pipeline"
	"github.com/bescobarm/card-auth-engine/internal/policy"
)

// regular is a cardholder with enough history (D-056) who has bought here,
// in this country, and usually spends about 4500 in this MCC.
var regular = domain.Signals{
	Approved: 20, CountrySeen: true, MerchantSeen: true,
	MCCCount: 10, MCCMeanMinor: 4500, MCCStdDevMinor: 500,
}

func evaluation(t *testing.T, amount int64, sig domain.Signals) *pipeline.Evaluation {
	t.Helper()
	doc, err := policy.Parse([]byte(`{"company_id": "acme-mx", "timezone": "America/Mexico_City",
		"controls": {"max_per_transaction": {"amount_minor": 50000, "currency": "USD"}},
		"risk": {"flag_at": 40, "decline_at": 70}}`))
	require.NoError(t, err)
	require.Empty(t, doc.Validate())

	e := pipeline.NewEvaluation(domain.AuthRequest{
		AuthorizationID: "a1", CardID: "card_123", AmountMinor: amount, Currency: "USD",
		Merchant:  domain.Merchant{ID: "m_987", MCC: "5734", Country: "US"},
		EntryMode: "ecommerce",
	})
	e.Policy, e.Controls, e.Signals = doc, doc.Controls, sig
	e.Card = domain.Card{ID: "card_123", CardholderID: "ch_ana"}
	e.ReceivedAt = time.Date(2026, 11, 14, 2, 30, 0, 0, time.UTC)
	return e
}

func score(c *require.Assertions, e *pipeline.Evaluation) {
	c.NoError(Stage{}.Evaluate(context.Background(), e))
}

func codes(e *pipeline.Evaluation) []string {
	var out []string
	for _, r := range e.Reasons() {
		out = append(out, r.Code)
	}

	return out
}

// AC4 of SP04: normal traffic is approved with a low score.
func TestNormalTrafficScoresZero(t *testing.T) {
	c := require.New(t)
	e := evaluation(t, 4321, regular)

	score(c, e)

	c.Zero(e.RiskScore)
	c.Empty(e.Reasons())
	c.False(e.Flagged)
	c.Equal(domain.Approve, e.Decision())
}

func TestCardTestingFiresOnASmallAmountAfterSmallOnes(t *testing.T) {
	c := require.New(t)
	sig := regular
	sig.SmallRecent = 3
	e := evaluation(t, 199, sig)

	score(c, e)

	c.Equal([]domain.Reason{{Code: domain.ReasonCardTestingPattern, Message: "4 small authorizations on this card in the last 10 minutes"}}, e.Reasons())
	c.Equal(WeightHigh, e.RiskScore)
	c.True(e.Flagged)
}

func TestCardTestingNeedsThreeEarlierSmallAmounts(t *testing.T) {
	c := require.New(t)
	sig := regular
	sig.SmallRecent = 2

	e := evaluation(t, 199, sig)
	score(c, e)

	c.Zero(e.RiskScore)
}

func TestCardTestingIgnoresANormalAmount(t *testing.T) {
	c := require.New(t)
	sig := regular
	sig.SmallRecent = 5

	e := evaluation(t, 4321, sig)
	score(c, e)

	c.Zero(e.RiskScore)
}

// AC2 of SP04: 490 + 490 + 490 with a 500 limit.
func TestSplitPurchaseFiresWhenPartsJustUnderTheLimitAddUpOverIt(t *testing.T) {
	c := require.New(t)
	sig := regular
	sig.SameMerchantToday = []int64{49000, 49000}
	sig.MCCMeanMinor = 49000

	e := evaluation(t, 49000, sig)
	score(c, e)

	c.Equal([]domain.Reason{{Code: domain.ReasonSplitTransaction,
		Message: "3 purchases today at this merchant total 147000, each above 80% of the 50000 USD limit"}}, e.Reasons())
	c.True(e.Flagged)
}

func TestSplitPurchaseIgnoresASmallEarlierPurchase(t *testing.T) {
	c := require.New(t)
	sig := regular
	sig.SameMerchantToday = []int64{1000}
	sig.MCCMeanMinor = 49000

	e := evaluation(t, 49000, sig)
	score(c, e)

	c.Zero(e.RiskScore)
}

func TestSplitPurchaseNeedsAnEarlierPurchaseToday(t *testing.T) {
	c := require.New(t)
	sig := regular
	sig.MCCMeanMinor = 49000

	e := evaluation(t, 49000, sig)
	score(c, e)

	c.Zero(e.RiskScore)
}

func TestSplitPurchaseNeedsALimitInTheSameCurrency(t *testing.T) {
	c := require.New(t)
	sig := regular
	sig.SameMerchantToday = []int64{49000}
	e := evaluation(t, 49000, sig)
	e.Controls.MaxPerTransaction = &policy.Money{AmountMinor: 50000, Currency: "MXN"}

	score(c, e)

	c.NotContains(codes(e), domain.ReasonSplitTransaction)
}

// AC3 of SP04: a stolen card used abroad, online, at a new merchant, for much more than usual.
func TestStolenCardIsDeclinedWithEveryReason(t *testing.T) {
	c := require.New(t)
	sig := regular
	sig.CountrySeen, sig.MerchantSeen = false, false

	e := evaluation(t, 30123, sig)
	score(c, e)

	c.Equal([]string{domain.ReasonNewCountry, domain.ReasonNewMerchantCNP, domain.ReasonAmountAnomaly}, codes(e))
	c.Equal(75, e.RiskScore)
	c.Equal(domain.Decline, e.Decision())
}

// D-056: a new cardholder isn't punished for being new.
func TestANewCardholderSkipsTheNoveltyFeatures(t *testing.T) {
	c := require.New(t)
	sig := domain.Signals{Approved: ColdStart - 1}

	e := evaluation(t, 30123, sig)
	score(c, e)

	c.Zero(e.RiskScore)
}

func TestNewMerchantNeedsTheCardNotPresent(t *testing.T) {
	c := require.New(t)
	sig := regular
	sig.MerchantSeen = false
	e := evaluation(t, 4321, sig)
	e.Request.EntryMode = "chip"

	score(c, e)

	c.Zero(e.RiskScore)
}

func TestNewMerchantFiresForAManualEntry(t *testing.T) {
	c := require.New(t)
	sig := regular
	sig.MerchantSeen = false
	e := evaluation(t, 4321, sig)
	e.Request.EntryMode = "manual"

	score(c, e)

	c.Equal([]domain.Reason{{Code: domain.ReasonNewMerchantCNP, Message: "First purchase at merchant m_987, card not present (manual)"}}, e.Reasons())
}

func TestAmountAnomalyNeedsHistoryInThisMCC(t *testing.T) {
	c := require.New(t)
	sig := regular
	sig.MCCCount = ColdStart - 1

	e := evaluation(t, 30123, sig)
	score(c, e)

	c.Zero(e.RiskScore)
}

func TestAmountAnomalyMessageShowsTheDeviation(t *testing.T) {
	c := require.New(t)

	e := evaluation(t, 6000, regular)
	score(c, e)

	c.Equal([]domain.Reason{{Code: domain.ReasonAmountAnomaly,
		Message: "Amount 6000 is 3.0 standard deviations above this cardholder's mean of 4500 for MCC 5734"}}, e.Reasons())
}

// A cardholder who always pays exactly the same has no deviation; the 10% floor
// keeps a small change from looking like an anomaly.
func TestAmountAnomalyIgnoresASmallChangeFromAFixedAmount(t *testing.T) {
	c := require.New(t)
	sig := regular
	sig.MCCStdDevMinor = 0

	e := evaluation(t, 4600, sig)
	score(c, e)

	c.Zero(e.RiskScore)
}

func TestRoundAmountFiresOnWholeHundreds(t *testing.T) {
	c := require.New(t)
	sig := regular
	sig.MCCMeanMinor = 20000

	e := evaluation(t, 20000, sig)
	score(c, e)

	c.Equal([]domain.Reason{{Code: domain.ReasonRoundAmount, Message: "Round amount 20000"}}, e.Reasons())
	c.Equal(WeightLow, e.RiskScore)
	c.False(e.Flagged)
}

func TestRoundAmountIgnoresSmallAmounts(t *testing.T) {
	c := require.New(t)

	e := evaluation(t, 5000, regular)
	score(c, e)

	c.Zero(e.RiskScore)
}

func TestTheScoreIsCappedAt100(t *testing.T) {
	c := require.New(t)
	sig := domain.Signals{Approved: 20, MCCCount: 10, MCCMeanMinor: 100, SmallRecent: 5}

	e := evaluation(t, 500, sig)
	e.Controls.MaxPerTransaction = &policy.Money{AmountMinor: 600, Currency: "USD"}
	e.Signals.SameMerchantToday = []int64{500}
	score(c, e)

	c.Len(e.Reasons(), 5)
	c.Equal(100, e.RiskScore)
}

func TestDefaultThresholdsApplyWithoutARiskSection(t *testing.T) {
	c := require.New(t)
	sig := regular
	sig.CountrySeen, sig.MerchantSeen = false, false
	e := evaluation(t, 30123, sig)
	e.Policy.Risk = nil

	score(c, e)

	c.Equal(domain.Decline, e.Decision())
}

func TestStageName(t *testing.T) {
	c := require.New(t)

	c.Equal("score", Stage{}.Name())
}

func TestQueryUsesTheEngineClockAndTheCompanyDay(t *testing.T) {
	c := require.New(t)
	e := evaluation(t, 4321, regular)
	mexico, err := time.LoadLocation("America/Mexico_City")
	c.NoError(err)

	q := Query(e)

	c.Equal(domain.SignalQuery{
		CardholderID: "ch_ana", CardID: "card_123", MerchantID: "m_987", Country: "US", MCC: "5734", Currency: "USD",
		HistorySince:  e.ReceivedAt.Add(-90 * 24 * time.Hour),
		RecentSince:   e.ReceivedAt.Add(-10 * time.Minute),
		DayStart:      time.Date(2026, 11, 13, 0, 0, 0, 0, mexico),
		SmallMaxMinor: 500,
	}, q)
}
