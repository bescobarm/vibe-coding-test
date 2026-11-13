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
	"github.com/bescobarm/card-auth-engine/internal/policy"
)

const topicDecided = "authorization.decided"

//go:embed seed_demo.sql
var demoSeed string

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// SeedDemo inserts demo companies and cards. It is safe to run on every start.
func (s *Store) SeedDemo(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, demoSeed)
	return err
}

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
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
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

const recordSQL = `
UPDATE authorizations
SET decision = $2, approved_minor = $3, status = $4, risk_score = $5, reasons = $6,
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

	status := "pending" // an approval places a hold
	if resp.Decision == domain.Decline {
		status = "declined"
	}
	if _, err := tx.Exec(ctx, recordSQL,
		req.AuthorizationID, resp.Decision, resp.ApprovedAmountMinor, status, resp.RiskScore,
		resp.Reasons, resp.PolicyVersion, resp.FallbackUsed, resp.LatencyMS, resp,
	); err != nil {
		return domain.AuthResponse{}, false, fmt.Errorf("record: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO outbox (topic, payload) VALUES ($1, $2)`, topicDecided, resp,
	); err != nil {
		return domain.AuthResponse{}, false, fmt.Errorf("outbox: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.AuthResponse{}, false, fmt.Errorf("commit: %w", err)
	}
	return resp, false, nil
}

func (s *Store) storedResponse(ctx context.Context, id string) (domain.AuthResponse, error) {
	var raw []byte
	if err := s.pool.QueryRow(ctx,
		`SELECT response FROM authorizations WHERE authorization_id = $1`, id,
	).Scan(&raw); err != nil {
		return domain.AuthResponse{}, fmt.Errorf("load stored response: %w", err)
	}
	var resp domain.AuthResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return domain.AuthResponse{}, fmt.Errorf("decode stored response: %w", err)
	}
	return resp, nil
}

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
