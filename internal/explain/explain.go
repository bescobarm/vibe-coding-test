// Package explain turns a decline or a flag into plain language for a
// finance team, after the decision and never inside it (D-009). The LLM is
// mocked (D-013): the flow is real (prompt, model, validation, fallback), the
// model is a local deterministic function.
package explain

import (
	"context"
	"fmt"
	"strings"

	"github.com/bescobarm/card-auth-engine/internal/domain"
)

// Decision is what an explanation is built from: the answer and its request.
type Decision struct {
	AuthorizationID string
	Decision        domain.Decision
	Flagged         bool
	RiskScore       int
	PolicyVersion   int
	Request         domain.AuthRequest
	Reasons         []domain.Reason
}

// Explanation is stored on the authorization and returned by
// GET /v1/authorizations/{id}. Mocked marks text no real model wrote.
type Explanation struct {
	Provider        string   `json:"provider"`
	Model           string   `json:"model"`
	Mocked          bool     `json:"mocked"`
	LatencyMS       float64  `json:"latency_ms"`
	Summary         string   `json:"summary"`
	SuggestedAction string   `json:"suggested_action"`
	ReasonCodes     []string `json:"reason_codes"`
	Validated       bool     `json:"validated"`
	FallbackUsed    bool     `json:"fallback_used"`
	Failed          bool     `json:"failed,omitempty"` // the explainer crashed; no text could be produced (D-032)
}

// Explainer writes the explanation of one decision.
type Explainer interface {
	Explain(ctx context.Context, decision Decision) (Explanation, error)
}

const (
	providerTemplate = "template"
	modelTemplate    = "template-v1"
	verbDeclined     = "Declined"
	verbFlagged      = "Flagged for review"
	actionDefault    = "Review the transaction with the cardholder."
	actionConfirm    = "Confirm with the cardholder that they made this purchase before retrying it."
	actionRaiseLimit = "If the spend is expected, ask a policy admin to raise the limit."
	notPresentNote   = ", card not present"
)

// actions is the suggested action for each reason, the same for the mock and the template.
var actions = map[string]string{
	domain.ReasonSplitTransaction:     "Ask the cardholder whether these charges are one purchase. If they are, request a limit exception instead of splitting it.",
	domain.ReasonCardTestingPattern:   "Freeze the card and confirm the recent small charges with the cardholder: repeated small charges often test a stolen card number.",
	domain.ReasonNewCountry:           actionConfirm,
	domain.ReasonNewMerchantCNP:       actionConfirm,
	domain.ReasonAmountAnomaly:        actionConfirm,
	domain.ReasonMCCBlocked:           "If the purchase is legitimate, ask a policy admin to review the blocked category.",
	domain.ReasonOverTransactionLimit: actionRaiseLimit,
	domain.ReasonOverDailyLimit:       actionRaiseLimit,
	domain.ReasonOverMonthlyLimit:     actionRaiseLimit,
	domain.ReasonFallbackApplied:      "Retry later: the engine couldn't run all its checks for this purchase.",
}

// NeedsExplanation reports whether a decision is worth explaining: declines and flags.
func NeedsExplanation(response domain.AuthResponse) bool {
	return response.Decision == domain.Decline || response.Flagged
}

// TemplateExplainer writes a fixed sentence from the reasons. It is the
// fallback when the model fails, and EXPLAINER=template uses it alone.
type TemplateExplainer struct{}

// Explain implements Explainer.
func (TemplateExplainer) Explain(_ context.Context, decision Decision) (Explanation, error) {
	messages := make([]string, len(decision.Reasons))
	for index, reason := range decision.Reasons {
		messages[index] = fmt.Sprintf("%s (%s)", reason.Message, reason.Code)
	}

	return Explanation{
		Provider:        providerTemplate,
		Model:           modelTemplate,
		Summary:         fmt.Sprintf("%s: %s.", describeVerb(decision), strings.Join(messages, "; ")),
		SuggestedAction: suggestAction(decision.Reasons),
		ReasonCodes:     listCodes(decision.Reasons),
		Validated:       true,
	}, nil
}

// BuildPrompt is what a real model would receive. The mock reads its facts;
// a real adapter would send it as is.
func BuildPrompt(decision Decision) string {
	var prompt strings.Builder

	request := decision.Request
	merchant := request.Merchant
	presence := ""

	if isCardNotPresent(request.EntryMode) {
		presence = notPresentNote
	}

	prompt.WriteString("You explain card authorization decisions to a company's finance team.\n")
	prompt.WriteString("Use ONLY the facts below. Do not invent amounts, merchants or reasons.\n\n")
	fmt.Fprintf(&prompt, "Decision: %s (risk score %d, policy v%d)\n", describeDecision(decision), decision.RiskScore, decision.PolicyVersion)
	fmt.Fprintf(&prompt, "Merchant: %s (MCC %s, %s)%s\n", merchant.Name, merchant.MCC, merchant.Country, presence)
	fmt.Fprintf(&prompt, "Amount: %s\n", domain.FormatMoney(request.AmountMinor, request.Currency))
	prompt.WriteString("Facts:\n")

	for _, reason := range decision.Reasons {
		fmt.Fprintf(&prompt, "- %s: %s\n", reason.Code, reason.Message)
	}

	prompt.WriteString("\nWrite 2 sentences of summary and 1 suggested action.\n")

	return prompt.String()
}

func describeDecision(decision Decision) string {
	if decision.Decision == domain.Decline {
		return string(domain.Decline)
	}

	return "APPROVE + FLAG"
}

func describeVerb(decision Decision) string {
	if decision.Decision == domain.Decline {
		return verbDeclined
	}

	return verbFlagged
}

func suggestAction(reasons []domain.Reason) string {
	for _, reason := range reasons {
		action, ok := actions[reason.Code]
		if ok {
			return action
		}
	}

	return actionDefault
}

func listCodes(reasons []domain.Reason) []string {
	codes := make([]string, len(reasons))
	for index, reason := range reasons {
		codes[index] = reason.Code
	}

	return codes
}

func isCardNotPresent(entryMode string) bool {
	return entryMode == "ecommerce" || entryMode == "manual"
}
