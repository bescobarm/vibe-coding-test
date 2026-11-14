package dashboard

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/bescobarm/card-auth-engine/internal/domain"
	"github.com/bescobarm/card-auth-engine/internal/lifecycle"
	"github.com/bescobarm/card-auth-engine/internal/simulator"
)

// recentSize is how many decisions the live feed keeps.
const recentSize = 50

// Decision is one line of the live feed.
type Decision struct {
	Time            time.Time       `json:"time"`
	Source          string          `json:"source"`
	AuthorizationID string          `json:"authorization_id"`
	CardID          string          `json:"card_id"`
	AmountMinor     int64           `json:"amount_minor"`
	Currency        string          `json:"currency"`
	MCC             string          `json:"mcc"`
	Country         string          `json:"country"`
	Decision        domain.Decision `json:"decision"`
	Flagged         bool            `json:"flagged"`
	RiskScore       int             `json:"risk_score"`
	Reasons         []string        `json:"reasons"`
	LatencyMS       float64         `json:"latency_ms"`
	FallbackUsed    bool            `json:"fallback_used"`
}

// Recent keeps the last decisions the dashboard sent, newest first.
type Recent struct {
	mu    sync.Mutex
	items []Decision
	size  int
}

// NewRecent returns a feed that keeps size decisions.
func NewRecent(size int) *Recent {
	return &Recent{size: size}
}

// Add puts a decision at the top, dropping the oldest when full.
func (r *Recent) Add(decision Decision) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.items = append([]Decision{decision}, r.items...)
	if len(r.items) > r.size {
		r.items = r.items[:r.size]
	}
}

// List returns a copy of the feed, newest first.
func (r *Recent) List() []Decision {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]Decision{}, r.items...)
}

type sourceKey struct{}

// withSource tags the decisions sent with ctx, e.g. "manual" or a scenario name.
func withSource(ctx context.Context, source string) context.Context {
	return context.WithValue(ctx, sourceKey{}, source)
}

func readSource(ctx context.Context) string {
	source, _ := ctx.Value(sourceKey{}).(string) // a missing tag reads as ""
	return source
}

// recordingEngine adds every decision to the feed on its way back, so manual
// transactions and scenario runs both show up live.
type recordingEngine struct {
	inner  simulator.Engine
	recent *Recent
}

func (e *recordingEngine) Authorize(ctx context.Context, request domain.AuthRequest) (domain.AuthResponse, error) {
	response, err := e.inner.Authorize(ctx, request)
	if err != nil {
		return response, err
	}

	codes := make([]string, len(response.Reasons))
	for index, reason := range response.Reasons {
		codes[index] = reason.Code
	}

	e.recent.Add(Decision{
		Time: time.Now().UTC(), Source: readSource(ctx), AuthorizationID: response.AuthorizationID,
		CardID: request.CardID, AmountMinor: request.AmountMinor, Currency: request.Currency,
		MCC: request.Merchant.MCC, Country: request.Merchant.Country, Decision: response.Decision,
		Flagged: response.Flagged, RiskScore: response.RiskScore, Reasons: codes,
		LatencyMS: response.LatencyMS, FallbackUsed: response.FallbackUsed,
	})

	return response, nil
}

func (e *recordingEngine) ApplyEvent(ctx context.Context, authorizationID, path string, body simulator.EventBody) (lifecycle.Result, error) {
	return e.inner.ApplyEvent(ctx, authorizationID, path, body)
}

func (e *recordingEngine) PublishPolicy(ctx context.Context, companyID string, policy json.RawMessage) error {
	return e.inner.PublishPolicy(ctx, companyID, policy)
}
