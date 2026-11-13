// Package lifecycle applies what happens to an approved authorization after
// the decision (FR6, D-012): reversal and expiry release the hold, an
// increment raises it, a capture settles it and a refund gives budget back.
// Every call carries an event_id, so retries are safe (D-050).
package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bescobarm/card-auth-engine/internal/domain"
	"github.com/bescobarm/card-auth-engine/internal/policy"
)

// Event types.
const (
	TypeReversal  = "reversal"
	TypeExpiry    = "expiry"
	TypeIncrement = "increment"
	TypeCapture   = "capture"
	TypeRefund    = "refund"
)

// Authorization statuses after the decision.
const (
	StatusPending  = "pending"
	StatusDeclined = "declined"
	StatusReversed = "reversed"
	StatusExpired  = "expired"
	StatusCaptured = "captured"
	StatusRefunded = "refunded"
)

// Errors a caller can act on. Each message is shown to the API client.
var (
	ErrDeclined      = errors.New("authorization was declined, nothing is held")
	ErrNotPending    = errors.New("authorization is no longer pending")
	ErrNotCaptured   = errors.New("authorization is not captured, nothing to refund")
	ErrOverHold      = errors.New("amount is larger than what is still held")
	ErrOverRefund    = errors.New("amount is larger than what can still be refunded")
	ErrEventConflict = errors.New("event_id was already used for a different event")
	ErrUnknownType   = errors.New("unknown lifecycle event type")
)

// Event is one lifecycle call. AmountMinor is nil for "all of it" (reversal, refund).
type Event struct {
	ID              string
	AuthorizationID string
	Type            string
	AmountMinor     *int64
}

// Same reports whether two events are the same call, so a stored one can be replayed.
func (ev Event) Same(other Event) bool {
	sameAmount := ev.AmountMinor == nil && other.AmountMinor == nil ||
		ev.AmountMinor != nil && other.AmountMinor != nil && *ev.AmountMinor == *other.AmountMinor

	return ev.ID == other.ID && ev.AuthorizationID == other.AuthorizationID && ev.Type == other.Type && sameAmount
}

func (ev Event) amountOr(all int64) int64 {
	if ev.AmountMinor == nil {
		return all
	}

	return *ev.AmountMinor
}

// Hold is the current state of an authorization, read under a row lock.
type Hold struct {
	AuthorizationID string
	CardID          string
	Currency        string
	MCC             string
	Status          string
	HeldMinor       int64
	CapturedMinor   int64
	RefundedMinor   int64
}

// Result is the answer to a lifecycle call. It is stored with the event and
// returned unchanged when the same event_id is sent again. Decision and
// Reasons are only set for an increment, the one event that can be declined.
type Result struct {
	AuthorizationID string          `json:"authorization_id"`
	EventID         string          `json:"event_id"`
	Type            string          `json:"type"`
	Decision        domain.Decision `json:"decision,omitempty"`
	Reasons         []domain.Reason `json:"reasons,omitempty"`
	Status          string          `json:"status"`
	HeldMinor       int64           `json:"held_minor"`
	CapturedMinor   int64           `json:"captured_minor"`
	RefundedMinor   int64           `json:"refunded_minor"`
}

// ApplyFunc changes one locked authorization and returns the result to store.
type ApplyFunc func(ctx context.Context, h Hold) (Result, error)

// Store locks the authorization, replays a known event_id, or runs apply and
// stores the event and the new hold in the same transaction.
type Store interface {
	ApplyEvent(ctx context.Context, ev Event, apply ApplyFunc) (res Result, replayed bool, err error)
}

// Adjust changes a card's hold by a signed amount, without limits.
type Adjust struct {
	CardID          string
	AuthorizationID string
	EventID         string
	DeltaMinor      int64
}

// Increase asks to raise a hold within the company's amount limits.
type Increase struct {
	CardID          string
	AuthorizationID string
	EventID         string
	AmountMinor     int64 // the increase
	TotalMinor      int64 // the hold after it, for rule filters
	Currency        string
	MCC             string
	Now             time.Time
	Policy          policy.Document
}

// Limit is the outcome of an Increase: Code and Message say which limit was hit.
type Limit struct {
	Exceeded bool
	Code     string
	Message  string
}

// Holds changes the budgets in Redis.
type Holds interface {
	Adjust(ctx context.Context, a Adjust) error
	Increase(ctx context.Context, inc Increase) (Limit, error)
}

// Transition computes the new state and the signed change to the budgets.
// An increment's limits are checked afterwards by the Service.
func Transition(h Hold, ev Event) (Result, int64, error) {
	steps := map[string]func(Hold, Event) (Result, int64, error){
		TypeReversal:  release,
		TypeExpiry:    release,
		TypeIncrement: increment,
		TypeCapture:   capture,
		TypeRefund:    refund,
	}

	step, ok := steps[ev.Type]
	if !ok {
		return Result{}, 0, fmt.Errorf("%w: %q", ErrUnknownType, ev.Type)
	}

	res, delta, err := step(h, ev)
	if err != nil {
		return Result{}, 0, err
	}

	res.AuthorizationID, res.EventID, res.Type = h.AuthorizationID, ev.ID, ev.Type
	return res, delta, nil
}

func requirePending(h Hold) error {
	switch h.Status {
	case StatusPending:
		return nil
	case StatusDeclined:
		return ErrDeclined
	default:
		return ErrNotPending
	}
}

// release: a partial reversal keeps the authorization pending.
func release(h Hold, ev Event) (Result, int64, error) {
	err := requirePending(h)
	if err != nil {
		return Result{}, 0, err
	}

	amount := h.HeldMinor
	if ev.Type == TypeReversal {
		amount = ev.amountOr(h.HeldMinor)
	}

	if amount > h.HeldMinor {
		return Result{}, 0, ErrOverHold
	}

	left := h.HeldMinor - amount
	status := StatusPending

	switch {
	case ev.Type == TypeExpiry:
		status = StatusExpired
	case left == 0:
		status = StatusReversed
	}

	return Result{Status: status, HeldMinor: left}, -amount, nil
}

func increment(h Hold, ev Event) (Result, int64, error) {
	err := requirePending(h)
	if err != nil {
		return Result{}, 0, err
	}

	amount := ev.amountOr(0)

	return Result{Decision: domain.Approve, Status: StatusPending, HeldMinor: h.HeldMinor + amount}, amount, nil
}

// capture settles the hold at the final amount, which may be larger (D-053).
func capture(h Hold, ev Event) (Result, int64, error) {
	err := requirePending(h)
	if err != nil {
		return Result{}, 0, err
	}

	captured := ev.amountOr(h.HeldMinor)

	return Result{Status: StatusCaptured, CapturedMinor: captured}, captured - h.HeldMinor, nil
}

func refund(h Hold, ev Event) (Result, int64, error) {
	if h.Status != StatusCaptured {
		return Result{}, 0, ErrNotCaptured
	}

	left := h.CapturedMinor - h.RefundedMinor
	amount := ev.amountOr(left)

	if amount > left {
		return Result{}, 0, ErrOverRefund
	}

	status := StatusCaptured
	if amount == left {
		status = StatusRefunded
	}

	return Result{Status: status, CapturedMinor: h.CapturedMinor, RefundedMinor: h.RefundedMinor + amount}, -amount, nil
}
