// Package scoring computes an explainable 0–100 anomaly score from six
// features (D-005). Every feature that fires adds its points and a reason.
package scoring

import (
	"errors"
	"fmt"
	"math"

	"github.com/bescobarm/card-auth-engine/internal/domain"
	"github.com/bescobarm/card-auth-engine/internal/pipeline"
)

// Default weights (D-057): high, medium and low. Each feature's weight can be
// overridden with an env var to tune against the SP06 simulator (D-065).
const (
	WeightHigh   = 40
	WeightMedium = 25
	WeightLow    = 10
)

// Weights are the points each feature adds when it fires.
type Weights struct {
	CardTesting    int
	SplitPurchase  int
	NewCountry     int
	NewMerchantCNP int
	AmountAnomaly  int
	RoundAmount    int
}

// ErrInvalidWeight means a weight is outside 0–100.
var ErrInvalidWeight = errors.New("score weight must be between 0 and 100")

// GetDefaultWeights returns the weights in code (D-057).
func GetDefaultWeights() Weights {
	return Weights{
		CardTesting:    WeightHigh,
		SplitPurchase:  WeightHigh,
		NewCountry:     WeightMedium,
		NewMerchantCNP: WeightMedium,
		AmountAnomaly:  WeightMedium,
		RoundAmount:    WeightLow,
	}
}

// Validate reports the first weight outside 0–100.
func (w Weights) Validate() error {
	for _, feature := range ListFeatures(w) {
		if feature.Points < 0 || feature.Points > maxScore {
			return fmt.Errorf("%w: %s = %d", ErrInvalidWeight, feature.Code, feature.Points)
		}
	}

	return nil
}

// Feature parameters.
const (
	// ColdStart is how many approved purchases a cardholder needs before the
	// novelty features count (D-056).
	ColdStart = 5

	cardTestingMin   = 3   // small authorizations already in the recent window
	smallMaxMinor    = 500 // a card-testing-sized amount (5.00)
	splitSharePct    = 80  // each part is over 80% of the per-transaction limit
	percent          = 100
	anomalyZ         = 3.0 // standard deviations above the mean
	minStdDevShare   = 0.1 // floor for the deviation: 10% of the mean
	roundUnitMinor   = 10000
	roundMinMinor    = 10000
	entryEcommerce   = "ecommerce"
	entryManual      = "manual"
	fmtCardTesting   = "%d small authorizations on this card in the last 10 minutes"
	fmtSplit         = "%d purchases today at this merchant total %s, each above %d%% of the %s limit"
	fmtNewCountry    = "First purchase in %s for this cardholder"
	fmtNewMerchant   = "First purchase at merchant %s, card not present (%s)"
	fmtAmountAnomaly = "Amount %s is %.1f standard deviations above this cardholder's mean of %s for MCC %s"
	fmtRoundAmount   = "Round amount %s"
)

// Feature is one explainable signal.
type Feature struct {
	Code   string
	Points int
	Check  func(e *pipeline.Evaluation) (fired bool, message string)
}

// ListFeatures returns the six features with their weights, in the order their
// reasons are reported.
func ListFeatures(w Weights) []Feature {
	return []Feature{
		{Code: domain.ReasonCardTestingPattern, Points: w.CardTesting, Check: detectCardTesting},
		{Code: domain.ReasonSplitTransaction, Points: w.SplitPurchase, Check: detectSplitPurchase},
		{Code: domain.ReasonNewCountry, Points: w.NewCountry, Check: detectNewCountry},
		{Code: domain.ReasonNewMerchantCNP, Points: w.NewMerchantCNP, Check: detectNewMerchantCNP},
		{Code: domain.ReasonAmountAnomaly, Points: w.AmountAnomaly, Check: detectAmountAnomaly},
		{Code: domain.ReasonRoundAmount, Points: w.RoundAmount, Check: detectRoundAmount},
	}
}

// detectCardTesting: a small amount after several small ones on the same card (D-025).
func detectCardTesting(e *pipeline.Evaluation) (bool, string) {
	recent := e.Signals.SmallRecent
	fired := e.Request.AmountMinor <= smallMaxMinor && recent >= cardTestingMin

	return fired, fmt.Sprintf(fmtCardTesting, recent+1)
}

// detectSplitPurchase: one purchase split in parts that each stay just under the limit.
func detectSplitPurchase(e *pipeline.Evaluation) (bool, string) {
	limit := e.Controls.MaxPerTransaction
	if limit == nil || limit.Currency != e.Request.Currency || len(e.Signals.SameMerchantToday) == 0 {
		return false, ""
	}

	floor := limit.AmountMinor * splitSharePct / percent
	total := e.Request.AmountMinor

	for _, earlier := range e.Signals.SameMerchantToday {
		if earlier <= floor {
			return false, ""
		}

		total += earlier
	}

	fired := e.Request.AmountMinor > floor && total > limit.AmountMinor
	parts := len(e.Signals.SameMerchantToday) + 1

	return fired, fmt.Sprintf(fmtSplit, parts, domain.FormatMoney(total, limit.Currency), splitSharePct,
		domain.FormatMoney(limit.AmountMinor, limit.Currency))
}

func detectNewCountry(e *pipeline.Evaluation) (bool, string) {
	fired := isKnownCardholder(e) && !e.Signals.CountrySeen

	return fired, fmt.Sprintf(fmtNewCountry, e.Request.Merchant.Country)
}

func detectNewMerchantCNP(e *pipeline.Evaluation) (bool, string) {
	mode := e.Request.EntryMode
	notPresent := mode == entryEcommerce || mode == entryManual
	fired := isKnownCardholder(e) && notPresent && !e.Signals.MerchantSeen

	return fired, fmt.Sprintf(fmtNewMerchant, e.Request.Merchant.ID, mode)
}

// detectAmountAnomaly: far above what this cardholder usually spends in this MCC.
// The deviation has a floor so a cardholder who always pays the same amount
// isn't flagged for a small change.
func detectAmountAnomaly(e *pipeline.Evaluation) (bool, string) {
	signals := e.Signals
	if !isKnownCardholder(e) || signals.MCCCount < ColdStart {
		return false, ""
	}

	spread := math.Max(signals.MCCStdDevMinor, signals.MCCMeanMinor*minStdDevShare)
	zScore := (float64(e.Request.AmountMinor) - signals.MCCMeanMinor) / spread

	currency := e.Request.Currency
	mean := domain.FormatMoney(int64(math.Round(signals.MCCMeanMinor)), currency)

	return zScore >= anomalyZ, fmt.Sprintf(fmtAmountAnomaly, domain.FormatMoney(e.Request.AmountMinor, currency), zScore, mean,
		e.Request.Merchant.MCC)
}

func detectRoundAmount(e *pipeline.Evaluation) (bool, string) {
	amount := e.Request.AmountMinor
	fired := amount >= roundMinMinor && amount%roundUnitMinor == 0

	return fired, fmt.Sprintf(fmtRoundAmount, domain.FormatMoney(amount, e.Request.Currency))
}

// isKnownCardholder: the cardholder has enough history for novelty to mean something (D-056).
func isKnownCardholder(e *pipeline.Evaluation) bool {
	return e.Signals.Approved >= ColdStart
}
