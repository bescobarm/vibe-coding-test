package scoring

import (
	"context"
	"time"

	"github.com/bescobarm/card-auth-engine/internal/domain"
	"github.com/bescobarm/card-auth-engine/internal/pipeline"
)

// Default thresholds when the policy has no `risk` section (D-057).
const (
	DefaultFlagAt    = 40
	DefaultDeclineAt = 70
	maxScore         = 100

	stageName = "score"

	historyWindow = 90 * 24 * time.Hour // D-059
	recentWindow  = 10 * time.Minute
)

// Stage adds the points of every feature that fires, caps the score at 100,
// and maps it to approve, approve + flag, or decline (FR4, FR5).
type Stage struct {
	weights Weights
}

// NewStage returns the score stage. Validate the weights first.
func NewStage(w Weights) Stage {
	return Stage{weights: w}
}

// GetName implements pipeline.Stage.
func (Stage) GetName() string {
	return stageName
}

// Evaluate implements pipeline.Stage. Fired features are reasons on a flag or
// a decline; a decline undoes the velocity reservation (D-023).
func (s Stage) Evaluate(_ context.Context, e *pipeline.Evaluation) error {
	score := 0

	for _, feature := range ListFeatures(s.weights) {
		fired, msg := feature.Check(e)
		if fired {
			score += feature.Points
			e.AddReason(feature.Code, msg)
		}
	}

	e.RiskScore = min(score, maxScore)
	flagAt, declineAt := getThresholds(e)

	switch {
	case e.RiskScore >= declineAt:
		e.DeclineWithReasons()
	case e.RiskScore >= flagAt:
		e.Flagged = true
	}

	return nil
}

func getThresholds(e *pipeline.Evaluation) (int, int) {
	risk := e.Policy.Risk
	if risk == nil {
		return DefaultFlagAt, DefaultDeclineAt
	}

	return risk.FlagAt, risk.DeclineAt
}

// BuildSignalQuery says which history the features need. Windows use the engine clock
// (D-045) and the company's time zone for "today" (D-026).
func BuildSignalQuery(e *pipeline.Evaluation) domain.SignalQuery {
	now := e.ReceivedAt
	local := now.In(e.Policy.GetLocation())

	return domain.SignalQuery{
		CardholderID:  e.Card.CardholderID,
		CardID:        e.Card.ID,
		MerchantID:    e.Request.Merchant.ID,
		Country:       e.Request.Merchant.Country,
		MCC:           e.Request.Merchant.MCC,
		Currency:      e.Request.Currency,
		HistorySince:  now.Add(-historyWindow),
		RecentSince:   now.Add(-recentWindow),
		DayStart:      time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, local.Location()),
		SmallMaxMinor: smallMaxMinor,
	}
}
