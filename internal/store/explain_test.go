package store_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bescobarm/card-auth-engine/internal/domain"
	"github.com/bescobarm/card-auth-engine/internal/explain"
	"github.com/bescobarm/card-auth-engine/internal/lifecycle"
)

func (f fixture) readExplanation(name string) *explain.Explanation {
	f.t.Helper()
	record, err := f.st.GetAuthorization(f.ctx, f.buildID(name))
	require.NoError(f.t, err)

	if len(record.Explanation) == 0 {
		return nil
	}

	var explanation explain.Explanation
	require.NoError(f.t, json.Unmarshal(record.Explanation, &explanation))
	return &explanation
}

// AC2 of SP07 end to end: a stored decline is explained by the worker, and
// its outbox event is published; an approval is published without one.
func TestTheWorkerExplainsAStoredDecline(t *testing.T) {
	c := require.New(t)
	harness := newFixtureWith(t, riskPolicy)
	c.Equal(domain.Decline, harness.authorize("too-big", 60000).Decision)
	c.Equal(domain.Approve, harness.authorize("fine", 4500).Decision)
	worker := explain.NewWorker(harness.st, explain.TemplateExplainer{}, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), time.Hour)
	ctx, stop := context.WithCancel(harness.ctx)

	var running sync.WaitGroup

	running.Go(func() {
		worker.Run(ctx)
	})

	c.Eventually(func() bool {
		return harness.readExplanation("too-big") != nil
	}, 5*time.Second, 20*time.Millisecond)
	stop()
	running.Wait()

	explanation := harness.readExplanation("too-big")
	c.True(explanation.Validated)
	c.Equal([]string{domain.ReasonOverTransactionLimit}, explanation.ReasonCodes)
	c.Contains(explanation.Summary, "Amount 60000 is over the per-transaction limit")
	c.Nil(harness.readExplanation("fine"))

	pending, err := harness.st.ListPendingDecisions(harness.ctx, 100)
	c.NoError(err)
	c.Empty(pending, "both events are published")
}

// The outbox delivers at least once: a second delivery never overwrites.
func TestAnExplanationIsNeverOverwritten(t *testing.T) {
	c := require.New(t)
	harness := newFixtureWith(t, riskPolicy)
	harness.authorize("too-big", 60000)
	pending, err := harness.st.ListPendingDecisions(harness.ctx, 100)
	c.NoError(err)
	c.Len(pending, 1)
	c.Equal(int64(60000), pending[0].Request.AmountMinor)
	first := &explain.Explanation{Summary: "first"}

	c.NoError(harness.st.CompleteDecision(harness.ctx, pending[0].OutboxID, harness.buildID("too-big"), first))
	c.NoError(harness.st.CompleteDecision(harness.ctx, pending[0].OutboxID, harness.buildID("too-big"), &explain.Explanation{Summary: "second"}))

	c.Equal("first", harness.readExplanation("too-big").Summary)
}

// Approvals are published in bulk; lifecycle events are another topic and
// are left alone.
func TestApprovalsArePublishedInBulkAndLifecycleEventsAreLeftAlone(t *testing.T) {
	c := require.New(t)
	harness := newFixture(t, budget500)
	harness.authorize("a1", 300)
	harness.authorize("a2", 100)

	published, err := harness.st.PublishUnexplained(harness.ctx)
	c.NoError(err)
	harness.applyEvent("a1", "e1", lifecycle.TypeExpiry, nil)
	again, err := harness.st.PublishUnexplained(harness.ctx)
	c.NoError(err)

	c.Equal(int64(2), published)
	c.Zero(again, "the lifecycle event is not a decision")
	pending, err := harness.st.ListPendingDecisions(harness.ctx, 100)
	c.NoError(err)
	c.Empty(pending)
}
