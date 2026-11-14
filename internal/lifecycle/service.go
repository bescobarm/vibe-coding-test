package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bescobarm/card-auth-engine/internal/controls"
	"github.com/bescobarm/card-auth-engine/internal/domain"
	"github.com/bescobarm/card-auth-engine/internal/pipeline"
	"github.com/bescobarm/card-auth-engine/internal/policy"
)

// HoldReader reads an authorization's state without locking it.
type HoldReader interface {
	GetHold(ctx context.Context, authorizationID string) (Hold, error)
}

// CardReader loads a card, or returns domain.ErrNotFound.
type CardReader interface {
	GetCard(ctx context.Context, cardID string) (domain.Card, error)
}

// PolicyReader returns the company's latest policy, or domain.ErrNotFound.
type PolicyReader interface {
	GetPolicy(ctx context.Context, companyID string) (policy.Document, error)
}

// Service applies lifecycle events.
type Service struct {
	store    Store
	reader   HoldReader
	holds    Holds
	cards    CardReader
	policies PolicyReader
	now      func() time.Time
}

// NewService returns a lifecycle Service that uses the real clock.
func NewService(s Store, r HoldReader, h Holds, c CardReader, p PolicyReader) *Service {
	return &Service{store: s, reader: r, holds: h, cards: c, policies: p, now: time.Now}
}

// incrementContext is what an increment is checked against, loaded before the
// transaction: no Postgres reads may happen while it holds a connection (D-029).
type incrementContext struct {
	card   domain.Card
	policy policy.Document
}

// Apply runs one event. Redis changes before Postgres commits: if the commit
// fails, the retry finds the event_id already applied in Redis.
func (s *Service) Apply(ctx context.Context, ev Event) (Result, bool, error) {
	var ic incrementContext

	if ev.Type == TypeIncrement {
		loaded, err := s.loadIncrementContext(ctx, ev.AuthorizationID)
		if err != nil {
			return Result{}, false, err
		}

		ic = loaded
	}

	return s.store.ApplyEvent(ctx, ev, func(ctx context.Context, h Hold) (Result, error) {
		res, delta, err := ComputeTransition(h, ev)
		if err != nil {
			return Result{}, err
		}

		if ev.Type == TypeIncrement {
			return s.checkIncrement(ctx, h, ev, res, delta, ic)
		}

		return res, s.adjust(ctx, h, ev, delta)
	})
}

func (s *Service) loadIncrementContext(ctx context.Context, authorizationID string) (incrementContext, error) {
	hold, err := s.reader.GetHold(ctx, authorizationID)
	if err != nil {
		return incrementContext{}, err
	}

	card, err := s.cards.GetCard(ctx, hold.CardID)
	if err != nil {
		return incrementContext{}, fmt.Errorf("load card: %w", err)
	}

	doc, err := s.policies.GetPolicy(ctx, card.CompanyID)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return incrementContext{}, fmt.Errorf("load policy: %w", err)
	}

	return incrementContext{card: card, policy: doc}, nil
}

// checkIncrement re-runs the card status and per-transaction limit on the new
// total, then raises the hold in Redis within the amount windows (D-054).
// A declined increment leaves the hold as it was.
func (s *Service) checkIncrement(ctx context.Context, h Hold, ev Event, res Result, amount int64, ic incrementContext) (Result, error) {
	ctrls, err := policy.MergeOverrides(ic.policy.Controls, ic.card.Overrides)
	if err != nil {
		return Result{}, fmt.Errorf("card %s: %w", ic.card.ID, err)
	}

	evaluation := pipeline.NewEvaluation(domain.AuthRequest{CardID: h.CardID, AmountMinor: res.HeldMinor, Currency: h.Currency})
	evaluation.Card, evaluation.CardFound, evaluation.Controls = ic.card, true, ctrls

	err = pipeline.New(controls.CardStatus{}, controls.TransactionLimit{}).Run(ctx, evaluation)
	if err != nil {
		return Result{}, err
	}

	if evaluation.IsHalted() {
		return markDeclined(h, res, evaluation.GetReasons()), nil
	}

	limit, err := s.holds.Increase(ctx, Increase{
		CardID: h.CardID, AuthorizationID: h.AuthorizationID, EventID: ev.ID, AmountMinor: amount, TotalMinor: res.HeldMinor,
		Currency: h.Currency, MCC: h.MCC, Now: s.now(), Policy: ic.policy,
	})
	if err != nil {
		return Result{}, fmt.Errorf("increase: %w", err)
	}

	if limit.Exceeded {
		return markDeclined(h, res, []domain.Reason{{Code: limit.Code, Message: limit.Message}}), nil
	}

	return res, nil
}

func markDeclined(h Hold, res Result, reasons []domain.Reason) Result {
	res.Decision, res.Reasons, res.HeldMinor = domain.Decline, reasons, h.HeldMinor
	return res
}

func (s *Service) adjust(ctx context.Context, h Hold, ev Event, delta int64) error {
	if delta == 0 {
		return nil
	}

	err := s.holds.Adjust(ctx, Adjust{CardID: h.CardID, AuthorizationID: h.AuthorizationID, EventID: ev.ID, DeltaMinor: delta})
	if err != nil {
		return fmt.Errorf("adjust: %w", err)
	}

	return nil
}
