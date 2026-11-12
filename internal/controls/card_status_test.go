package controls

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bescobarm/card-auth-engine/internal/domain"
	"github.com/bescobarm/card-auth-engine/internal/pipeline"
)

func evaluationWithCard(found bool, status domain.CardStatus) *pipeline.Evaluation {
	e := pipeline.NewEvaluation(domain.AuthRequest{AmountMinor: 100})
	e.CardFound = found
	e.Card.Status = status
	return e
}

func TestCardStatusLetsAnActiveCardPass(t *testing.T) {
	c := require.New(t)
	e := evaluationWithCard(true, domain.CardActive)

	c.NoError(CardStatus{}.Evaluate(context.Background(), e))

	c.False(e.Halted())
	c.Equal(domain.Approve, e.Decision())
	c.Empty(e.Reasons())
}

func TestCardStatusDeclinesAFrozenCard(t *testing.T) {
	c := require.New(t)
	e := evaluationWithCard(true, domain.CardFrozen)

	c.NoError(CardStatus{}.Evaluate(context.Background(), e))

	c.Equal(domain.Decline, e.Decision())
	c.Equal([]domain.Reason{{Code: domain.ReasonCardInactive, Message: "Card is frozen"}}, e.Reasons())
}

func TestCardStatusDeclinesATerminatedCard(t *testing.T) {
	c := require.New(t)
	e := evaluationWithCard(true, domain.CardTerminated)

	c.NoError(CardStatus{}.Evaluate(context.Background(), e))

	c.Equal(domain.Decline, e.Decision())
	c.Equal([]domain.Reason{{Code: domain.ReasonCardInactive, Message: "Card is terminated"}}, e.Reasons())
}

func TestCardStatusDeclinesAnUnknownCard(t *testing.T) {
	c := require.New(t)
	e := evaluationWithCard(false, "")

	c.NoError(CardStatus{}.Evaluate(context.Background(), e))

	c.Equal(domain.Decline, e.Decision())
	c.Equal([]domain.Reason{{Code: domain.ReasonCardInactive, Message: "Card not found"}}, e.Reasons())
}

func TestCardStatusName(t *testing.T) {
	c := require.New(t)

	c.Equal("card_status", CardStatus{}.Name())
}
