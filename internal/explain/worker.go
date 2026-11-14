package explain

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"github.com/bescobarm/card-auth-engine/internal/domain"
)

const (
	// DefaultPoll is the backup poll when no doorbell rings (D-034).
	DefaultPoll = 2 * time.Second

	// minGap spaces passes over the outbox. Under load every decision rings the
	// doorbell; without a gap the worker would query Postgres back to back and
	// slow the authorizations down (I-034). A ring during the gap is kept.
	minGap = 200 * time.Millisecond

	batchSize   = 20
	parallelism = 8               // explanations in flight at once; one slow model call doesn't hold up the rest
	jobTimeout  = 3 * time.Second // shorter than the engine's shutdown timeout, so a job can finish (D-033)

	logKeyAuthorizationID = "authorization_id"
)

// Pending is a decision event from the outbox, with the request it answered.
type Pending struct {
	OutboxID int64
	Response domain.AuthResponse
	Request  domain.AuthRequest
}

// Outbox gives the worker the unpublished decisions and records each one done.
type Outbox interface {
	// PublishUnexplained marks every pending approval that needs no
	// explanation as published, in one step.
	PublishUnexplained(ctx context.Context) (int64, error)
	// ListPendingDecisions returns pending declines and flags, oldest first.
	ListPendingDecisions(ctx context.Context, limit int) ([]Pending, error)
	// CompleteDecision stores the explanation, if any, and marks the event
	// published, in one transaction. An explanation is never overwritten.
	CompleteDecision(ctx context.Context, outboxID int64, authorizationID string, explanation *Explanation) error
}

// Worker explains declines and flags in the background (D-028). A decision
// rings its doorbell; a slow poll is the backup (D-034).
type Worker struct {
	outbox    Outbox
	explainer Explainer
	log       *slog.Logger
	poll      time.Duration
	doorbell  chan struct{}
}

// NewWorker returns a worker that polls every poll when the doorbell is quiet.
func NewWorker(outbox Outbox, explainer Explainer, log *slog.Logger, poll time.Duration) *Worker {
	return &Worker{outbox: outbox, explainer: explainer, log: log, poll: poll, doorbell: make(chan struct{}, 1)}
}

// Ring wakes the worker. It never blocks: when a ring is already waiting,
// one more adds nothing, so a decision is never slowed down (SP07 AC1).
func (w *Worker) Ring() {
	select {
	case w.doorbell <- struct{}{}:
	default:
	}
}

// Run works until ctx ends. The job in progress finishes first (D-033).
func (w *Worker) Run(ctx context.Context) {
	for {
		err := w.drain(ctx)
		if err != nil && ctx.Err() == nil {
			w.log.Error("explainer: outbox unavailable", "err", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(minGap):
		}

		select {
		case <-ctx.Done():
			return
		case <-w.doorbell:
		case <-time.After(w.poll):
		}
	}
}

// drain handles pending decisions until none are left or ctx ends. Approvals
// are published in bulk first; declines and flags are explained in batches,
// several at a time.
func (w *Worker) drain(ctx context.Context) error {
	for ctx.Err() == nil {
		_, err := w.outbox.PublishUnexplained(ctx)
		if err != nil {
			return err
		}

		batch, err := w.outbox.ListPendingDecisions(ctx, batchSize)
		if err != nil {
			return err
		}

		if len(batch) == 0 {
			return nil
		}

		err = w.processBatch(ctx, batch)
		if err != nil {
			return err
		}
	}

	return nil
}

// processBatch explains a batch with at most `parallelism` jobs in flight. No
// job starts once ctx has ended; jobs already running finish (D-033).
func (w *Worker) processBatch(ctx context.Context, batch []Pending) error {
	slots := make(chan struct{}, parallelism)
	errs := make([]error, len(batch))

	var jobs sync.WaitGroup

	for index, pending := range batch {
		if !acquireSlot(ctx, slots) {
			break
		}

		jobs.Go(func() {
			defer func() {
				<-slots
			}()

			errs[index] = w.process(ctx, pending)
		})
	}

	jobs.Wait()

	return errors.Join(errs...)
}

// acquireSlot waits for a free slot, and gives up when ctx ends, so no job
// starts after the worker is told to stop.
func acquireSlot(ctx context.Context, slots chan<- struct{}) bool {
	if ctx.Err() != nil {
		return false
	}

	select {
	case slots <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

// process explains one decision, if it needs it, and records it done. The job
// runs detached from ctx, so stopping the worker doesn't cut it in half.
func (w *Worker) process(ctx context.Context, pending Pending) error {
	jobCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), jobTimeout)
	defer cancel()

	var explanation *Explanation

	if NeedsExplanation(pending.Response) {
		text, err := w.explainSafely(jobCtx, buildDecision(pending))
		if err != nil {
			return fmt.Errorf("explain %s: %w", pending.Response.AuthorizationID, err)
		}

		explanation = &text
	}

	return w.outbox.CompleteDecision(jobCtx, pending.OutboxID, pending.Response.AuthorizationID, explanation)
}

// explainSafely turns a panic into a failed explanation (D-032): the panic is
// logged with its stack, and the worker and the engine keep running.
func (w *Worker) explainSafely(ctx context.Context, decision Decision) (explanation Explanation, err error) {
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}

		w.log.Error("explainer panicked", logKeyAuthorizationID, decision.AuthorizationID,
			"panic", fmt.Sprint(recovered), "stack", string(debug.Stack()))

		explanation, err = Explanation{Failed: true, ReasonCodes: listCodes(decision.Reasons)}, nil
	}()

	return w.explainer.Explain(ctx, decision)
}

func buildDecision(pending Pending) Decision {
	response := pending.Response

	return Decision{
		AuthorizationID: response.AuthorizationID,
		Decision:        response.Decision,
		Flagged:         response.Flagged,
		RiskScore:       response.RiskScore,
		PolicyVersion:   response.PolicyVersion,
		Request:         pending.Request,
		Reasons:         response.Reasons,
	}
}
