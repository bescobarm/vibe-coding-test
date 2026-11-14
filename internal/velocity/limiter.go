// Package velocity enforces velocity and budget rules with one atomic Redis
// Lua script per request (D-003): check every window and reserve the amount.
package velocity

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/bescobarm/card-auth-engine/internal/lifecycle"
)

//go:embed reserve.lua
var reserveLua string

//go:embed adjust.lua
var adjustLua string

//go:embed increase.lua
var increaseLua string

//go:embed cancel.lua
var cancelLua string

var (
	reserveScript  = redis.NewScript(reserveLua)
	adjustScript   = redis.NewScript(adjustLua)
	increaseScript = redis.NewScript(increaseLua)
	cancelScript   = redis.NewScript(cancelLua)
)

// ErrUnexpectedReply means the script answered something the engine can't read.
var ErrUnexpectedReply = errors.New("unexpected reply from a velocity script")

// NoLimit marks a window without a count or amount limit.
const NoLimit = -1

const (
	// holdRetention keeps a hold longer than any authorization can stay pending.
	holdRetention = 35 * 24 * time.Hour

	fmtWindowKeys = "vel:{%s}:%s:"
	suffixTimes   = "ts"
	suffixAmounts = "amt"
	fmtHoldKey    = "hold:{%s}:%s"

	replyLen      = 4
	replyReserved = 1
	argsPerRule   = 4
	fixedArgs     = 4
	keysPerRule   = 2
)

// Window is one rule as the script sees it.
type Window struct {
	RuleID    string
	Start     time.Time // oldest instant that still counts
	MaxCount  int64     // NoLimit when the rule has no count limit
	MaxAmount int64     // NoLimit when the rule has no amount limit
	Retention time.Duration
}

// Reservation asks to hold AmountMinor on a card against every window.
type Reservation struct {
	CardID          string
	AuthorizationID string
	AmountMinor     int64
	Now             time.Time
	Windows         []Window
}

// Result says whether the amount was reserved. When it wasn't, Window is the
// index of the first full window and Count and SpentMinor are what it held.
type Result struct {
	Reserved   bool
	Window     int
	Count      int64
	SpentMinor int64
}

// Limiter runs the reserve script on Redis.
type Limiter struct {
	rdb     redis.Scripter
	timeout time.Duration
}

// NewLimiter returns a Limiter whose calls fail after timeout (D-048).
func NewLimiter(rdb redis.Scripter, timeout time.Duration) *Limiter {
	return &Limiter{rdb: rdb, timeout: timeout}
}

// Reserve checks every window and, only if all have room, records the
// authorization in all of them. It is idempotent per authorization_id.
func (l *Limiter) Reserve(ctx context.Context, r Reservation) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, l.timeout)
	defer cancel()

	keys, args := scriptInput(r)

	return run(ctx, l.rdb, reserveScript, keys, args)
}

// run reads the {ok, window, count, spent} reply shared by reserve and increase.
func run(ctx context.Context, rdb redis.Scripter, script *redis.Script, keys []string, args []any) (Result, error) {
	reply, err := script.Run(ctx, rdb, keys, args...).Int64Slice()
	if err != nil {
		return Result{}, fmt.Errorf("run script: %w", err)
	}

	if len(reply) != replyLen {
		return Result{}, fmt.Errorf("%w: %v", ErrUnexpectedReply, reply)
	}

	if reply[0] == replyReserved {
		return Result{Reserved: true}, nil
	}

	return Result{Window: int(reply[1]) - 1, Count: reply[2], SpentMinor: reply[3]}, nil
}

func scriptInput(r Reservation) ([]string, []any) {
	keys := make([]string, 0, 1+keysPerRule*len(r.Windows))
	keys = append(keys, fmt.Sprintf(fmtHoldKey, r.CardID, r.AuthorizationID))

	args := make([]any, 0, fixedArgs+argsPerRule*len(r.Windows))
	args = append(args, r.AuthorizationID, r.AmountMinor, r.Now.UnixMilli(), holdRetention.Milliseconds())

	for _, w := range r.Windows {
		prefix := fmt.Sprintf(fmtWindowKeys, r.CardID, w.RuleID)
		keys = append(keys, prefix+suffixTimes, prefix+suffixAmounts)
		args = append(args, w.Start.UnixMilli(), w.MaxCount, w.MaxAmount, w.Retention.Milliseconds())
	}

	return keys, args
}

// Adjust implements lifecycle.Holds. It is idempotent per event_id, and a
// card without a hold (no rule applied when it was approved) is a no-op.
func (l *Limiter) Adjust(ctx context.Context, a lifecycle.Adjust) error {
	ctx, cancel := context.WithTimeout(ctx, l.timeout)
	defer cancel()

	key := fmt.Sprintf(fmtHoldKey, a.CardID, a.AuthorizationID)

	err := adjustScript.Run(ctx, l.rdb, []string{key}, a.AuthorizationID, a.EventID, a.DeltaMinor).Err()
	if err != nil {
		return fmt.Errorf("adjust: %w", err)
	}

	return nil
}

// Cancel removes a reservation that a later stage declined (D-023).
func (l *Limiter) Cancel(ctx context.Context, cardID, authorizationID string) error {
	ctx, cancel := context.WithTimeout(ctx, l.timeout)
	defer cancel()

	key := fmt.Sprintf(fmtHoldKey, cardID, authorizationID)

	err := cancelScript.Run(ctx, l.rdb, []string{key}, authorizationID).Err()
	if err != nil {
		return fmt.Errorf("cancel: %w", err)
	}

	return nil
}
