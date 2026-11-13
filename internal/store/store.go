// Package store is the Postgres layer: cards, policies, the authorization
// audit log (which is also the idempotency store, D-024) and the outbox.
package store

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bescobarm/card-auth-engine/internal/authz"
	"github.com/bescobarm/card-auth-engine/internal/domain"
	"github.com/bescobarm/card-auth-engine/internal/lifecycle"
	"github.com/bescobarm/card-auth-engine/internal/policy"
)

const (
	topicDecided    = "authorization.decided"
	topicLifecycle  = "authorization.lifecycle"
	uniqueViolation = "23505" // Postgres SQLSTATE
)

//go:embed seed_demo.sql
var demoSeed string

// Store implements the engine's storage interfaces on Postgres.
type Store struct {
	pool *pgxpool.Pool
}

// New returns a Store that uses pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Ping checks that Postgres is reachable.
func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// SeedDemo inserts demo companies and cards. It is safe to run on every start.
func (s *Store) SeedDemo(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, demoSeed)
	return err
}

// GetCard loads a card, or returns domain.ErrNotFound.
func (s *Store) GetCard(ctx context.Context, cardID string) (domain.Card, error) {
	var c domain.Card
	err := s.pool.QueryRow(ctx,
		`SELECT card_id, company_id, cardholder_id, status, overrides FROM cards WHERE card_id = $1`, cardID,
	).Scan(&c.ID, &c.CompanyID, &c.CardholderID, &c.Status, &c.Overrides)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Card{}, domain.ErrNotFound
	}

	return c, err
}

// LatestPolicy implements policy.Store.
func (s *Store) LatestPolicy(ctx context.Context, companyID string) (policy.Document, error) {
	var raw []byte
	err := s.pool.QueryRow(ctx,
		`SELECT document FROM policies WHERE company_id = $1 ORDER BY version DESC LIMIT 1`, companyID,
	).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return policy.Document{}, domain.ErrNotFound
	}

	if err != nil {
		return policy.Document{}, err
	}

	return policy.Load(raw)
}

// PublishPolicy implements policy.Store. It inserts the next version and writes
// that version into the stored document. Two concurrent publishes for the same
// company collide on the primary key and the loser gets ErrConflict.
func (s *Store) PublishPolicy(ctx context.Context, doc policy.Document) (int, error) {
	var v int
	err := s.pool.QueryRow(ctx, `
		WITH next AS (SELECT COALESCE(MAX(version), 0) + 1 AS v FROM policies WHERE company_id = $1)
		INSERT INTO policies (company_id, version, document, created_by)
		SELECT $1, v, $2::jsonb || jsonb_build_object('version', v), 'api' FROM next
		RETURNING version`, doc.CompanyID, doc,
	).Scan(&v)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return 0, domain.ErrConflict
	}

	return v, err
}

const claimSQL = `
INSERT INTO authorizations
  (authorization_id, card_id, amount_minor, currency, mcc, merchant_id, merchant_country, entry_mode,
   decision, status, request)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'PENDING', 'claiming', $9)
ON CONFLICT (authorization_id) DO NOTHING`

const (
	outboxSQL         = `INSERT INTO outbox (topic, payload) VALUES ($1, $2)`
	storedResponseSQL = `SELECT response FROM authorizations WHERE authorization_id = $1`
)

const recordSQL = `
UPDATE authorizations
SET decision = $2, approved_minor = $3, held_minor = $3, status = $4, risk_score = $5, reasons = $6,
    policy_version = $7, fallback_used = $8, latency_ms = $9, response = $10, updated_at = now()
WHERE authorization_id = $1`

// Decide implements authz.Recorder.
//
// It claims the authorization_id with INSERT ... ON CONFLICT DO NOTHING inside a
// transaction. A concurrent request with the same ID blocks on the unique index
// until this transaction ends, then sees the conflict and gets the stored response.
// So decide runs at most once per ID, for retries and for simultaneous duplicates.
func (s *Store) Decide(ctx context.Context, req domain.AuthRequest, decide authz.DecideFunc) (domain.AuthResponse, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.AuthResponse{}, false, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit

	tag, err := tx.Exec(ctx, claimSQL,
		req.AuthorizationID, req.CardID, req.AmountMinor, req.Currency, req.Merchant.MCC,
		req.Merchant.ID, req.Merchant.Country, req.EntryMode, req)
	if err != nil {
		return domain.AuthResponse{}, false, fmt.Errorf("claim: %w", err)
	}

	if tag.RowsAffected() == 0 {
		_ = tx.Rollback(ctx)
		resp, err := s.storedResponse(ctx, req.AuthorizationID)
		return resp, true, err
	}

	resp, err := decide(ctx)
	if err != nil {
		return domain.AuthResponse{}, false, err
	}

	status := lifecycle.StatusPending
	if resp.Decision == domain.Decline {
		status = lifecycle.StatusDeclined
	}

	_, err = tx.Exec(ctx, recordSQL,
		req.AuthorizationID, resp.Decision, resp.ApprovedAmountMinor, status, resp.RiskScore,
		resp.Reasons, resp.PolicyVersion, resp.FallbackUsed, resp.LatencyMS, resp)
	if err != nil {
		return domain.AuthResponse{}, false, fmt.Errorf("record: %w", err)
	}

	_, err = tx.Exec(ctx, outboxSQL, topicDecided, resp)
	if err != nil {
		return domain.AuthResponse{}, false, fmt.Errorf("outbox: %w", err)
	}

	err = tx.Commit(ctx)
	if err != nil {
		return domain.AuthResponse{}, false, fmt.Errorf("commit: %w", err)
	}

	return resp, false, nil
}

func (s *Store) storedResponse(ctx context.Context, id string) (domain.AuthResponse, error) {
	var raw []byte
	err := s.pool.QueryRow(ctx, storedResponseSQL, id).Scan(&raw)
	if err != nil {
		return domain.AuthResponse{}, fmt.Errorf("load stored response: %w", err)
	}

	var resp domain.AuthResponse

	err = json.Unmarshal(raw, &resp)
	if err != nil {
		return domain.AuthResponse{}, fmt.Errorf("decode stored response: %w", err)
	}

	return resp, nil
}

// GetAuthorization reads one authorization from the audit log, or returns domain.ErrNotFound.
func (s *Store) GetAuthorization(ctx context.Context, id string) (domain.AuditRecord, error) {
	var (
		rec                    domain.AuditRecord
		request, resp, explain []byte
	)
	err := s.pool.QueryRow(ctx, `
		SELECT authorization_id, status, request, response, explanation, created_at
		FROM authorizations WHERE authorization_id = $1`, id,
	).Scan(&rec.AuthorizationID, &rec.Status, &request, &resp, &explain, &rec.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AuditRecord{}, domain.ErrNotFound
	}

	if err != nil {
		return domain.AuditRecord{}, err
	}

	rec.Request, rec.Response, rec.Explanation = request, resp, explain
	return rec, nil
}


const (
	lockHoldSQL = `SELECT authorization_id, card_id, status, held_minor
		FROM authorizations WHERE authorization_id = $1 FOR UPDATE`
	claimEventSQL = `INSERT INTO lifecycle_events (event_id, authorization_id, type, amount_minor, response)
		VALUES ($1, $2, $3, $4, '{}') ON CONFLICT (event_id) DO NOTHING`
	storedEventSQL = `SELECT authorization_id, type, amount_minor, response FROM lifecycle_events WHERE event_id = $1`
	recordEventSQL = `UPDATE lifecycle_events SET response = $2 WHERE event_id = $1`
	updateHoldSQL  = `UPDATE authorizations SET status = $2, held_minor = $3, updated_at = now() WHERE authorization_id = $1`
)

// ApplyEvent implements lifecycle.Store.
//
// The authorization row is locked first, so events on one authorization run
// one at a time. The event_id is claimed like an authorization_id in Decide:
// a concurrent call with the same event_id waits on the unique index, then
// replays the stored result, before anything reaches Redis.
func (s *Store) ApplyEvent(ctx context.Context, ev lifecycle.Event, apply lifecycle.ApplyFunc) (lifecycle.Result, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return lifecycle.Result{}, false, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit

	var h lifecycle.Hold

	err = tx.QueryRow(ctx, lockHoldSQL, ev.AuthorizationID).Scan(&h.AuthorizationID, &h.CardID, &h.Status, &h.HeldMinor)
	if errors.Is(err, pgx.ErrNoRows) {
		return lifecycle.Result{}, false, domain.ErrNotFound
	}

	if err != nil {
		return lifecycle.Result{}, false, fmt.Errorf("lock authorization: %w", err)
	}

	tag, err := tx.Exec(ctx, claimEventSQL, ev.ID, ev.AuthorizationID, ev.Type, ev.AmountMinor)
	if err != nil {
		return lifecycle.Result{}, false, fmt.Errorf("claim event: %w", err)
	}

	if tag.RowsAffected() == 0 {
		_ = tx.Rollback(ctx)
		res, err := s.storedEvent(ctx, ev)
		return res, true, err
	}

	res, err := apply(ctx, h)
	if err != nil {
		return lifecycle.Result{}, false, err
	}

	err = recordEvent(ctx, tx, res)
	if err != nil {
		return lifecycle.Result{}, false, err
	}

	err = tx.Commit(ctx)
	if err != nil {
		return lifecycle.Result{}, false, fmt.Errorf("commit: %w", err)
	}

	return res, false, nil
}

func recordEvent(ctx context.Context, tx pgx.Tx, res lifecycle.Result) error {
	_, err := tx.Exec(ctx, recordEventSQL, res.EventID, res)
	if err != nil {
		return fmt.Errorf("record event: %w", err)
	}

	_, err = tx.Exec(ctx, updateHoldSQL, res.AuthorizationID, res.Status, res.HeldMinor)
	if err != nil {
		return fmt.Errorf("update hold: %w", err)
	}

	_, err = tx.Exec(ctx, outboxSQL, topicLifecycle, res)
	if err != nil {
		return fmt.Errorf("outbox: %w", err)
	}

	return nil
}

// storedEvent replays a known event_id, or reports ErrEventConflict when the
// same event_id was used for a different call.
func (s *Store) storedEvent(ctx context.Context, ev lifecycle.Event) (lifecycle.Result, error) {
	stored := lifecycle.Event{ID: ev.ID}

	var raw []byte

	err := s.pool.QueryRow(ctx, storedEventSQL, ev.ID).Scan(&stored.AuthorizationID, &stored.Type, &stored.AmountMinor, &raw)
	if err != nil {
		return lifecycle.Result{}, fmt.Errorf("load stored event: %w", err)
	}

	if !stored.Same(ev) {
		return lifecycle.Result{}, lifecycle.ErrEventConflict
	}

	var res lifecycle.Result

	err = json.Unmarshal(raw, &res)
	if err != nil {
		return lifecycle.Result{}, fmt.Errorf("decode stored event: %w", err)
	}

	return res, nil
}
