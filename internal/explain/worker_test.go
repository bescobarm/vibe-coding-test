package explain

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bescobarm/card-auth-engine/internal/domain"
)

var errOutboxDown = errors.New("outbox down")

// fakeOutbox holds pending events and records what the worker completed.
type fakeOutbox struct {
	mu        sync.Mutex
	pending   []Pending
	completed map[int64]*Explanation
	listErr   error
}

func newFakeOutbox(pending ...Pending) *fakeOutbox {
	return &fakeOutbox{pending: pending, completed: map[int64]*Explanation{}}
}

func (f *fakeOutbox) PublishUnexplained(context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.listErr != nil {
		return 0, f.listErr
	}

	var published int64

	for _, pending := range f.pending {
		_, done := f.completed[pending.OutboxID]
		if !done && !NeedsExplanation(pending.Response) {
			f.completed[pending.OutboxID] = nil
			published++
		}
	}

	return published, nil
}

func (f *fakeOutbox) ListPendingDecisions(_ context.Context, limit int) ([]Pending, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.listErr != nil {
		return nil, f.listErr
	}

	var batch []Pending
	for _, pending := range f.pending {
		_, done := f.completed[pending.OutboxID]
		if !done && len(batch) < limit {
			batch = append(batch, pending)
		}
	}

	return batch, nil
}

func (f *fakeOutbox) CompleteDecision(_ context.Context, outboxID int64, _ string, explanation *Explanation) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.completed[outboxID] = explanation
	return nil
}

func (f *fakeOutbox) add(pending Pending) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.pending = append(f.pending, pending)
}

func (f *fakeOutbox) readCompleted() map[int64]*Explanation {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.completed
}

// panicExplainer stands for a bug in the explainer (D-032).
type panicExplainer struct{}

func (panicExplainer) Explain(context.Context, Decision) (Explanation, error) {
	panic("explainer bug")
}

type failingExplainer struct{}

func (failingExplainer) Explain(context.Context, Decision) (Explanation, error) {
	return Explanation{}, errModelDown
}

func newDecline(outboxID int64) Pending {
	decision := newSplitDecision()

	return Pending{
		OutboxID: outboxID,
		Response: domain.AuthResponse{AuthorizationID: "a1", Decision: domain.Decline, Reasons: decision.Reasons},
		Request:  decision.Request,
	}
}

func newQuietLogger(buffer *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buffer, nil))
}

func startWorker(t *testing.T, worker *Worker) (context.CancelFunc, *sync.WaitGroup) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())

	var running sync.WaitGroup

	running.Go(func() {
		worker.Run(ctx)
	})

	return cancel, &running
}

// AC2 and AC8 of SP07: a decline is explained right away, and an approval is
// only marked published.
func TestTheWorkerExplainsDeclinesAndSkipsApprovals(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := require.New(t)
		approval := Pending{OutboxID: 2, Response: domain.AuthResponse{AuthorizationID: "a2", Decision: domain.Approve}}
		outbox := newFakeOutbox(newDecline(1), approval)
		var logs bytes.Buffer
		worker := NewWorker(outbox, TemplateExplainer{}, newQuietLogger(&logs), DefaultPoll)
		stop, running := startWorker(t, worker)

		synctest.Wait()
		stop()
		running.Wait()

		completed := outbox.readCompleted()
		c.Len(completed, 2)
		c.NotNil(completed[1])
		c.True(completed[1].Validated)
		c.Nil(completed[2], "an approval is published without an explanation")
	})
}

// AC8 of SP07 (D-034): the doorbell wakes the worker at once; the poll is the backup.
func TestTheDoorbellWakesTheWorkerBeforeThePoll(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := require.New(t)
		outbox := newFakeOutbox()
		worker := NewWorker(outbox, TemplateExplainer{}, newQuietLogger(&bytes.Buffer{}), time.Hour)
		stop, running := startWorker(t, worker)
		synctest.Wait()

		outbox.add(newDecline(1))
		worker.Ring()
		time.Sleep(minGap)
		synctest.Wait()

		c.Len(outbox.readCompleted(), 1, "explained within the gap, without waiting an hour for the poll")
		stop()
		running.Wait()
	})
}

func TestThePollExplainsWhenNoDoorbellRings(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := require.New(t)
		outbox := newFakeOutbox()
		worker := NewWorker(outbox, TemplateExplainer{}, newQuietLogger(&bytes.Buffer{}), DefaultPoll)
		stop, running := startWorker(t, worker)
		synctest.Wait()

		outbox.add(newDecline(1))
		time.Sleep(minGap + DefaultPoll + time.Millisecond)
		synctest.Wait()

		c.Len(outbox.readCompleted(), 1)
		stop()
		running.Wait()
	})
}

// AC1 of SP07: ringing never blocks a decision, even when a ring is already waiting.
func TestRingingNeverBlocks(t *testing.T) {
	c := require.New(t)
	worker := NewWorker(newFakeOutbox(), TemplateExplainer{}, newQuietLogger(&bytes.Buffer{}), DefaultPoll)

	for range 100 {
		worker.Ring()
	}

	c.Len(worker.doorbell, 1)
}

// AC6 of SP07 (D-032): a panic is logged with its stack, the explanation is
// marked failed, and the worker keeps going.
func TestAPanicIsRecoveredAndMarkedFailed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := require.New(t)
		outbox := newFakeOutbox(newDecline(1), newDecline(2))
		var logs bytes.Buffer
		worker := NewWorker(outbox, panicExplainer{}, newQuietLogger(&logs), DefaultPoll)
		stop, running := startWorker(t, worker)

		synctest.Wait()
		stop()
		running.Wait()

		completed := outbox.readCompleted()
		c.Len(completed, 2, "the second job ran after the first panicked")
		c.True(completed[1].Failed)
		c.Contains(logs.String(), `"msg":"explainer panicked"`)
		c.Contains(logs.String(), `"stack":"goroutine`)
	})
}

func TestAnExplainerErrorLeavesTheEventForLater(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := require.New(t)
		outbox := newFakeOutbox(newDecline(1))
		var logs bytes.Buffer
		worker := NewWorker(outbox, failingExplainer{}, newQuietLogger(&logs), DefaultPoll)
		stop, running := startWorker(t, worker)

		synctest.Wait()
		stop()
		running.Wait()

		c.Empty(outbox.readCompleted())
		c.Contains(logs.String(), "model down")
	})
}

func TestAnUnavailableOutboxIsLogged(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := require.New(t)
		outbox := newFakeOutbox()
		outbox.listErr = errOutboxDown
		var logs bytes.Buffer
		worker := NewWorker(outbox, TemplateExplainer{}, newQuietLogger(&logs), DefaultPoll)
		stop, running := startWorker(t, worker)

		synctest.Wait()
		stop()
		running.Wait()

		c.Contains(logs.String(), `"msg":"explainer: outbox unavailable"`)
	})
}

// slowExplainer takes a while, like the mock's 300–800 ms.
type slowExplainer struct{}

func (slowExplainer) Explain(ctx context.Context, decision Decision) (Explanation, error) {
	time.Sleep(500 * time.Millisecond)
	return TemplateExplainer{}.Explain(ctx, decision)
}

// AC7 of SP07 (D-033): stopping the worker lets the jobs in progress finish,
// and no new job starts.
func TestStoppingTheWorkerFinishesTheJobsInProgress(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := require.New(t)
		var pending []Pending
		for id := range int64(parallelism + 2) {
			pending = append(pending, newDecline(id+1))
		}

		outbox := newFakeOutbox(pending...)
		worker := NewWorker(outbox, slowExplainer{}, newQuietLogger(&bytes.Buffer{}), DefaultPoll)
		stop, running := startWorker(t, worker)

		time.Sleep(100 * time.Millisecond)
		stop()
		running.Wait()

		completed := outbox.readCompleted()
		c.Len(completed, parallelism, "the jobs in flight finished; the rest never started")
		for _, explanation := range completed {
			c.True(explanation.Validated)
		}
	})
}

// Explanations run in parallel: a batch of slow ones takes one explanation's time.
func TestABatchIsExplainedInParallel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := require.New(t)
		var pending []Pending
		for id := range int64(parallelism) {
			pending = append(pending, newDecline(id+1))
		}

		outbox := newFakeOutbox(pending...)
		worker := NewWorker(outbox, slowExplainer{}, newQuietLogger(&bytes.Buffer{}), time.Hour)
		start := time.Now()

		c.NoError(worker.drain(context.Background()))

		c.Equal(500*time.Millisecond, time.Since(start))
		c.Len(outbox.readCompleted(), parallelism)
	})
}

// I-034: under load the doorbell rings constantly; the worker still waits
// minGap between passes instead of querying Postgres back to back.
func TestTheWorkerWaitsBetweenPassesEvenWhenRungConstantly(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := require.New(t)
		outbox := &countingOutbox{fakeOutbox: newFakeOutbox()}
		worker := NewWorker(outbox, TemplateExplainer{}, newQuietLogger(&bytes.Buffer{}), time.Hour)
		stop, running := startWorker(t, worker)

		for range 100 {
			worker.Ring()
			time.Sleep(10 * time.Millisecond)
		}

		stop()
		running.Wait()

		c.LessOrEqual(outbox.passes.Load(), int64(time.Second/minGap)+1, "about one pass per gap over one second")
	})
}

// countingOutbox counts passes over the outbox.
type countingOutbox struct {
	*fakeOutbox
	passes atomic.Int64
}

func (o *countingOutbox) PublishUnexplained(ctx context.Context) (int64, error) {
	o.passes.Add(1)
	return o.fakeOutbox.PublishUnexplained(ctx)
}
