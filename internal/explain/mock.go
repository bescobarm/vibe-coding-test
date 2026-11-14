package explain

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/bescobarm/card-auth-engine/internal/domain"
)

const (
	providerMock     = "mock-llm"
	modelMock        = "mock-llm-v0"
	mockMinLatency   = 300 * time.Millisecond
	mockLatencySpan  = 500 // ms on top of the minimum: 300–800 ms
	failureBuckets   = 1000
	millisPerSecond  = 1000
	factPrefix       = "- "
	factSeparator    = ": "
	merchantPrefix   = "Merchant: "
	merchantNameStop = " (MCC"
	fmtMockSummary   = "%s at %s: %s."
	fmtMockAlso      = " Also: %s."
	fmtMockInvented  = " This also matches %s."
)

// ErrNoFacts means the prompt had no facts to explain.
var ErrNoFacts = errors.New("prompt has no facts")

// codeRe finds anything that looks like a reason code in model output.
var codeRe = regexp.MustCompile(`\b[A-Z]+(?:_[A-Z]+)+\b`)

// Reply is what a model returns: a summary, an action, and the reason codes it used.
type Reply struct {
	Summary         string
	SuggestedAction string
	ReasonCodes     []string
}

// Model completes a prompt. A real provider adapter would implement it with an HTTP call.
type Model interface {
	Complete(ctx context.Context, prompt string) (Reply, error)
	Name() (provider, model string)
}

// MockLLM is a local, deterministic stand-in for a model (D-013). The same
// prompt always gets the same text and latency (300–800 ms). With
// FailureRate it sometimes mentions a reason code it was not given, which the
// validator must catch.
type MockLLM struct {
	FailureRate float64
	sleep       func(ctx context.Context, d time.Duration) error
}

// NewMockLLM returns the mock with the given failure rate (0 to 1).
func NewMockLLM(failureRate float64) *MockLLM {
	return &MockLLM{FailureRate: failureRate, sleep: sleepContext}
}

// Name implements Model.
func (m *MockLLM) Name() (string, string) {
	return providerMock, modelMock
}

// Complete implements Model: it reads the facts from the prompt and writes from them.
func (m *MockLLM) Complete(ctx context.Context, prompt string) (Reply, error) {
	hash := hashPrompt(prompt)

	err := m.sleep(ctx, mockMinLatency+time.Duration(hash%mockLatencySpan)*time.Millisecond)
	if err != nil {
		return Reply{}, err
	}

	codes, messages := readFacts(prompt)
	if len(codes) == 0 {
		return Reply{}, ErrNoFacts
	}

	verb := verbFlagged
	if strings.Contains(prompt, "Decision: "+string(domain.Decline)) {
		verb = verbDeclined
	}

	summary := fmt.Sprintf(fmtMockSummary, verb, readMerchant(prompt), lowerFirst(messages[0]))
	for _, message := range messages[1:] {
		summary += fmt.Sprintf(fmtMockAlso, lowerFirst(message))
	}

	reply := Reply{Summary: summary, SuggestedAction: suggestActionForCodes(codes), ReasonCodes: codes}

	if float64(hash%failureBuckets) < m.FailureRate*failureBuckets {
		invented := inventCode(codes)
		reply.Summary += fmt.Sprintf(fmtMockInvented, invented)
		reply.ReasonCodes = append(slices.Clone(codes), invented)
	}

	return reply, nil
}

// LLMExplainer asks a model, validates the answer against the decision, and
// falls back to the template when the model fails or invents (SP07 R3).
type LLMExplainer struct {
	model    Model
	fallback TemplateExplainer
	now      func() time.Time
}

// NewLLMExplainer returns an explainer backed by model.
func NewLLMExplainer(model Model) *LLMExplainer {
	return &LLMExplainer{model: model, now: time.Now}
}

// Explain implements Explainer.
func (l *LLMExplainer) Explain(ctx context.Context, decision Decision) (Explanation, error) {
	provider, model := l.model.Name()
	start := l.now()

	reply, err := l.model.Complete(ctx, BuildPrompt(decision))
	latency := float64(l.now().Sub(start).Microseconds()) / millisPerSecond

	if err == nil && isValid(reply, decision.Reasons) {
		return Explanation{
			Provider: provider, Model: model, Mocked: provider == providerMock, LatencyMS: latency,
			Summary: reply.Summary, SuggestedAction: reply.SuggestedAction, ReasonCodes: reply.ReasonCodes,
			Validated: true,
		}, nil
	}

	explanation, fallbackErr := l.fallback.Explain(ctx, decision)
	explanation.Provider, explanation.Model, explanation.Mocked = provider, model, provider == providerMock
	explanation.LatencyMS, explanation.Validated, explanation.FallbackUsed = latency, false, true

	return explanation, fallbackErr
}

// isValid accepts a reply only if every reason code it uses or mentions was
// given: an explanation must never invent a reason (SP07 R3).
func isValid(reply Reply, reasons []domain.Reason) bool {
	given := listCodes(reasons)
	mentioned := append(slices.Clone(reply.ReasonCodes), codeRe.FindAllString(reply.Summary+" "+reply.SuggestedAction, -1)...)

	for _, code := range mentioned {
		if !slices.Contains(given, code) {
			return false
		}
	}

	return reply.Summary != ""
}

func readFacts(prompt string) ([]string, []string) {
	var codes, messages []string

	for _, line := range strings.Split(prompt, "\n") {
		fact, ok := strings.CutPrefix(line, factPrefix)
		if !ok {
			continue
		}

		code, message, found := strings.Cut(fact, factSeparator)
		if found {
			codes = append(codes, code)
			messages = append(messages, message)
		}
	}

	return codes, messages
}

func readMerchant(prompt string) string {
	for _, line := range strings.Split(prompt, "\n") {
		rest, ok := strings.CutPrefix(line, merchantPrefix)
		if ok {
			name, _, _ := strings.Cut(rest, merchantNameStop)
			return name
		}
	}

	return ""
}

// inventCode picks a real reason code that was not given, to simulate a model that invents one.
func inventCode(given []string) string {
	for _, code := range []string{
		domain.ReasonVelocityExceeded,
		domain.ReasonAmountAnomaly,
		domain.ReasonNewCountry,
	} {
		if !slices.Contains(given, code) {
			return code
		}
	}

	return domain.ReasonRoundAmount
}

func suggestActionForCodes(codes []string) string {
	reasons := make([]domain.Reason, len(codes))
	for index, code := range codes {
		reasons[index] = domain.Reason{Code: code}
	}

	return suggestAction(reasons)
}

func hashPrompt(prompt string) uint64 {
	hasher := fnv.New64a()
	_, err := hasher.Write([]byte(prompt))
	if err != nil {
		return 0
	}

	return hasher.Sum64()
}

func lowerFirst(text string) string {
	if text == "" {
		return text
	}

	return strings.ToLower(text[:1]) + text[1:]
}

func sleepContext(ctx context.Context, wait time.Duration) error {
	timer := time.NewTimer(wait)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
