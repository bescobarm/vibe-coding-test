package controls

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bescobarm/card-auth-engine/internal/domain"
	"github.com/bescobarm/card-auth-engine/internal/pipeline"
)

func newEvaluationWithCard(found bool, status domain.CardStatus) *pipeline.Evaluation {
	evaluation := pipeline.NewEvaluation(domain.AuthRequest{AmountMinor: 100})
	evaluation.CardFound = found
	evaluation.Card.Status = status
	return evaluation
}

func TestCardStatusLetsAnActiveCardPass(t *testing.T) {
	c := require.New(t)
	evaluation := newEvaluationWithCard(true, domain.CardActive)

	c.NoError(CardStatus{}.Evaluate(context.Background(), evaluation))

	c.False(evaluation.IsHalted())
	c.Equal(domain.Approve, evaluation.GetDecision())
	c.Empty(evaluation.GetReasons())
}

func TestCardStatusDeclinesAFrozenCard(t *testing.T) {
	c := require.New(t)
	evaluation := newEvaluationWithCard(true, domain.CardFrozen)

	c.NoError(CardStatus{}.Evaluate(context.Background(), evaluation))

	c.Equal(domain.Decline, evaluation.GetDecision())
	c.Equal([]domain.Reason{{Code: domain.ReasonCardInactive, Message: "Card is frozen"}}, evaluation.GetReasons())
}

func TestCardStatusDeclinesATerminatedCard(t *testing.T) {
	c := require.New(t)
	evaluation := newEvaluationWithCard(true, domain.CardTerminated)

	c.NoError(CardStatus{}.Evaluate(context.Background(), evaluation))

	c.Equal(domain.Decline, evaluation.GetDecision())
	c.Equal([]domain.Reason{{Code: domain.ReasonCardInactive, Message: "Card is terminated"}}, evaluation.GetReasons())
}

func TestCardStatusDeclinesAnUnknownCard(t *testing.T) {
	c := require.New(t)
	evaluation := newEvaluationWithCard(false, "")

	c.NoError(CardStatus{}.Evaluate(context.Background(), evaluation))

	c.Equal(domain.Decline, evaluation.GetDecision())
	c.Equal([]domain.Reason{{Code: domain.ReasonCardInactive, Message: "Card not found"}}, evaluation.GetReasons())
}

func TestCardStatusName(t *testing.T) {
	c := require.New(t)

	c.Equal("card_status", CardStatus{}.GetName())
}
