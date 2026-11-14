// Package scoring computes an explainable 0–100 anomaly score from six
// features (D-005). Every feature that fires adds its points and a reason.
package scoring

import (
	"fmt"
	"math"

	"github.com/bescobarm/card-auth-engine/internal/domain"
	"github.com/bescobarm/card-auth-engine/internal/pipeline"
)

// Weights (D-057): defaults in code, tuned against the SP06 simulator.
const (
	WeightHigh   = 40
	WeightMedium = 25
	WeightLow    = 10
)

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
	fmtSplit         = "%d purchases today at this merchant total %d, each above %d%% of the %d %s limit"
	fmtNewCountry    = "First purchase in %s for this cardholder"
	fmtNewMerchant   = "First purchase at merchant %s, card not present (%s)"
	fmtAmountAnomaly = "Amount %d is %.1f standard deviations above this cardholder's mean of %.0f for MCC %s"
	fmtRoundAmount   = "Round amount %d"
)

// Feature is one explainable signal.
type Feature struct {
	Code   string
	Points int
	Check  func(e *pipeline.Evaluation) (fired bool, message string)
}

// Features returns the six features in the order their reasons are reported.
func Features() []Feature {
	return []Feature{
		{Code: domain.ReasonCardTestingPattern, Points: WeightHigh, Check: cardTesting},
		{Code: domain.ReasonSplitTransaction, Points: WeightHigh, Check: splitPurchase},
		{Code: domain.ReasonNewCountry, Points: WeightMedium, Check: newCountry},
		{Code: domain.ReasonNewMerchantCNP, Points: WeightMedium, Check: newMerchantCNP},
		{Code: domain.ReasonAmountAnomaly, Points: WeightMedium, Check: amountAnomaly},
		{Code: domain.ReasonRoundAmount, Points: WeightLow, Check: roundAmount},
	}
}

// cardTesting: a small amount after several small ones on the same card (D-025).
func cardTesting(e *pipeline.Evaluation) (bool, string) {
	recent := e.Signals.SmallRecent
	fired := e.Request.AmountMinor <= smallMaxMinor && recent >= cardTestingMin

	return fired, fmt.Sprintf(fmtCardTesting, recent+1)
}

// splitPurchase: one purchase split in parts that each stay just under the limit.
func splitPurchase(e *pipeline.Evaluation) (bool, string) {
	limit := e.Controls.MaxPerTransaction
	if limit == nil || limit.Currency != e.Request.Currency || len(e.Signals.SameMerchantToday) == 0 {
		return false, ""
	}

	floor := limit.AmountMinor * splitSharePct / percent
	total := e.Request.AmountMinor

	for _, a := range e.Signals.SameMerchantToday {
		if a <= floor {
			return false, ""
		}

		total += a
	}

	fired := e.Request.AmountMinor > floor && total > limit.AmountMinor
	parts := len(e.Signals.SameMerchantToday) + 1

	return fired, fmt.Sprintf(fmtSplit, parts, total, splitSharePct, limit.AmountMinor, limit.Currency)
}

func newCountry(e *pipeline.Evaluation) (bool, string) {
	fired := known(e) && !e.Signals.CountrySeen

	return fired, fmt.Sprintf(fmtNewCountry, e.Request.Merchant.Country)
}

func newMerchantCNP(e *pipeline.Evaluation) (bool, string) {
	mode := e.Request.EntryMode
	notPresent := mode == entryEcommerce || mode == entryManual
	fired := known(e) && notPresent && !e.Signals.MerchantSeen

	return fired, fmt.Sprintf(fmtNewMerchant, e.Request.Merchant.ID, mode)
}

// amountAnomaly: far above what this cardholder usually spends in this MCC.
// The deviation has a floor so a cardholder who always pays the same amount
// isn't flagged for a small change.
func amountAnomaly(e *pipeline.Evaluation) (bool, string) {
	s := e.Signals
	if !known(e) || s.MCCCount < ColdStart {
		return false, ""
	}

	spread := math.Max(s.MCCStdDevMinor, s.MCCMeanMinor*minStdDevShare)
	z := (float64(e.Request.AmountMinor) - s.MCCMeanMinor) / spread

	return z >= anomalyZ, fmt.Sprintf(fmtAmountAnomaly, e.Request.AmountMinor, z, s.MCCMeanMinor, e.Request.Merchant.MCC)
}

func roundAmount(e *pipeline.Evaluation) (bool, string) {
	amount := e.Request.AmountMinor
	fired := amount >= roundMinMinor && amount%roundUnitMinor == 0

	return fired, fmt.Sprintf(fmtRoundAmount, amount)
}

// known: the cardholder has enough history for novelty to mean something (D-056).
func known(e *pipeline.Evaluation) bool {
	return e.Signals.Approved >= ColdStart
}
