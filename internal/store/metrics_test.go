package store_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bescobarm/card-auth-engine/internal/domain"
)

func TestMetricsSummarizeTheAuditLog(t *testing.T) {
	c := require.New(t)
	harness := newFixtureWith(t, riskPolicy)
	since := time.Now().Add(-time.Minute)
	harness.authorize("ok1", 4500)
	harness.authorize("ok2", 4600)
	harness.authorize("too-big", 60000)

	metrics, err := harness.st.GetMetrics(harness.ctx, since)

	c.NoError(err)
	c.Equal(3, metrics.Decisions)
	c.Equal(2, metrics.Approved)
	c.Equal(1, metrics.Declined)
	c.InDelta(2.0/3, metrics.ApprovalRate, 0.001)
	c.Equal(map[string]int{domain.ReasonOverTransactionLimit: 1}, metrics.DeclineReasons)
	c.Positive(metrics.LatencyMS.P99)
	c.GreaterOrEqual(metrics.LatencyMS.P99, metrics.LatencyMS.P50)
}

func TestMetricsWithNoDecisionsAreZero(t *testing.T) {
	c := require.New(t)
	harness := newFixtureWith(t, budgetAndRisk)

	metrics, err := harness.st.GetMetrics(harness.ctx, time.Now().Add(time.Hour))

	c.NoError(err)
	c.Zero(metrics.Decisions)
	c.Zero(metrics.ApprovalRate)
	c.Empty(metrics.DeclineReasons)
	c.Zero(metrics.LatencyMS.P99)
}
