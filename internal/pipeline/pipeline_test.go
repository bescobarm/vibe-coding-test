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

func (s stageFunc) Name() string { return s.name }

func (s stageFunc) Evaluate(_ context.Context, e *Evaluation) error { return s.fn(e) }

func request() domain.AuthRequest {
	return domain.AuthRequest{AuthorizationID: "auth_1", AmountMinor: 4500}
}

func TestRunExecutesStagesInOrder(t *testing.T) {
	c := require.New(t)
	var order []string
	record := func(name string) Stage {
		return stageFunc{name, func(*Evaluation) error { order = append(order, name); return nil }}
	}

	err := New(record("a"), record("b")).Run(context.Background(), NewEvaluation(request()))

	c.NoError(err)
	c.Equal([]string{"a", "b"}, order)
}

func TestRunApprovesWhenNoStageDeclines(t *testing.T) {
	c := require.New(t)
	pass := stageFunc{"pass", func(*Evaluation) error { return nil }}
	e := NewEvaluation(request())
	e.PolicyVersion = 3

	c.NoError(New(pass).Run(context.Background(), e))

	resp := e.Response()
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
	after := stageFunc{"after", func(*Evaluation) error { ranAfter = true; return nil }}
	e := NewEvaluation(request())

	c.NoError(New(decline, after).Run(context.Background(), e))

	c.False(ranAfter, "stage after a decline must not run")
	c.True(e.Halted())
	resp := e.Response()
	c.Equal(domain.Decline, resp.Decision)
	c.Zero(resp.ApprovedAmountMinor)
	c.Equal([]domain.Reason{{Code: "CODE", Message: "message"}}, e.Reasons())
}

func TestAddReasonDoesNotStopThePipeline(t *testing.T) {
	c := require.New(t)
	flag := stageFunc{"flag", func(e *Evaluation) error { e.AddReason("ROUND_AMOUNT", "round"); return nil }}
	ran := false
	next := stageFunc{"next", func(*Evaluation) error { ran = true; return nil }}
	e := NewEvaluation(request())

	c.NoError(New(flag, next).Run(context.Background(), e))

	c.True(ran)
	c.False(e.Halted())
	c.Equal(domain.Approve, e.Decision())
	c.Equal([]domain.Reason{{Code: "ROUND_AMOUNT", Message: "round"}}, e.Reasons())
}

func TestStageErrorIsWrappedWithTheStageName(t *testing.T) {
	c := require.New(t)
	boom := errors.New("redis down")
	failing := stageFunc{"velocity", func(*Evaluation) error { return boom }}

	err := New(failing).Run(context.Background(), NewEvaluation(request()))

	c.ErrorIs(err, boom)
	c.EqualError(err, "stage velocity: redis down")
}
