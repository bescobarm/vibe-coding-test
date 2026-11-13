package policy

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bescobarm/card-auth-engine/internal/domain"
)

func parseVelocity(t *testing.T, rules string) (Document, map[string]string) {
	t.Helper()
	d, err := Parse([]byte(`{"company_id": "acme-mx", "velocity": ` + rules + `}`))
	require.NoError(t, err)
	return d, d.Validate()
}

func TestValidateAcceptsTheREADMEVelocityRules(t *testing.T) {
	c := require.New(t)

	d, errs := parseVelocity(t, `[
		{"id": "card-testing", "scope": "card", "window": "10m", "max_count": 5, "filter": {"max_amount_minor": 500}},
		{"id": "daily-spend", "scope": "card", "window": "calendar_day", "max_amount": {"amount_minor": 200000, "currency": "USD"}}
	]`)

	c.Empty(errs)
	c.Equal(10*time.Minute, d.Velocity[0].Retention())
	c.Equal(24*time.Hour, d.Velocity[1].Retention())
}

func TestValidateRejectsAnInvalidRule(t *testing.T) {
	c := require.New(t)

	_, errs := parseVelocity(t, `[{"id": "bad id", "scope": "company", "window": "forever", "max_count": 0,
		"max_amount": {"amount_minor": 0, "currency": "usd"}, "filter": {"max_amount_minor": -1, "mccs": ["12"]}}]`)

	c.Equal(map[string]string{
		"velocity[0].id":                      msgRuleID,
		"velocity[0].scope":                   msgScope,
		"velocity[0].window":                  msgWindow,
		"velocity[0].max_count":               domain.MsgPositive,
		"velocity[0].max_amount.amount_minor": domain.MsgPositive,
		"velocity[0].max_amount.currency":     domain.MsgCurrency,
		"velocity[0].filter.max_amount_minor": msgNotNegative,
		"velocity[0].filter.mccs[0]":          domain.MsgMCC,
	}, errs)
}

func TestValidateRequiresALimit(t *testing.T) {
	c := require.New(t)

	_, errs := parseVelocity(t, `[{"id": "empty", "window": "1h"}]`)

	c.Equal(map[string]string{"velocity[0]": msgNeedsLimit}, errs)
}

func TestValidateRejectsDuplicateRuleIDs(t *testing.T) {
	c := require.New(t)

	_, errs := parseVelocity(t, `[{"id": "r", "window": "1h", "max_count": 1}, {"id": "r", "window": "2h", "max_count": 1}]`)

	c.Equal(map[string]string{"velocity[1].id": msgDuplicateID}, errs)
}

func TestValidateRejectsAWindowLongerThan31Days(t *testing.T) {
	c := require.New(t)

	_, errs := parseVelocity(t, `[{"id": "r", "window": "745h", "max_count": 1}]`)

	c.Equal(map[string]string{"velocity[0].window": msgWindow}, errs)
}

func TestRuleWithoutFilterAppliesToEveryRequest(t *testing.T) {
	c := require.New(t)

	c.True(VelocityRule{}.Applies(1_000_000, "7995"))
}

func TestFilterMatchesAmountAndMCC(t *testing.T) {
	c := require.New(t)
	r := VelocityRule{Filter: &VelocityFilter{MaxAmountMinor: 500, MCCs: []string{"5814"}}}

	c.True(r.Applies(500, "5814"))
	c.False(r.Applies(501, "5814"))
	c.False(r.Applies(100, "5734"))
}

func TestCalendarWindowsStartAtLocalMidnight(t *testing.T) {
	c := require.New(t)
	mexico, err := time.LoadLocation("America/Mexico_City")
	c.NoError(err)
	// 02:30 UTC on Nov 14 is still Nov 13 in Mexico City (UTC-6).
	now := time.Date(2026, 11, 14, 2, 30, 0, 0, time.UTC)

	day := VelocityRule{Window: CalendarDay}.WindowStart(now, mexico)
	month := VelocityRule{Window: CalendarMonth}.WindowStart(now, mexico)

	c.Equal(time.Date(2026, 11, 13, 0, 0, 0, 0, mexico), day)
	c.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, mexico), month)
	c.Equal(31*24*time.Hour, VelocityRule{Window: CalendarMonth}.Retention())
}

func TestRollingWindowStartsADurationAgo(t *testing.T) {
	c := require.New(t)
	d, errs := parseVelocity(t, `[{"id": "r", "window": "90m", "max_count": 1}]`)
	c.Empty(errs)
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)

	c.Equal(now.Add(-90*time.Minute), d.Velocity[0].WindowStart(now, time.UTC))
}
