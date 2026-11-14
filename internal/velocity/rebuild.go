package velocity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/bescobarm/card-auth-engine/internal/domain"
	"github.com/bescobarm/card-auth-engine/internal/lifecycle"
	"github.com/bescobarm/card-auth-engine/internal/policy"
)

// maxRetention is the longest a rule can look back (31 days), so older
// authorizations never need to be restored.
const maxRetention = 31 * 24 * time.Hour

// Fields of a hold hash, as the Lua scripts write them.
const (
	holdFieldAmount = "amount"
	holdFieldKeys   = "keys"
)

// Authorization is an approved authorization as Postgres stores it now.
type Authorization struct {
	lifecycle.Hold

	CompanyID   string
	AmountMinor int64 // the amount the decision approved, used by rule filters
	CreatedAt   time.Time
}

// RebuildSource reads approved authorizations, the source of truth (D-003).
type RebuildSource interface {
	ListApprovedSince(ctx context.Context, since time.Time) ([]Authorization, error)
}

// PolicyReader returns the company's latest policy, or domain.ErrNotFound.
type PolicyReader interface {
	GetPolicy(ctx context.Context, companyID string) (policy.Document, error)
}

// Rebuilder restores the Redis counters from Postgres after Redis lost them.
type Rebuilder struct {
	src       RebuildSource
	policies  PolicyReader
	rdb       redis.Cmdable
	markerKey string
	now       func() time.Time
}

// NewRebuilder returns a Rebuilder. markerKey is set once the counters are
// complete; when Redis loses its data it loses the marker too.
func NewRebuilder(src RebuildSource, policies PolicyReader, rdb redis.Cmdable, markerKey string) *Rebuilder {
	return &Rebuilder{src: src, policies: policies, rdb: rdb, markerKey: markerKey, now: time.Now}
}

// RebuildIfLost rebuilds the counters when the marker is missing (D-055). It
// must run before the engine serves, so no reserve races with it.
func (r *Rebuilder) RebuildIfLost(ctx context.Context) (rebuilt int, err error) {
	present, err := r.rdb.Exists(ctx, r.markerKey).Result()
	if err != nil {
		return 0, fmt.Errorf("check marker: %w", err)
	}

	if present == 1 {
		return 0, nil
	}

	restored, err := r.Rebuild(ctx)
	if err != nil {
		return 0, err
	}

	err = r.rdb.Set(ctx, r.markerKey, r.now().UTC().Format(time.RFC3339), 0).Err()
	if err != nil {
		return 0, fmt.Errorf("set marker: %w", err)
	}

	return restored, nil
}

// Rebuild writes every approved authorization of the last 31 days back into
// its windows and hold, as the scripts would have left them. It returns how
// many authorizations were restored. It sets values, so it is safe to repeat.
func (r *Rebuilder) Rebuild(ctx context.Context) (int, error) {
	now := r.now()

	auths, err := r.src.ListApprovedSince(ctx, now.Add(-maxRetention))
	if err != nil {
		return 0, fmt.Errorf("read authorizations: %w", err)
	}

	docs := map[string]policy.Document{}
	pipe := r.rdb.TxPipeline()
	restored := 0

	for _, authorization := range auths {
		doc, err := r.getPolicy(ctx, docs, authorization.CompanyID)
		if err != nil {
			return 0, err
		}

		ok, err := restoreAuthorization(ctx, pipe, authorization, doc, now)
		if err != nil {
			return 0, err
		}

		if ok {
			restored++
		}
	}

	_, err = pipe.Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("write counters: %w", err)
	}

	return restored, nil
}

func (r *Rebuilder) getPolicy(ctx context.Context, docs map[string]policy.Document, companyID string) (policy.Document, error) {
	doc, ok := docs[companyID]
	if ok {
		return doc, nil
	}

	doc, err := r.policies.GetPolicy(ctx, companyID)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return policy.Document{}, fmt.Errorf("load policy %s: %w", companyID, err)
	}

	docs[companyID] = doc
	return doc, nil
}

// restoreAuthorization queues one authorization's entries; it reports false when no rule applies.
func restoreAuthorization(ctx context.Context, pipe redis.Pipeliner, a Authorization, doc policy.Document, now time.Time) (bool, error) {
	counted := a.GetCountedMinor()
	loc := doc.GetLocation()

	var held []string
	for _, rule := range doc.Velocity {
		if !rule.Applies(a.AmountMinor, a.MCC) {
			continue
		}

		prefix := fmt.Sprintf(fmtWindowKeys, a.CardID, rule.ID)
		held = append(held, prefix+suffixAmounts)

		if a.CreatedAt.Before(rule.GetWindowStart(now, loc)) {
			continue
		}

		pipe.ZAdd(ctx, prefix+suffixTimes, redis.Z{Score: float64(a.CreatedAt.UnixMilli()), Member: a.AuthorizationID})
		pipe.PExpire(ctx, prefix+suffixTimes, rule.GetRetention())

		if counted > 0 {
			pipe.HSet(ctx, prefix+suffixAmounts, a.AuthorizationID, counted)
			pipe.PExpire(ctx, prefix+suffixAmounts, rule.GetRetention())
		}
	}

	if len(held) == 0 {
		return false, nil
	}

	keys, err := json.Marshal(held)
	if err != nil {
		return false, fmt.Errorf("encode hold keys: %w", err)
	}

	hold := fmt.Sprintf(fmtHoldKey, a.CardID, a.AuthorizationID)
	pipe.HSet(ctx, hold, holdFieldAmount, counted, holdFieldKeys, string(keys))
	pipe.PExpire(ctx, hold, holdRetention)

	return true, nil
}
