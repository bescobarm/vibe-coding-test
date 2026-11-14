package pipeline

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bescobarm/card-auth-engine/internal/domain"
)

type stageFunc struct {
	name string
	fn   func(*Evaluation) error
}

func (s stageFunc) GetName() string {
	return s.name
}

func (s stageFunc) Evaluate(_ context.Context, e *Evaluation) error {
	return s.fn(e)
}

func newRequest() domain.AuthRequest {
	return domain.AuthRequest{AuthorizationID: "auth_1", AmountMinor: 4500}
}

func TestRunExecutesStagesInOrder(t *testing.T) {
	c := require.New(t)
	var order []string
	record := func(name string) Stage {
		return stageFunc{name, func(*Evaluation) error {
			order = append(order, name)
			return nil
		}}
	}

	err := New(record("a"), record("b")).Run(context.Background(), NewEvaluation(newRequest()))

	c.NoError(err)
	c.Equal([]string{"a", "b"}, order)
}

func TestRunApprovesWhenNoStageDeclines(t *testing.T) {
	c := require.New(t)
	pass := stageFunc{"pass", func(*Evaluation) error {
		return nil
	}}
	evaluation := NewEvaluation(newRequest())
	evaluation.PolicyVersion = 3

	c.NoError(New(pass).Run(context.Background(), evaluation))

	resp := evaluation.BuildResponse()
	c.Equal(domain.Approve, resp.Decision)
	c.Equal(int64(4500), resp.ApprovedAmountMinor)
	c.Equal(3, resp.PolicyVersion)
	c.NotNil(resp.Reasons, "reasons must be non-nil so JSON shows []")
	c.Empty(resp.Reasons)
}

func TestDeclineStopsThePipeline(t *testing.T) {
	c := require.New(t)
	ranAfter := false
	decline := stageFunc{"decline", func(e *Evaluation) error {
		e.Decline("CODE", "message")
		return nil
	}}
	after := stageFunc{"after", func(*Evaluation) error {
		ranAfter = true
		return nil
	}}
	evaluation := NewEvaluation(newRequest())

	c.NoError(New(decline, after).Run(context.Background(), evaluation))

	c.False(ranAfter, "stage after a decline must not run")
	c.True(evaluation.IsHalted())
	resp := evaluation.BuildResponse()
	c.Equal(domain.Decline, resp.Decision)
	c.Zero(resp.ApprovedAmountMinor)
	c.Equal([]domain.Reason{{Code: "CODE", Message: "message"}}, evaluation.GetReasons())
}

func TestAddReasonDoesNotStopThePipeline(t *testing.T) {
	c := require.New(t)
	flag := stageFunc{"flag", func(e *Evaluation) error {
		e.AddReason("ROUND_AMOUNT", "round")
		return nil
	}}
	ran := false
	next := stageFunc{"next", func(*Evaluation) error {
		ran = true
		return nil
	}}
	evaluation := NewEvaluation(newRequest())

	c.NoError(New(flag, next).Run(context.Background(), evaluation))

	c.True(ran)
	c.False(evaluation.IsHalted())
	c.Equal(domain.Approve, evaluation.GetDecision())
	c.Equal([]domain.Reason{{Code: "ROUND_AMOUNT", Message: "round"}}, evaluation.GetReasons())
}

func TestStageErrorIsWrappedWithTheStageName(t *testing.T) {
	c := require.New(t)
	boom := errors.New("redis down")
	failing := stageFunc{"velocity", func(*Evaluation) error {
		return boom
	}}

	err := New(failing).Run(context.Background(), NewEvaluation(newRequest()))

	c.ErrorIs(err, boom)
	c.EqualError(err, "stage velocity: redis down")
}

var errUndo = errors.New("undo failed")

// registerUndo is a stage that, like velocity after a reservation, registers an undo.
func registerUndo(name string, log *[]string, err error) Stage {
	return stageFunc{name, func(e *Evaluation) error {
		e.OnDecline(func(context.Context) error {
			*log = append(*log, name)
			return err
		})

		return nil
	}}
}

func newDecliningScoreStage() Stage {
	return stageFunc{"score", func(e *Evaluation) error {
		e.AddReason("NEW_COUNTRY", "first purchase in BR")
		e.DeclineWithReasons()
		return nil
	}}
}

func TestADeclineUndoesEarlierStagesInReverseOrder(t *testing.T) {
	c := require.New(t)
	var undone []string
	evaluation := NewEvaluation(newRequest())

	err := New(registerUndo("first", &undone, nil), registerUndo("second", &undone, nil), newDecliningScoreStage()).Run(context.Background(), evaluation)

	c.NoError(err)
	c.Equal([]string{"second", "first"}, undone)
	c.Equal(domain.Decline, evaluation.GetDecision())
	c.Equal([]domain.Reason{{Code: "NEW_COUNTRY", Message: "first purchase in BR"}}, evaluation.GetReasons())
}

func TestAnApprovalUndoesNothing(t *testing.T) {
	c := require.New(t)
	var undone []string

	err := New(registerUndo("first", &undone, nil)).Run(context.Background(), NewEvaluation(newRequest()))

	c.NoError(err)
	c.Empty(undone)
}

func TestAFailedUndoIsAnError(t *testing.T) {
	c := require.New(t)
	var undone []string

	err := New(registerUndo("first", &undone, errUndo), newDecliningScoreStage()).Run(context.Background(), NewEvaluation(newRequest()))

	c.ErrorIs(err, errUndo)
}

func TestAFlaggedApprovalSaysSoInTheResponse(t *testing.T) {
	c := require.New(t)
	evaluation := NewEvaluation(newRequest())
	evaluation.Flagged, evaluation.RiskScore = true, 45

	resp := evaluation.BuildResponse()

	c.True(resp.Flagged)
	c.Equal(domain.Approve, resp.Decision)
	c.Equal(45, resp.RiskScore)
}
