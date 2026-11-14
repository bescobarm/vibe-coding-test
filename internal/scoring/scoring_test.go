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

func newEvaluation(t *testing.T, amount int64, sig domain.Signals) *pipeline.Evaluation {
	t.Helper()
	doc, err := policy.Parse([]byte(`{"company_id": "acme-mx", "timezone": "America/Mexico_City",
		"controls": {"max_per_transaction": {"amount_minor": 50000, "currency": "USD"}},
		"risk": {"flag_at": 40, "decline_at": 70}}`))
	require.NoError(t, err)
	require.Empty(t, doc.Validate())

	evaluation := pipeline.NewEvaluation(domain.AuthRequest{
		AuthorizationID: "a1", CardID: "card_123", AmountMinor: amount, Currency: "USD",
		Merchant:  domain.Merchant{ID: "m_987", MCC: "5734", Country: "US"},
		EntryMode: "ecommerce",
	})
	evaluation.Policy, evaluation.Controls, evaluation.Signals = doc, doc.Controls, sig
	evaluation.Card = domain.Card{ID: "card_123", CardholderID: "ch_ana"}
	evaluation.ReceivedAt = time.Date(2026, 11, 14, 2, 30, 0, 0, time.UTC)
	return evaluation
}

func runScore(c *require.Assertions, e *pipeline.Evaluation) {
	c.NoError(NewStage(GetDefaultWeights()).Evaluate(context.Background(), e))
}

func listCodes(e *pipeline.Evaluation) []string {
	var out []string
	for _, reason := range e.GetReasons() {
		out = append(out, reason.Code)
	}

	return out
}

// AC4 of SP04: normal traffic is approved with a low score.
func TestNormalTrafficScoresZero(t *testing.T) {
	c := require.New(t)
	evaluation := newEvaluation(t, 4321, regular)

	runScore(c, evaluation)

	c.Zero(evaluation.RiskScore)
	c.Empty(evaluation.GetReasons())
	c.False(evaluation.Flagged)
	c.Equal(domain.Approve, evaluation.GetDecision())
}

func TestCardTestingFiresOnASmallAmountAfterSmallOnes(t *testing.T) {
	c := require.New(t)
	sig := regular
	sig.SmallRecent = 3
	evaluation := newEvaluation(t, 199, sig)

	runScore(c, evaluation)

	c.Equal([]domain.Reason{{Code: domain.ReasonCardTestingPattern, Message: "4 small authorizations on this card in the last 10 minutes"}}, evaluation.GetReasons())
	c.Equal(WeightHigh, evaluation.RiskScore)
	c.True(evaluation.Flagged)
}

func TestCardTestingNeedsThreeEarlierSmallAmounts(t *testing.T) {
	c := require.New(t)
	sig := regular
	sig.SmallRecent = 2

	evaluation := newEvaluation(t, 199, sig)
	runScore(c, evaluation)

	c.Zero(evaluation.RiskScore)
}

func TestCardTestingIgnoresANormalAmount(t *testing.T) {
	c := require.New(t)
	sig := regular
	sig.SmallRecent = 5

	evaluation := newEvaluation(t, 4321, sig)
	runScore(c, evaluation)

	c.Zero(evaluation.RiskScore)
}

// AC2 of SP04: 490 + 490 + 490 with a 500 limit.
func TestSplitPurchaseFiresWhenPartsJustUnderTheLimitAddUpOverIt(t *testing.T) {
	c := require.New(t)
	sig := regular
	sig.SameMerchantToday = []int64{49000, 49000}
	sig.MCCMeanMinor = 49000

	evaluation := newEvaluation(t, 49000, sig)
	runScore(c, evaluation)

	c.Equal([]domain.Reason{{Code: domain.ReasonSplitTransaction,
		Message: "3 purchases today at this merchant total 147000, each above 80% of the 50000 USD limit"}}, evaluation.GetReasons())
	c.True(evaluation.Flagged)
}

func TestSplitPurchaseIgnoresASmallEarlierPurchase(t *testing.T) {
	c := require.New(t)
	sig := regular
	sig.SameMerchantToday = []int64{1000}
	sig.MCCMeanMinor = 49000

	evaluation := newEvaluation(t, 49000, sig)
	runScore(c, evaluation)

	c.Zero(evaluation.RiskScore)
}

func TestSplitPurchaseNeedsAnEarlierPurchaseToday(t *testing.T) {
	c := require.New(t)
	sig := regular
	sig.MCCMeanMinor = 49000

	evaluation := newEvaluation(t, 49000, sig)
	runScore(c, evaluation)

	c.Zero(evaluation.RiskScore)
}

func TestSplitPurchaseNeedsALimitInTheSameCurrency(t *testing.T) {
	c := require.New(t)
	sig := regular
	sig.SameMerchantToday = []int64{49000}
	evaluation := newEvaluation(t, 49000, sig)
	evaluation.Controls.MaxPerTransaction = &policy.Money{AmountMinor: 50000, Currency: "MXN"}

	runScore(c, evaluation)

	c.NotContains(listCodes(evaluation), domain.ReasonSplitTransaction)
}

// AC3 of SP04: a stolen card used abroad, online, at a new merchant, for much more than usual.
func TestStolenCardIsDeclinedWithEveryReason(t *testing.T) {
	c := require.New(t)
	sig := regular
	sig.CountrySeen, sig.MerchantSeen = false, false

	evaluation := newEvaluation(t, 30123, sig)
	runScore(c, evaluation)

	c.Equal([]string{
		domain.ReasonNewCountry,
		domain.ReasonNewMerchantCNP,
		domain.ReasonAmountAnomaly,
	}, listCodes(evaluation))
	c.Equal(75, evaluation.RiskScore)
	c.Equal(domain.Decline, evaluation.GetDecision())
}

// D-056: a new cardholder isn't punished for being new.
func TestANewCardholderSkipsTheNoveltyFeatures(t *testing.T) {
	c := require.New(t)
	sig := domain.Signals{Approved: ColdStart - 1}

	evaluation := newEvaluation(t, 30123, sig)
	runScore(c, evaluation)

	c.Zero(evaluation.RiskScore)
}

func TestNewMerchantNeedsTheCardNotPresent(t *testing.T) {
	c := require.New(t)
	sig := regular
	sig.MerchantSeen = false
	evaluation := newEvaluation(t, 4321, sig)
	evaluation.Request.EntryMode = "chip"

	runScore(c, evaluation)

	c.Zero(evaluation.RiskScore)
}

func TestNewMerchantFiresForAManualEntry(t *testing.T) {
	c := require.New(t)
	sig := regular
	sig.MerchantSeen = false
	evaluation := newEvaluation(t, 4321, sig)
	evaluation.Request.EntryMode = "manual"

	runScore(c, evaluation)

	c.Equal([]domain.Reason{{Code: domain.ReasonNewMerchantCNP, Message: "First purchase at merchant m_987, card not present (manual)"}}, evaluation.GetReasons())
}

func TestAmountAnomalyNeedsHistoryInThisMCC(t *testing.T) {
	c := require.New(t)
	sig := regular
	sig.MCCCount = ColdStart - 1

	evaluation := newEvaluation(t, 30123, sig)
	runScore(c, evaluation)

	c.Zero(evaluation.RiskScore)
}

func TestAmountAnomalyMessageShowsTheDeviation(t *testing.T) {
	c := require.New(t)

	evaluation := newEvaluation(t, 6000, regular)
	runScore(c, evaluation)

	c.Equal([]domain.Reason{{Code: domain.ReasonAmountAnomaly,
		Message: "Amount 6000 is 3.0 standard deviations above this cardholder's mean of 4500 for MCC 5734"}}, evaluation.GetReasons())
}

// A cardholder who always pays exactly the same has no deviation; the 10% floor
// keeps a small change from looking like an anomaly.
func TestAmountAnomalyIgnoresASmallChangeFromAFixedAmount(t *testing.T) {
	c := require.New(t)
	sig := regular
	sig.MCCStdDevMinor = 0

	evaluation := newEvaluation(t, 4600, sig)
	runScore(c, evaluation)

	c.Zero(evaluation.RiskScore)
}

func TestRoundAmountFiresOnWholeHundreds(t *testing.T) {
	c := require.New(t)
	sig := regular
	sig.MCCMeanMinor = 20000

	evaluation := newEvaluation(t, 20000, sig)
	runScore(c, evaluation)

	c.Equal([]domain.Reason{{Code: domain.ReasonRoundAmount, Message: "Round amount 20000"}}, evaluation.GetReasons())
	c.Equal(WeightLow, evaluation.RiskScore)
	c.False(evaluation.Flagged)
}

func TestRoundAmountIgnoresSmallAmounts(t *testing.T) {
	c := require.New(t)

	evaluation := newEvaluation(t, 5000, regular)
	runScore(c, evaluation)

	c.Zero(evaluation.RiskScore)
}

func TestTheScoreIsCappedAt100(t *testing.T) {
	c := require.New(t)
	sig := domain.Signals{Approved: 20, MCCCount: 10, MCCMeanMinor: 100, SmallRecent: 5}

	evaluation := newEvaluation(t, 500, sig)
	evaluation.Controls.MaxPerTransaction = &policy.Money{AmountMinor: 600, Currency: "USD"}
	evaluation.Signals.SameMerchantToday = []int64{500}
	runScore(c, evaluation)

	c.Len(evaluation.GetReasons(), 5)
	c.Equal(100, evaluation.RiskScore)
}

func TestDefaultThresholdsApplyWithoutARiskSection(t *testing.T) {
	c := require.New(t)
	sig := regular
	sig.CountrySeen, sig.MerchantSeen = false, false
	evaluation := newEvaluation(t, 30123, sig)
	evaluation.Policy.Risk = nil

	runScore(c, evaluation)

	c.Equal(domain.Decline, evaluation.GetDecision())
}

func TestStageName(t *testing.T) {
	c := require.New(t)

	c.Equal("score", NewStage(GetDefaultWeights()).GetName())
}

func TestQueryUsesTheEngineClockAndTheCompanyDay(t *testing.T) {
	c := require.New(t)
	evaluation := newEvaluation(t, 4321, regular)
	mexico, err := time.LoadLocation("America/Mexico_City")
	c.NoError(err)

	query := BuildSignalQuery(evaluation)

	c.Equal(domain.SignalQuery{
		CardholderID: "ch_ana", CardID: "card_123", MerchantID: "m_987", Country: "US", MCC: "5734", Currency: "USD",
		HistorySince:  evaluation.ReceivedAt.Add(-90 * 24 * time.Hour),
		RecentSince:   evaluation.ReceivedAt.Add(-10 * time.Minute),
		DayStart:      time.Date(2026, 11, 13, 0, 0, 0, 0, mexico),
		SmallMaxMinor: 500,
	}, query)
}

// D-065: a weight from the environment changes what a feature adds.
func TestAChangedWeightChangesTheScore(t *testing.T) {
	c := require.New(t)
	weights := GetDefaultWeights()
	weights.RoundAmount = 45
	sig := regular
	sig.MCCMeanMinor = 20000
	evaluation := newEvaluation(t, 20000, sig)

	c.NoError(NewStage(weights).Evaluate(context.Background(), evaluation))

	c.Equal(45, evaluation.RiskScore)
	c.True(evaluation.Flagged, "45 is over flag_at 40")
}

func TestTheDefaultWeightsAreValid(t *testing.T) {
	c := require.New(t)

	c.NoError(GetDefaultWeights().Validate())
}

func TestAWeightOver100IsInvalid(t *testing.T) {
	c := require.New(t)
	weights := GetDefaultWeights()
	weights.SplitPurchase = 101

	err := weights.Validate()

	c.ErrorIs(err, ErrInvalidWeight)
	c.ErrorContains(err, "SPLIT_TRANSACTION_PATTERN = 101")
}

func TestANegativeWeightIsInvalid(t *testing.T) {
	c := require.New(t)
	weights := GetDefaultWeights()
	weights.NewCountry = -1

	c.ErrorIs(weights.Validate(), ErrInvalidWeight)
}
