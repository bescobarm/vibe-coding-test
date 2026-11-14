package velocity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bescobarm/card-auth-engine/internal/domain"
	"github.com/bescobarm/card-auth-engine/internal/pipeline"
	"github.com/bescobarm/card-auth-engine/internal/policy"
)

var errRedisDown = errors.New("redis down")

type fakeReserver struct {
	result    Result
	err       error
	got       []Reservation
	cancelled []string
}

func (f *fakeReserver) Reserve(_ context.Context, r Reservation) (Result, error) {
	f.got = append(f.got, r)
	return f.result, f.err
}

func (f *fakeReserver) Cancel(_ context.Context, cardID, authorizationID string) error {
	f.cancelled = append(f.cancelled, cardID+"/"+authorizationID)
	return f.err
}

// declineStage stands for a later stage, like the score, that declines.
type declineStage struct{}

func (declineStage) Name() string { return "decline" }

func (declineStage) Evaluate(_ context.Context, e *pipeline.Evaluation) error {
	e.Decline("TEST", "declined later")
	return nil
}

// evaluation runs a policy with the given rules through Validate, so rolling
// windows are resolved like a published policy.
func evaluation(t *testing.T, rules string) *pipeline.Evaluation {
	t.Helper()
	doc, err := policy.Parse([]byte(`{"company_id": "acme-mx", "timezone": "America/Mexico_City", "velocity": ` + rules + `}`))
	require.NoError(t, err)
	require.Empty(t, doc.Validate())

	e := pipeline.NewEvaluation(domain.AuthRequest{
		AuthorizationID: "a1", CardID: "card_123", AmountMinor: 4500, Currency: "USD",
		Merchant:  domain.Merchant{MCC: "5734"},
		CreatedAt: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	e.Policy = doc
	e.ReceivedAt = t0
	return e
}

const (
	countRule = `{"id": "card-testing", "window": "10m", "max_count": 5}`
	dailyRule = `{"id": "daily-spend", "window": "calendar_day", "max_amount": {"amount_minor": 200000, "currency": "USD"}}`
)

func TestStageSkipsRedisWhenNoRuleApplies(t *testing.T) {
	c := require.New(t)
	f := &fakeReserver{}
	e := evaluation(t, `[{"id": "small", "window": "10m", "max_count": 5, "filter": {"max_amount_minor": 500}}]`)

	err := NewStage(f).Evaluate(context.Background(), e)

	c.NoError(err)
	c.Empty(f.got)
	c.False(e.Halted())
}

func TestStageReservesWithTheEngineClockAndPolicyTimezone(t *testing.T) {
	c := require.New(t)
	f := &fakeReserver{result: Result{Reserved: true}}
	e := evaluation(t, `[`+countRule+`, `+dailyRule+`]`)

	err := NewStage(f).Evaluate(context.Background(), e)

	c.NoError(err)
	c.False(e.Halted())
	c.Len(f.got, 1)

	mexico, _ := time.LoadLocation("America/Mexico_City")
	c.Equal(Reservation{
		CardID: "card_123", AuthorizationID: "a1", AmountMinor: 4500, Now: t0,
		Windows: []Window{
			{RuleID: "card-testing", Start: t0.Add(-10 * time.Minute), MaxCount: 5, MaxAmount: NoLimit, Retention: 10 * time.Minute},
			{RuleID: "daily-spend", Start: time.Date(2026, 11, 13, 0, 0, 0, 0, mexico), MaxCount: NoLimit, MaxAmount: 200000, Retention: 24 * time.Hour},
		},
	}, f.got[0])
}

func TestStageDeclinesAnotherCurrencyWithoutCallingRedis(t *testing.T) {
	c := require.New(t)
	f := &fakeReserver{}
	e := evaluation(t, `[`+dailyRule+`]`)
	e.Request.Currency = "MXN"

	err := NewStage(f).Evaluate(context.Background(), e)

	c.NoError(err)
	c.Empty(f.got)
	c.Equal([]domain.Reason{{Code: domain.ReasonOverDailyLimit, Message: "Rule daily-spend limit is in USD, request is in MXN"}}, e.Reasons())
}

func TestStageDeclinesAFullCountWindow(t *testing.T) {
	c := require.New(t)
	f := &fakeReserver{result: Result{Window: 0, Count: 5, SpentMinor: 2000}}
	e := evaluation(t, `[`+countRule+`]`)

	err := NewStage(f).Evaluate(context.Background(), e)

	c.NoError(err)
	c.Equal([]domain.Reason{{Code: domain.ReasonVelocityExceeded, Message: "Rule card-testing allows 5 authorizations per 10m; 5 already approved"}}, e.Reasons())
}

func TestStageDeclinesAFullDailyBudget(t *testing.T) {
	c := require.New(t)
	f := &fakeReserver{result: Result{Window: 1, Count: 3, SpentMinor: 198000}}
	e := evaluation(t, `[`+countRule+`, `+dailyRule+`]`)

	err := NewStage(f).Evaluate(context.Background(), e)

	c.NoError(err)
	c.Equal([]domain.Reason{{Code: domain.ReasonOverDailyLimit,
		Message: "Rule daily-spend: 198000 already spent plus 4500 is over the limit of 200000 USD per calendar_day"}}, e.Reasons())
}

func TestStageDeclinesAFullMonthlyBudget(t *testing.T) {
	c := require.New(t)
	f := &fakeReserver{result: Result{Window: 0, Count: 1, SpentMinor: 990000}}
	e := evaluation(t, `[{"id": "monthly", "window": "calendar_month", "max_count": 100, "max_amount": {"amount_minor": 1000000, "currency": "USD"}}]`)

	err := NewStage(f).Evaluate(context.Background(), e)

	c.NoError(err)
	c.Equal(domain.ReasonOverMonthlyLimit, e.Reasons()[0].Code)
}

func TestStageUsesVelocityExceededForARollingAmountWindow(t *testing.T) {
	c := require.New(t)
	f := &fakeReserver{result: Result{Window: 0, Count: 1, SpentMinor: 9000}}
	e := evaluation(t, `[{"id": "hourly", "window": "1h", "max_amount": {"amount_minor": 10000, "currency": "USD"}}]`)

	err := NewStage(f).Evaluate(context.Background(), e)

	c.NoError(err)
	c.Equal(domain.ReasonVelocityExceeded, e.Reasons()[0].Code)
}

func TestStageReturnsTheRedisErrorInsteadOfDeciding(t *testing.T) {
	c := require.New(t)
	f := &fakeReserver{err: errRedisDown}
	e := evaluation(t, `[`+countRule+`]`)

	err := NewStage(f).Evaluate(context.Background(), e)

	c.ErrorIs(err, errRedisDown)
	c.False(e.Halted())
}

func TestStageName(t *testing.T) {
	c := require.New(t)

	c.Equal("velocity", NewStage(nil).Name())
}

// D-023: a reservation is cancelled when a later stage declines.
func TestAReservationIsCancelledWhenALaterStageDeclines(t *testing.T) {
	c := require.New(t)
	f := &fakeReserver{result: Result{Reserved: true}}
	e := evaluation(t, `[`+countRule+`]`)

	err := pipeline.New(NewStage(f), declineStage{}).Run(context.Background(), e)

	c.NoError(err)
	c.Equal([]string{"card_123/a1"}, f.cancelled)
}

func TestAnApprovedReservationIsKept(t *testing.T) {
	c := require.New(t)
	f := &fakeReserver{result: Result{Reserved: true}}
	e := evaluation(t, `[`+countRule+`]`)

	err := pipeline.New(NewStage(f)).Run(context.Background(), e)

	c.NoError(err)
	c.Empty(f.cancelled)
}

// D-025, D-060: the card-testing rule reports CARD_TESTING_PATTERN.
func TestACountRuleCanNameItsReason(t *testing.T) {
	c := require.New(t)
	f := &fakeReserver{result: Result{Window: 0, Count: 5}}
	e := evaluation(t, `[{"id": "card-testing", "window": "10m", "max_count": 5, "reason": "CARD_TESTING_PATTERN"}]`)

	err := NewStage(f).Evaluate(context.Background(), e)

	c.NoError(err)
	c.Equal(domain.ReasonCardTestingPattern, e.Reasons()[0].Code)
}
