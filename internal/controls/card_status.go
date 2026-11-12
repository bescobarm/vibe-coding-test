// Package controls holds the hard controls: deterministic, in-memory checks
// that decline immediately with a reason code.
package controls

import (
	"context"
	"fmt"

	"github.com/bescobarm/card-auth-engine/internal/domain"
	"github.com/bescobarm/card-auth-engine/internal/pipeline"
)

// CardStatus declines unknown cards and cards that are not active
// (for example an ex-employee's card that was terminated).
type CardStatus struct{}

func (CardStatus) Name() string { return "card_status" }

func (CardStatus) Evaluate(_ context.Context, e *pipeline.Evaluation) error {
	switch {
	case !e.CardFound:
		e.Decline(domain.ReasonCardInactive, "Card not found")
	case e.Card.Status != domain.CardActive:
		e.Decline(domain.ReasonCardInactive, fmt.Sprintf("Card is %s", e.Card.Status))
	}

	return nil
}
