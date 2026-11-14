// Package controls holds the hard controls: deterministic, in-memory checks
// that decline immediately with a reason code.
package controls

import (
	"context"
	"fmt"

	"github.com/bescobarm/card-auth-engine/internal/domain"
	"github.com/bescobarm/card-auth-engine/internal/pipeline"
)

const (
	msgCardNotFound = "Card not found"
	fmtCardStatus   = "Card is %s"
)

// CardStatus declines unknown cards and cards that are not active
// (for example an ex-employee's card that was terminated).
type CardStatus struct{}

// GetName implements pipeline.Stage.
func (CardStatus) GetName() string {
	return "card_status"
}

// Evaluate declines a card that is unknown or not active.
func (CardStatus) Evaluate(_ context.Context, e *pipeline.Evaluation) error {
	switch {
	case !e.CardFound:
		e.Decline(domain.ReasonCardInactive, msgCardNotFound)
	case e.Card.Status != domain.CardActive:
		e.Decline(domain.ReasonCardInactive, fmt.Sprintf(fmtCardStatus, e.Card.Status))
	}

	return nil
}
