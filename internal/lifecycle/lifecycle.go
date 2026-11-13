// Package lifecycle applies what happens to an approved authorization after
// the decision: reversals and expiry release its hold (FR6, D-012). Every call
// carries an event_id, so retries are safe (D-050).
package lifecycle

import (
	"context"
	"errors"
	"fmt"
)

// Event types.
const (
	TypeReversal = "reversal"
	TypeExpiry   = "expiry"
)

// Authorization statuses after the decision.
const (
	StatusPending  = "pending"
	StatusDeclined = "declined"
	StatusReversed = "reversed"
	StatusExpired  = "expired"
)

// Errors a caller can act on. Each message is shown to the API client.
var (
	ErrDeclined      = errors.New("authorization was declined, nothing is held")
	ErrNotPending    = errors.New("authorization is no longer pending")
	ErrOverHold      = errors.New("amount is larger than what is still held")
	ErrEventConflict = errors.New("event_id was already used for a different event")
)

// Event is one lifecycle call. AmountMinor is nil for "the whole hold".
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

// Hold is the current state of an authorization, read under a row lock.
type Hold struct {
	AuthorizationID string
	CardID          string
	Status          string
	HeldMinor       int64
}

// Result is the answer to a lifecycle call. It is stored with the event and
// returned unchanged when the same event_id is sent again.
type Result struct {
	AuthorizationID string `json:"authorization_id"`
	EventID         string `json:"event_id"`
	Type            string `json:"type"`
	Status          string `json:"status"`
	ReleasedMinor   int64  `json:"released_minor"`
	HeldMinor       int64  `json:"held_minor"`
}

// ApplyFunc changes one locked authorization and returns the result to store.
type ApplyFunc func(ctx context.Context, h Hold) (Result, error)

// Store locks the authorization, replays a known event_id, or runs apply and
// stores the event and the new hold in the same transaction.
type Store interface {
	ApplyEvent(ctx context.Context, ev Event, apply ApplyFunc) (res Result, replayed bool, err error)
}

// Release asks Redis to free part of a card's hold.
type Release struct {
	CardID          string
	AuthorizationID string
	EventID         string
	AmountMinor     int64
}

// Releaser frees held amounts from the velocity and budget windows.
type Releaser interface {
	Release(ctx context.Context, r Release) error
}

// Service applies lifecycle events.
type Service struct {
	store    Store
	releaser Releaser
}

// NewService returns a lifecycle Service.
func NewService(s Store, r Releaser) *Service {
	return &Service{store: s, releaser: r}
}

// Apply runs one event. Redis is released before Postgres commits: if the
// commit fails, the retry finds the event_id already applied in Redis and does
// not release twice.
func (s *Service) Apply(ctx context.Context, ev Event) (Result, bool, error) {
	return s.store.ApplyEvent(ctx, ev, func(ctx context.Context, h Hold) (Result, error) {
		res, err := Transition(h, ev)
		if err != nil {
			return Result{}, err
		}

		err = s.releaser.Release(ctx, Release{
			CardID: h.CardID, AuthorizationID: h.AuthorizationID, EventID: ev.ID, AmountMinor: res.ReleasedMinor,
		})
		if err != nil {
			return Result{}, fmt.Errorf("release: %w", err)
		}

		return res, nil
	})
}

// Transition computes the new hold. Only a pending authorization can change.
func Transition(h Hold, ev Event) (Result, error) {
	switch h.Status {
	case StatusPending:
	case StatusDeclined:
		return Result{}, ErrDeclined
	default:
		return Result{}, ErrNotPending
	}

	release := h.HeldMinor
	if ev.Type == TypeReversal && ev.AmountMinor != nil {
		release = *ev.AmountMinor
	}

	if release > h.HeldMinor {
		return Result{}, ErrOverHold
	}

	left := h.HeldMinor - release

	return Result{
		AuthorizationID: h.AuthorizationID,
		EventID:         ev.ID,
		Type:            ev.Type,
		Status:          nextStatus(ev.Type, left),
		ReleasedMinor:   release,
		HeldMinor:       left,
	}, nil
}

// nextStatus: a partial reversal keeps the authorization pending.
func nextStatus(eventType string, left int64) string {
	switch {
	case eventType == TypeExpiry:
		return StatusExpired
	case left == 0:
		return StatusReversed
	default:
		return StatusPending
	}
}
