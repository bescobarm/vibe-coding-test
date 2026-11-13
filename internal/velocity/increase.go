package velocity

import (
	"context"
	"fmt"

	"github.com/bescobarm/card-auth-engine/internal/lifecycle"
	"github.com/bescobarm/card-auth-engine/internal/policy"
)

const increaseFixedArgs = 5

// Increase implements lifecycle.Holds. Only amount rules are checked: the
// count doesn't change, it is the same authorization (D-054).
func (l *Limiter) Increase(ctx context.Context, inc lifecycle.Increase) (lifecycle.Limit, error) {
	var rules []policy.VelocityRule
	for _, r := range inc.Policy.Velocity {
		if r.MaxAmount != nil && r.Applies(inc.TotalMinor, inc.MCC) {
			rules = append(rules, r)
		}
	}

	if len(rules) == 0 {
		return lifecycle.Limit{}, nil
	}

	for _, r := range rules {
		if r.MaxAmount.Currency != inc.Currency {
			return lifecycle.Limit{Exceeded: true, Code: amountReason(r),
				Message: fmt.Sprintf(fmtCurrencyMismatch, r.ID, r.MaxAmount.Currency, inc.Currency)}, nil
		}
	}

	ctx, cancel := context.WithTimeout(ctx, l.timeout)
	defer cancel()

	keys, args := increaseInput(inc, rules)

	res, err := run(ctx, l.rdb, increaseScript, keys, args)
	if err != nil {
		return lifecycle.Limit{}, err
	}

	if res.Reserved {
		return lifecycle.Limit{}, nil
	}

	code, msg := limitHit(rules[res.Window], res, inc.AmountMinor)
	return lifecycle.Limit{Exceeded: true, Code: code, Message: msg}, nil
}

func increaseInput(inc lifecycle.Increase, rules []policy.VelocityRule) ([]string, []any) {
	loc := inc.Policy.Location()
	keys := []string{fmt.Sprintf(fmtHoldKey, inc.CardID, inc.AuthorizationID)}
	args := make([]any, 0, increaseFixedArgs+len(rules))
	args = append(args, inc.AuthorizationID, inc.EventID, inc.AmountMinor, inc.Now.UnixMilli(), holdRetention.Milliseconds())

	for _, r := range rules {
		prefix := fmt.Sprintf(fmtWindowKeys, inc.CardID, r.ID)
		keys = append(keys, prefix+suffixTimes, prefix+suffixAmounts)
		args = append(args, r.WindowStart(inc.Now, loc).UnixMilli(), r.MaxAmount.AmountMinor, r.Retention().Milliseconds())
	}

	return keys, args
}
