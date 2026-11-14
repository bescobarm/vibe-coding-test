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
	for _, rule := range inc.Policy.Velocity {
		if rule.MaxAmount != nil && rule.Applies(inc.TotalMinor, inc.MCC) {
			rules = append(rules, rule)
		}
	}

	if len(rules) == 0 {
		return lifecycle.Limit{}, nil
	}

	for _, rule := range rules {
		if rule.MaxAmount.Currency != inc.Currency {
			return lifecycle.Limit{Exceeded: true, Code: getAmountReason(rule),
				Message: fmt.Sprintf(fmtCurrencyMismatch, rule.ID, rule.MaxAmount.Currency, inc.Currency)}, nil
		}
	}

	ctx, cancel := context.WithTimeout(ctx, l.timeout)
	defer cancel()

	keys, args := buildIncreaseInput(inc, rules)

	res, err := runScript(ctx, l.rdb, increaseScript, keys, args)
	if err != nil {
		return lifecycle.Limit{}, err
	}

	if res.Reserved {
		return lifecycle.Limit{}, nil
	}

	code, msg := describeLimitHit(rules[res.Window], res, inc.AmountMinor)
	return lifecycle.Limit{Exceeded: true, Code: code, Message: msg}, nil
}

func buildIncreaseInput(inc lifecycle.Increase, rules []policy.VelocityRule) ([]string, []any) {
	loc := inc.Policy.GetLocation()
	keys := []string{fmt.Sprintf(fmtHoldKey, inc.CardID, inc.AuthorizationID)}
	args := make([]any, 0, increaseFixedArgs+len(rules))
	args = append(args, inc.AuthorizationID, inc.EventID, inc.AmountMinor, inc.Now.UnixMilli(), holdRetention.Milliseconds())

	for _, rule := range rules {
		prefix := fmt.Sprintf(fmtWindowKeys, inc.CardID, rule.ID)
		keys = append(keys, prefix+suffixTimes, prefix+suffixAmounts)
		args = append(args, rule.GetWindowStart(inc.Now, loc).UnixMilli(), rule.MaxAmount.AmountMinor, rule.GetRetention().Milliseconds())
	}

	return keys, args
}
