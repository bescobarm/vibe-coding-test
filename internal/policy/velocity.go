package policy

import (
	"fmt"
	"regexp"
	"slices"
	"time"

	"github.com/bescobarm/card-auth-engine/internal/domain"
)

// VelocityRule limits how many authorizations, or how much money, a card can
// approve in a window (D-052). Filter narrows the requests the rule counts.
type VelocityRule struct {
	ID        string          `json:"id"`
	Scope     string          `json:"scope,omitempty"` // only ScopeCard (D-044); empty means card
	Window    string          `json:"window"`
	MaxCount  *int            `json:"max_count,omitempty"`
	MaxAmount *Money          `json:"max_amount,omitempty"` // D-049: a request in another currency is declined
	Filter    *VelocityFilter `json:"filter,omitempty"`
	Reason    string          `json:"reason,omitempty"` // code when the count is full; VELOCITY_EXCEEDED by default (D-060)

	rolling time.Duration // set by validation when Window is a duration
}

// VelocityFilter makes a rule count only matching requests. Empty fields match everything.
type VelocityFilter struct {
	MaxAmountMinor int64    `json:"max_amount_minor,omitempty"`
	MCCs           []string `json:"mccs,omitempty"`
}

// Velocity scopes and calendar windows (D-044, D-052).
const (
	ScopeCard     = "card"
	CalendarDay   = "calendar_day"
	CalendarMonth = "calendar_month"
)

const (
	maxRollingWindow = 31 * 24 * time.Hour
	daysPerMonthMax  = 31
	hoursPerDay      = 24

	msgRuleID         = "must be 1-64 letters, digits, '-' or '_'"
	msgDuplicateID    = "duplicate rule id"
	msgScope          = "must be " + ScopeCard
	msgWindow         = "must be a duration up to 744h (e.g. 10m, 1h) or " + CalendarDay + " or " + CalendarMonth
	msgNeedsLimit     = "needs max_count or max_amount"
	fieldVelocityItem = "velocity[%d]"
	msgReason         = "must be " + domain.ReasonVelocityExceeded + " or " + domain.ReasonCardTestingPattern
)

var (
	ruleIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

	// ruleReasons are the codes a velocity rule may report when its count is full.
	ruleReasons = map[string]bool{
		"":                              true,
		domain.ReasonVelocityExceeded:   true,
		domain.ReasonCardTestingPattern: true,
	}
)

// Applies reports whether the rule counts a request with this amount and MCC.
func (r VelocityRule) Applies(amountMinor int64, mcc string) bool {
	if r.Filter == nil {
		return true
	}

	if r.Filter.MaxAmountMinor > 0 && amountMinor > r.Filter.MaxAmountMinor {
		return false
	}

	return len(r.Filter.MCCs) == 0 || slices.Contains(r.Filter.MCCs, mcc)
}

// GetWindowStart is the oldest instant that still counts at now. Calendar windows
// start at local midnight (or the first of the month) in loc (D-026).
func (r VelocityRule) GetWindowStart(now time.Time, loc *time.Location) time.Time {
	local := now.In(loc)
	switch r.Window {
	case CalendarDay:
		return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	case CalendarMonth:
		return time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, loc)
	default:
		return now.Add(-r.rolling)
	}
}

// GetRetention is how long an entry can still count, so Redis can expire the keys.
func (r VelocityRule) GetRetention() time.Duration {
	switch r.Window {
	case CalendarDay:
		return hoursPerDay * time.Hour
	case CalendarMonth:
		return daysPerMonthMax * hoursPerDay * time.Hour
	default:
		return r.rolling
	}
}

func validateVelocity(rules []VelocityRule, errs map[string]string) {
	seen := map[string]bool{}
	for i := range rules {
		field := fmt.Sprintf(fieldVelocityItem, i)
		rule := &rules[i]

		if seen[rule.ID] {
			errs[field+".id"] = msgDuplicateID
		}

		seen[rule.ID] = true
		validateRule(rule, field, errs)
	}
}

func validateRule(r *VelocityRule, field string, errs map[string]string) {
	if !ruleIDRe.MatchString(r.ID) {
		errs[field+".id"] = msgRuleID
	}

	if r.Scope != "" && r.Scope != ScopeCard {
		errs[field+".scope"] = msgScope
	}

	validateWindow(r, field, errs)

	if r.MaxCount == nil && r.MaxAmount == nil {
		errs[field] = msgNeedsLimit
	}

	if r.MaxCount != nil && *r.MaxCount <= 0 {
		errs[field+".max_count"] = domain.MsgPositive
	}

	if !ruleReasons[r.Reason] {
		errs[field+".reason"] = msgReason
	}

	validateMoney(r.MaxAmount, field+".max_amount", errs)
	validateFilter(r.Filter, field+".filter", errs)
}

// validateWindow also resolves a rolling window to a duration.
func validateWindow(r *VelocityRule, field string, errs map[string]string) {
	if r.Window == CalendarDay || r.Window == CalendarMonth {
		return
	}

	duration, err := time.ParseDuration(r.Window)
	if err != nil || duration <= 0 || duration > maxRollingWindow {
		errs[field+".window"] = msgWindow
		return
	}

	r.rolling = duration
}

func validateFilter(f *VelocityFilter, field string, errs map[string]string) {
	if f == nil {
		return
	}

	if f.MaxAmountMinor < 0 {
		errs[field+".max_amount_minor"] = msgNotNegative
	}

	validateEach(f.MCCs, field+".mccs", mccRe.MatchString, domain.MsgMCC, errs)
}
