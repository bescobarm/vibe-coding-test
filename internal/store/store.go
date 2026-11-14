// Package store is the Postgres layer: cards, policies, the authorization
// audit log (which is also the idempotency store, D-024) and the outbox.
package store

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bescobarm/card-auth-engine/internal/authz"
	"github.com/bescobarm/card-auth-engine/internal/domain"
	"github.com/bescobarm/card-auth-engine/internal/explain"
	"github.com/bescobarm/card-auth-engine/internal/lifecycle"
	"github.com/bescobarm/card-auth-engine/internal/policy"
	"github.com/bescobarm/card-auth-engine/internal/velocity"
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
	var card domain.Card
	err := s.pool.QueryRow(ctx,
		`SELECT card_id, company_id, cardholder_id, status, overrides FROM cards WHERE card_id = $1`, cardID,
	).Scan(&card.ID, &card.CompanyID, &card.CardholderID, &card.Status, &card.Overrides)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Card{}, domain.ErrNotFound
	}

	return card, err
}

// GetLatestPolicy implements policy.Store.
func (s *Store) GetLatestPolicy(ctx context.Context, companyID string) (policy.Document, error) {
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
	var version int
	err := s.pool.QueryRow(ctx, `
		WITH next AS (SELECT COALESCE(MAX(version), 0) + 1 AS v FROM policies WHERE company_id = $1)
		INSERT INTO policies (company_id, version, document, created_by)
		SELECT $1, v, $2::jsonb || jsonb_build_object('version', v), 'api' FROM next
		RETURNING version`, doc.CompanyID, doc,
	).Scan(&version)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return 0, domain.ErrConflict
	}

	return version, err
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
		resp, err := s.getStoredResponse(ctx, req.AuthorizationID)
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

func (s *Store) getStoredResponse(ctx context.Context, id string) (domain.AuthResponse, error) {
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
	holdSQL = `SELECT authorization_id, card_id, currency, mcc, status, held_minor, captured_minor, refunded_minor
		FROM authorizations WHERE authorization_id = $1`
	lockHoldSQL   = holdSQL + ` FOR UPDATE`
	claimEventSQL = `INSERT INTO lifecycle_events (event_id, authorization_id, type, amount_minor, response)
		VALUES ($1, $2, $3, $4, '{}') ON CONFLICT (event_id) DO NOTHING`
	storedEventSQL = `SELECT authorization_id, type, amount_minor, response FROM lifecycle_events WHERE event_id = $1`
	recordEventSQL = `UPDATE lifecycle_events SET response = $2 WHERE event_id = $1`
	updateHoldSQL  = `UPDATE authorizations
		SET status = $2, held_minor = $3, captured_minor = $4, refunded_minor = $5, updated_at = now()
		WHERE authorization_id = $1`
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

	hold, err := scanHold(tx.QueryRow(ctx, lockHoldSQL, ev.AuthorizationID))
	if err != nil {
		return lifecycle.Result{}, false, err
	}

	tag, err := tx.Exec(ctx, claimEventSQL, ev.ID, ev.AuthorizationID, ev.Type, ev.AmountMinor)
	if err != nil {
		return lifecycle.Result{}, false, fmt.Errorf("claim event: %w", err)
	}

	if tag.RowsAffected() == 0 {
		_ = tx.Rollback(ctx)
		res, err := s.getStoredEvent(ctx, ev)
		return res, true, err
	}

	res, err := apply(ctx, hold)
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

	_, err = tx.Exec(ctx, updateHoldSQL, res.AuthorizationID, res.Status, res.HeldMinor, res.CapturedMinor, res.RefundedMinor)
	if err != nil {
		return fmt.Errorf("update hold: %w", err)
	}

	_, err = tx.Exec(ctx, outboxSQL, topicLifecycle, res)
	if err != nil {
		return fmt.Errorf("outbox: %w", err)
	}

	return nil
}

// getStoredEvent replays a known event_id, or reports ErrEventConflict when the
// same event_id was used for a different call.
func (s *Store) getStoredEvent(ctx context.Context, ev lifecycle.Event) (lifecycle.Result, error) {
	stored := lifecycle.Event{ID: ev.ID}

	var raw []byte

	err := s.pool.QueryRow(ctx, storedEventSQL, ev.ID).Scan(&stored.AuthorizationID, &stored.Type, &stored.AmountMinor, &raw)
	if err != nil {
		return lifecycle.Result{}, fmt.Errorf("load stored event: %w", err)
	}

	if !stored.IsSame(ev) {
		return lifecycle.Result{}, lifecycle.ErrEventConflict
	}

	var res lifecycle.Result

	err = json.Unmarshal(raw, &res)
	if err != nil {
		return lifecycle.Result{}, fmt.Errorf("decode stored event: %w", err)
	}

	return res, nil
}

// GetHold implements lifecycle.HoldReader.
func (s *Store) GetHold(ctx context.Context, authorizationID string) (lifecycle.Hold, error) {
	return scanHold(s.pool.QueryRow(ctx, holdSQL, authorizationID))
}

func scanHold(row pgx.Row) (lifecycle.Hold, error) {
	var hold lifecycle.Hold

	err := row.Scan(&hold.AuthorizationID, &hold.CardID, &hold.Currency, &hold.MCC, &hold.Status, &hold.HeldMinor, &hold.CapturedMinor, &hold.RefundedMinor)
	if errors.Is(err, pgx.ErrNoRows) {
		return lifecycle.Hold{}, domain.ErrNotFound
	}

	if err != nil {
		return lifecycle.Hold{}, fmt.Errorf("read authorization: %w", err)
	}

	return hold, nil
}

const approvedSinceSQL = `
SELECT a.authorization_id, a.card_id, c.company_id, a.amount_minor, a.mcc, a.currency, a.status,
       a.held_minor, a.captured_minor, a.refunded_minor, a.created_at
FROM authorizations a JOIN cards c ON c.card_id = a.card_id
WHERE a.decision = 'APPROVE' AND a.created_at >= $1`

// ListApprovedSince implements velocity.RebuildSource.
func (s *Store) ListApprovedSince(ctx context.Context, since time.Time) ([]velocity.Authorization, error) {
	rows, err := s.pool.Query(ctx, approvedSinceSQL, since)
	if err != nil {
		return nil, fmt.Errorf("query approved: %w", err)
	}

	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (velocity.Authorization, error) {
		var authorization velocity.Authorization

		err := row.Scan(&authorization.AuthorizationID, &authorization.CardID, &authorization.CompanyID, &authorization.AmountMinor, &authorization.MCC, &authorization.Currency, &authorization.Status,
			&authorization.HeldMinor, &authorization.CapturedMinor, &authorization.RefundedMinor, &authorization.CreatedAt)

		return authorization, err
	})
}

// signalsSQL reads what the anomaly features need in one round trip (D-058,
// D-059). The cardholder's history counts approvals only; the card-testing
// count includes declined attempts, because testers get declined too.
const signalsSQL = `
WITH history AS (
  SELECT count(*) AS approved,
         coalesce(bool_or(a.merchant_country = $2), false) AS country_seen,
         coalesce(bool_or(a.merchant_id = $3), false) AS merchant_seen,
         count(*) FILTER (WHERE a.mcc = $4 AND a.currency = $5) AS mcc_count,
         coalesce(avg(a.amount_minor) FILTER (WHERE a.mcc = $4 AND a.currency = $5), 0)::float8 AS mcc_mean,
         coalesce(stddev_samp(a.amount_minor) FILTER (WHERE a.mcc = $4 AND a.currency = $5), 0)::float8 AS mcc_stddev
  FROM authorizations a JOIN cards c ON c.card_id = a.card_id
  WHERE c.cardholder_id = $1 AND a.decision = 'APPROVE' AND a.created_at >= $6
), recent AS (
  SELECT count(*) FILTER (WHERE created_at >= $8 AND amount_minor <= $9 AND decision <> 'PENDING') AS small_recent,
         coalesce(array_agg(amount_minor ORDER BY created_at)
                  FILTER (WHERE merchant_id = $3 AND currency = $5 AND decision = 'APPROVE' AND created_at >= $10),
                  '{}') AS same_merchant_today
  FROM authorizations
  WHERE card_id = $7 AND created_at >= LEAST($8, $10)
)
SELECT approved, country_seen, merchant_seen, mcc_count, mcc_mean, mcc_stddev, small_recent, same_merchant_today
FROM history, recent`

// GetSignals implements authz.SignalReader.
func (s *Store) GetSignals(ctx context.Context, q domain.SignalQuery) (domain.Signals, error) {
	var sig domain.Signals

	err := s.pool.QueryRow(ctx, signalsSQL,
		q.CardholderID, q.Country, q.MerchantID, q.MCC, q.Currency, q.HistorySince,
		q.CardID, q.RecentSince, q.SmallMaxMinor, q.DayStart,
	).Scan(&sig.Approved, &sig.CountrySeen, &sig.MerchantSeen, &sig.MCCCount, &sig.MCCMeanMinor,
		&sig.MCCStdDevMinor, &sig.SmallRecent, &sig.SameMerchantToday)
	if err != nil {
		return domain.Signals{}, fmt.Errorf("read signals: %w", err)
	}

	return sig, nil
}

// metricsSQL counts decided authorizations since $1. A claim still in flight
// has decision PENDING and is left out. With no rows the percentiles are NULL.
const metricsSQL = `
SELECT count(*),
       count(*) FILTER (WHERE decision = 'APPROVE'),
       count(*) FILTER (WHERE decision = 'DECLINE'),
       count(*) FILTER (WHERE (response->>'flagged')::boolean),
       count(*) FILTER (WHERE fallback_used),
       round(coalesce(percentile_cont(0.50) WITHIN GROUP (ORDER BY latency_ms), 0)::numeric, 2)::float8,
       round(coalesce(percentile_cont(0.95) WITHIN GROUP (ORDER BY latency_ms), 0)::numeric, 2)::float8,
       round(coalesce(percentile_cont(0.99) WITHIN GROUP (ORDER BY latency_ms), 0)::numeric, 2)::float8
FROM authorizations
WHERE created_at >= $1 AND decision <> 'PENDING'`

// declineReasonsSQL counts every reason on the declines since $1.
const declineReasonsSQL = `
SELECT reason->>'code', count(*)
FROM authorizations, jsonb_array_elements(reasons) AS reason
WHERE created_at >= $1 AND decision = 'DECLINE'
GROUP BY 1`

// GetMetrics implements api.MetricsReader.
func (s *Store) GetMetrics(ctx context.Context, since time.Time) (domain.Metrics, error) {
	metrics := domain.Metrics{Since: since, DeclineReasons: map[string]int{}}

	err := s.pool.QueryRow(ctx, metricsSQL, since).Scan(&metrics.Decisions, &metrics.Approved, &metrics.Declined,
		&metrics.Flagged, &metrics.FallbackUsed, &metrics.LatencyMS.P50, &metrics.LatencyMS.P95, &metrics.LatencyMS.P99)
	if err != nil {
		return domain.Metrics{}, fmt.Errorf("read metrics: %w", err)
	}

	rows, err := s.pool.Query(ctx, declineReasonsSQL, since)
	if err != nil {
		return domain.Metrics{}, fmt.Errorf("read decline reasons: %w", err)
	}

	defer rows.Close()

	for rows.Next() {
		var (
			code  string
			count int
		)

		err = rows.Scan(&code, &count)
		if err != nil {
			return domain.Metrics{}, fmt.Errorf("scan decline reason: %w", err)
		}

		metrics.DeclineReasons[code] = count
	}

	if metrics.Decisions > 0 {
		metrics.ApprovalRate = float64(metrics.Approved) / float64(metrics.Decisions)
	}

	return metrics, rows.Err()
}

// pendingDecisionsSQL reads decision events not yet published, oldest first,
// with the request each one answered (the explainer needs the merchant).
const pendingDecisionsSQL = `
SELECT o.id, o.payload, a.request
FROM outbox o JOIN authorizations a ON a.authorization_id = o.payload->>'authorization_id'
WHERE o.published_at IS NULL AND o.topic = 'authorization.decided' AND ` + needsExplanationSQL + `
ORDER BY o.id
LIMIT $1`

// needsExplanationSQL matches declines and flags (explain.NeedsExplanation).
const needsExplanationSQL = `(o.payload->>'decision' = 'DECLINE' OR coalesce((o.payload->>'flagged')::boolean, false))`

// publishUnexplainedSQL publishes every decision event that needs no
// explanation in one statement, so approvals never wait behind slow ones.
const publishUnexplainedSQL = `
UPDATE outbox o SET published_at = now()
WHERE o.published_at IS NULL AND o.topic = 'authorization.decided' AND NOT ` + needsExplanationSQL

const (
	// saveExplanationSQL never overwrites: the outbox delivers at least once.
	saveExplanationSQL = `UPDATE authorizations SET explanation = $2 WHERE authorization_id = $1 AND explanation IS NULL`
	publishEventSQL    = `UPDATE outbox SET published_at = now() WHERE id = $1`
)

// PublishUnexplained implements explain.Outbox.
func (s *Store) PublishUnexplained(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, publishUnexplainedSQL)
	if err != nil {
		return 0, fmt.Errorf("publish unexplained: %w", err)
	}

	return tag.RowsAffected(), nil
}

// ListPendingDecisions implements explain.Outbox.
func (s *Store) ListPendingDecisions(ctx context.Context, limit int) ([]explain.Pending, error) {
	rows, err := s.pool.Query(ctx, pendingDecisionsSQL, limit)
	if err != nil {
		return nil, fmt.Errorf("query pending decisions: %w", err)
	}

	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (explain.Pending, error) {
		var (
			pending           explain.Pending
			response, request []byte
		)

		err := row.Scan(&pending.OutboxID, &response, &request)
		if err != nil {
			return explain.Pending{}, err
		}

		err = json.Unmarshal(response, &pending.Response)
		if err != nil {
			return explain.Pending{}, fmt.Errorf("decode response: %w", err)
		}

		return pending, json.Unmarshal(request, &pending.Request)
	})
}

// CompleteDecision implements explain.Outbox.
func (s *Store) CompleteDecision(ctx context.Context, outboxID int64, authorizationID string, explanation *explain.Explanation) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit

	if explanation != nil {
		_, err = tx.Exec(ctx, saveExplanationSQL, authorizationID, explanation)
		if err != nil {
			return fmt.Errorf("save explanation: %w", err)
		}
	}

	_, err = tx.Exec(ctx, publishEventSQL, outboxID)
	if err != nil {
		return fmt.Errorf("publish event: %w", err)
	}

	return tx.Commit(ctx)
}
