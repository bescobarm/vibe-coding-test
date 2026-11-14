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

// README §15: the metrics count explanations by how they were written.
func TestMetricsCountTheExplanations(t *testing.T) {
	c := require.New(t)
	harness := newFixtureWith(t, riskPolicy)
	since := time.Now().Add(-time.Minute)
	harness.authorize("by-model", 60000)
	harness.authorize("by-template", 61000)
	harness.authorize("crashed", 62000)
	explanations := map[string]string{
		"by-model":    `{"validated": true, "fallback_used": false}`,
		"by-template": `{"validated": false, "fallback_used": true}`,
		"crashed":     `{"failed": true}`,
	}
	for name, explanation := range explanations {
		_, err := harness.pool.Exec(harness.ctx, `UPDATE authorizations SET explanation = $2 WHERE authorization_id = $1`,
			harness.buildID(name), explanation)
		c.NoError(err)
	}

	metrics, err := harness.st.GetMetrics(harness.ctx, since)

	c.NoError(err)
	c.Equal(domain.ExplainerMetrics{Explained: 3, FallbackUsed: 1, Failed: 1}, metrics.Explainer)
}
