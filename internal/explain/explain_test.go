package explain

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bescobarm/card-auth-engine/internal/domain"
)

var errModelDown = errors.New("model down")

func newSplitDecision() Decision {
	return Decision{
		AuthorizationID: "a1",
		Decision:        domain.Decline,
		RiskScore:       78,
		PolicyVersion:   3,
		Request: domain.AuthRequest{
			AmountMinor: 45000, Currency: "USD", EntryMode: "ecommerce",
			Merchant: domain.Merchant{Name: "ACME SOFTWARE", MCC: "5734", Country: "US"},
		},
		Reasons: []domain.Reason{
			{Code: domain.ReasonSplitTransaction, Message: "3 charges at this merchant today totaling 1,350.00 USD"},
			{Code: domain.ReasonNewMerchantCNP, Message: "First purchase at this merchant, card not present"},
		},
	}
}

// noWait replaces the mock's 300–800 ms sleep in unit tests.
func noWait(context.Context, time.Duration) error {
	return nil
}

func newInstantMock(failureRate float64) *MockLLM {
	mock := NewMockLLM(failureRate)
	mock.sleep = noWait
	return mock
}

func TestThePromptCarriesOnlyTheFacts(t *testing.T) {
	c := require.New(t)

	prompt := BuildPrompt(newSplitDecision())

	c.Contains(prompt, "Decision: DECLINE (risk score 78, policy v3)")
	c.Contains(prompt, "Merchant: ACME SOFTWARE (MCC 5734, US), card not present")
	c.Contains(prompt, "Amount: 450.00 USD")
	c.Contains(prompt, "- SPLIT_TRANSACTION_PATTERN: 3 charges at this merchant today totaling 1,350.00 USD")
	c.Contains(prompt, "Use ONLY the facts below.")
}

func TestAFlagIsDescribedAsAFlag(t *testing.T) {
	c := require.New(t)
	decision := newSplitDecision()
	decision.Decision, decision.Flagged = domain.Approve, true
	decision.Request.EntryMode = "chip"

	prompt := BuildPrompt(decision)

	c.Contains(prompt, "Decision: APPROVE + FLAG")
	c.Contains(prompt, "Merchant: ACME SOFTWARE (MCC 5734, US)\n", "a chip purchase has no card-not-present note")
}

func TestMoneyIsGroupedByThousands(t *testing.T) {
	c := require.New(t)

	c.Equal("1,350.00 USD", FormatMoney(135000, "USD"))
	c.Equal("1,234,567.89 MXN", FormatMoney(123456789, "MXN"))
	c.Equal("0.05 USD", FormatMoney(5, "USD"))
}

func TestOnlyDeclinesAndFlagsNeedAnExplanation(t *testing.T) {
	c := require.New(t)

	c.True(NeedsExplanation(domain.AuthResponse{Decision: domain.Decline}))
	c.True(NeedsExplanation(domain.AuthResponse{Decision: domain.Approve, Flagged: true}))
	c.False(NeedsExplanation(domain.AuthResponse{Decision: domain.Approve}))
}

func TestTheTemplateListsEveryReason(t *testing.T) {
	c := require.New(t)

	explanation, err := TemplateExplainer{}.Explain(context.Background(), newSplitDecision())

	c.NoError(err)
	c.Equal("template", explanation.Provider)
	c.False(explanation.Mocked)
	c.True(explanation.Validated)
	c.Equal("Declined: 3 charges at this merchant today totaling 1,350.00 USD (SPLIT_TRANSACTION_PATTERN); "+
		"First purchase at this merchant, card not present (NEW_MERCHANT_CNP).", explanation.Summary)
	c.Equal([]string{domain.ReasonSplitTransaction, domain.ReasonNewMerchantCNP}, explanation.ReasonCodes)
	c.Contains(explanation.SuggestedAction, "request a limit exception")
}

func TestAnUnknownReasonGetsTheDefaultAction(t *testing.T) {
	c := require.New(t)
	decision := newSplitDecision()
	decision.Reasons = []domain.Reason{{Code: domain.ReasonRoundAmount, Message: "Round amount 20000"}}

	explanation, err := TemplateExplainer{}.Explain(context.Background(), decision)

	c.NoError(err)
	c.Equal(actionDefault, explanation.SuggestedAction)
}

// AC2 of SP07 and README §13: a mocked, validated explanation built from the facts.
func TestTheMockExplainsFromTheFacts(t *testing.T) {
	c := require.New(t)

	explanation, err := NewLLMExplainer(newInstantMock(0)).Explain(context.Background(), newSplitDecision())

	c.NoError(err)
	c.Equal("mock-llm", explanation.Provider)
	c.Equal("mock-llm-v0", explanation.Model)
	c.True(explanation.Mocked)
	c.True(explanation.Validated)
	c.False(explanation.FallbackUsed)
	c.Equal("Declined at ACME SOFTWARE: 3 charges at this merchant today totaling 1,350.00 USD. "+
		"Also: first purchase at this merchant, card not present.", explanation.Summary)
	c.Equal([]string{domain.ReasonSplitTransaction, domain.ReasonNewMerchantCNP}, explanation.ReasonCodes)
}

// AC4 of SP07: the same decision gets the same explanation.
func TestTheMockIsDeterministic(t *testing.T) {
	c := require.New(t)
	mock := newInstantMock(0)

	first, err := mock.Complete(context.Background(), BuildPrompt(newSplitDecision()))
	c.NoError(err)
	second, err := mock.Complete(context.Background(), BuildPrompt(newSplitDecision()))
	c.NoError(err)

	c.Equal(first, second)
}

func TestTheMockTakesBetween300And800Milliseconds(t *testing.T) {
	c := require.New(t)
	var waited time.Duration
	mock := NewMockLLM(0)
	mock.sleep = func(_ context.Context, wait time.Duration) error {
		waited = wait
		return nil
	}

	_, err := mock.Complete(context.Background(), BuildPrompt(newSplitDecision()))

	c.NoError(err)
	c.GreaterOrEqual(waited, 300*time.Millisecond)
	c.Less(waited, 800*time.Millisecond)
}

// AC3 of SP07: a model that invents a reason is rejected, and the template answers.
func TestAnInventedReasonFallsBackToTheTemplate(t *testing.T) {
	c := require.New(t)

	explanation, err := NewLLMExplainer(newInstantMock(1)).Explain(context.Background(), newSplitDecision())

	c.NoError(err)
	c.False(explanation.Validated)
	c.True(explanation.FallbackUsed)
	c.True(explanation.Mocked)
	c.True(strings.HasPrefix(explanation.Summary, "Declined: 3 charges"), "template text")
}

func TestAnInventedReasonIsCaughtEvenInTheText(t *testing.T) {
	c := require.New(t)
	reply := Reply{Summary: "Declined. This looks like VELOCITY_EXCEEDED.", ReasonCodes: []string{domain.ReasonSplitTransaction}}

	c.False(isValid(reply, newSplitDecision().Reasons))
}

func TestAnEmptyReplyIsNotValid(t *testing.T) {
	c := require.New(t)

	c.False(isValid(Reply{}, newSplitDecision().Reasons))
}

func TestAModelErrorFallsBackToTheTemplate(t *testing.T) {
	c := require.New(t)
	mock := NewMockLLM(0)
	mock.sleep = func(context.Context, time.Duration) error {
		return errModelDown
	}

	explanation, err := NewLLMExplainer(mock).Explain(context.Background(), newSplitDecision())

	c.NoError(err)
	c.True(explanation.FallbackUsed)
}

func TestAPromptWithoutFactsIsAnError(t *testing.T) {
	c := require.New(t)

	_, err := newInstantMock(0).Complete(context.Background(), "Decision: DECLINE\n")

	c.ErrorIs(err, ErrNoFacts)
}

func TestTheMockWaitStopsWithItsContext(t *testing.T) {
	c := require.New(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := NewMockLLM(0).Complete(ctx, BuildPrompt(newSplitDecision()))

	c.ErrorIs(err, context.Canceled)
}

func TestTheMockWaitEndsAfterItsDuration(t *testing.T) {
	c := require.New(t)

	c.NoError(sleepContext(context.Background(), time.Millisecond))
}

func TestTheMockInventsACodeThatWasNotGiven(t *testing.T) {
	c := require.New(t)

	c.Equal(domain.ReasonVelocityExceeded, inventCode([]string{domain.ReasonSplitTransaction}))
	c.Equal(domain.ReasonRoundAmount, inventCode([]string{
		domain.ReasonVelocityExceeded,
		domain.ReasonAmountAnomaly,
		domain.ReasonNewCountry,
	}))
}

func TestHelpersHandleEmptyInput(t *testing.T) {
	c := require.New(t)

	c.Empty(lowerFirst(""))
	c.Empty(readMerchant("no merchant line"))
}
