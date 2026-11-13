// Package policy holds each company's versioned policy document (D-004): the
// hard controls, velocity rules, risk thresholds and fallback settings.
package policy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"time"
	_ "time/tzdata" // policies name IANA time zones; don't depend on the host's tz database

	"github.com/bescobarm/card-auth-engine/internal/domain"
)

// Document is one published version of a company policy. Version is assigned by
// the server when the document is published; it is never updated in place.
type Document struct {
	CompanyID string          `json:"company_id"`
	Version   int             `json:"version"`
	Timezone  string          `json:"timezone,omitempty"` // D-026; empty means UTC
	Controls  Controls        `json:"controls"`
	Velocity  json.RawMessage `json:"velocity,omitempty"` // typed and validated in SP03
	Risk      *Risk           `json:"risk,omitempty"`
	Fallback  *Fallback       `json:"fallback,omitempty"`

	loc *time.Location
}

// Controls are the hard controls. An empty list or nil field means "no restriction".
type Controls struct {
	BlockedMCCs       []string      `json:"blocked_mccs,omitempty"`
	AllowedCountries  []string      `json:"allowed_countries,omitempty"`
	MaxPerTransaction *Money        `json:"max_per_transaction,omitempty"`
	AllowedEntryModes []string      `json:"allowed_entry_modes,omitempty"`
	AllowedHours      *AllowedHours `json:"allowed_hours,omitempty"` // D-038: hard control
}

type Money struct {
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
}

// AllowedHours is a daily local-time window, [Start, End). When End is before
// Start the window crosses midnight, e.g. 22:00–06:00 (D-040).
type AllowedHours struct {
	Start string `json:"start"`
	End   string `json:"end"`

	start, end int // minutes after midnight
}

type Risk struct {
	FlagAt    int `json:"flag_at"`
	DeclineAt int `json:"decline_at"`
}

type Fallback struct {
	Mode           string `json:"mode"`
	MaxAmountMinor int64  `json:"max_amount_minor,omitempty"`
}

const (
	minutesPerHour   = 60
	maxRiskScore     = 100
	clockHourEnd     = 2 // "HH" in "HH:MM"
	clockMinuteStart = 3 // "MM" in "HH:MM"
	jsonNull         = "null"

	prefixControls  = "controls."
	prefixOverrides = "overrides."

	msgUnknownTimezone = "unknown IANA time zone"
	msgRiskOrder       = "need 0 < flag_at < decline_at <= 100"
	msgFallbackMode    = "must be " + FailClosed + " or " + FailOpenLimited
	msgNotNegative     = "must not be negative"
	msgClock           = "must be HH:MM (24h)"
	msgHoursMustDiffer = "start and end must differ"
)

// Fallback modes (D-006).
const (
	FailClosed      = "fail_closed"
	FailOpenLimited = "fail_open_limited"
)

var (
	mccRe      = regexp.MustCompile(`^[0-9]{4}$`)
	countryRe  = regexp.MustCompile(`^[A-Z]{2}$`)
	currencyRe = regexp.MustCompile(`^[A-Z]{3}$`)
	clockRe    = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

	fallbackModes = map[string]bool{FailClosed: true, FailOpenLimited: true}
)

// Parse decodes a document sent by a client. Unknown fields are rejected, so a
// typo like "blocked_mcc" fails loudly instead of silently disabling a control.
// Call Validate afterwards.
func Parse(data []byte) (Document, error) {
	var d Document
	err := decodeStrict(data, &d)
	if err != nil {
		return Document{}, err
	}

	return d, nil
}

// Load decodes and validates a document read back from storage.
func Load(data []byte) (Document, error) {
	var d Document
	err := json.Unmarshal(data, &d)
	if err != nil {
		return Document{}, fmt.Errorf("decode policy: %w", err)
	}

	errs := d.Validate()
	if len(errs) > 0 {
		return Document{}, fmt.Errorf("stored policy %s v%d is invalid: %v", d.CompanyID, d.Version, errs)
	}

	return d, nil
}

// Validate returns field → problem. When it returns no errors the document is
// ready to use (time zone and hours are resolved).
func (d *Document) Validate() map[string]string {
	errs := map[string]string{}
	if d.CompanyID == "" {
		errs["company_id"] = domain.MsgRequired
	}

	loc, err := time.LoadLocation(d.Timezone)
	if err != nil {
		errs["timezone"] = msgUnknownTimezone
	}

	d.loc = loc

	validateControls(&d.Controls, prefixControls, errs)
	validateRisk(d.Risk, errs)
	validateFallback(d.Fallback, errs)

	return errs
}

func validateRisk(r *Risk, errs map[string]string) {
	if r == nil {
		return
	}

	ordered := 0 < r.FlagAt && r.FlagAt < r.DeclineAt && r.DeclineAt <= maxRiskScore
	if !ordered {
		errs["risk"] = msgRiskOrder
	}
}

func validateFallback(f *Fallback, errs map[string]string) {
	if f == nil {
		return
	}

	if !fallbackModes[f.Mode] {
		errs["fallback.mode"] = msgFallbackMode
	}

	if f.MaxAmountMinor < 0 {
		errs["fallback.max_amount_minor"] = msgNotNegative
	}
}

// Location is the company's time zone, UTC by default.
func (d Document) Location() *time.Location {
	if d.loc == nil {
		return time.UTC
	}

	return d.loc
}

// Effective merges card-level overrides into the company controls, field by
// field: any field present in the overrides replaces the company value, and an
// explicit empty list removes the restriction for that card.
func Effective(company Controls, overrides json.RawMessage) (Controls, error) {
	if len(overrides) == 0 || string(overrides) == jsonNull {
		return company, nil
	}

	var o Controls
	err := decodeStrict(overrides, &o)
	if err != nil {
		return Controls{}, fmt.Errorf("decode card overrides: %w", err)
	}

	errs := map[string]string{}
	validateControls(&o, prefixOverrides, errs)
	if len(errs) > 0 {
		return Controls{}, fmt.Errorf("invalid card overrides: %v", errs)
	}

	out := company
	if o.BlockedMCCs != nil {
		out.BlockedMCCs = o.BlockedMCCs
	}

	if o.AllowedCountries != nil {
		out.AllowedCountries = o.AllowedCountries
	}

	if o.MaxPerTransaction != nil {
		out.MaxPerTransaction = o.MaxPerTransaction
	}

	if o.AllowedEntryModes != nil {
		out.AllowedEntryModes = o.AllowedEntryModes
	}

	if o.AllowedHours != nil {
		out.AllowedHours = o.AllowedHours
	}

	return out, nil
}

// Contains reports whether t, already in the company's time zone, falls inside the window.
func (h AllowedHours) Contains(t time.Time) bool {
	m := t.Hour()*minutesPerHour + t.Minute()
	if h.start < h.end {
		return h.start <= m && m < h.end
	}

	return m >= h.start || m < h.end
}

func validateControls(c *Controls, prefix string, errs map[string]string) {
	validateEach(c.BlockedMCCs, prefix+"blocked_mccs", mccRe.MatchString, domain.MsgMCC, errs)
	validateEach(c.AllowedCountries, prefix+"allowed_countries", countryRe.MatchString, domain.MsgCountry, errs)
	validateEach(c.AllowedEntryModes, prefix+"allowed_entry_modes", isEntryMode, domain.MsgEntryMode, errs)
	validateMoney(c.MaxPerTransaction, prefix+"max_per_transaction", errs)
	validateHours(c.AllowedHours, prefix+"allowed_hours", errs)
}

// validateEach reports every value in list that ok rejects, as field[i] → msg.
func validateEach(list []string, field string, ok func(string) bool, msg string, errs map[string]string) {
	for i, v := range list {
		if !ok(v) {
			errs[fmt.Sprintf("%s[%d]", field, i)] = msg
		}
	}
}

func isEntryMode(mode string) bool {
	return domain.EntryModes[mode]
}

func validateMoney(m *Money, field string, errs map[string]string) {
	if m == nil {
		return
	}

	if m.AmountMinor <= 0 {
		errs[field+".amount_minor"] = domain.MsgPositive
	}

	if !currencyRe.MatchString(m.Currency) {
		errs[field+".currency"] = domain.MsgCurrency
	}
}

// validateHours also resolves the window to minutes, so Contains can use it.
func validateHours(h *AllowedHours, field string, errs map[string]string) {
	if h == nil {
		return
	}

	start, okStart := minutes(h.Start)
	end, okEnd := minutes(h.End)

	switch {
	case !okStart:
		errs[field+".start"] = msgClock
	case !okEnd:
		errs[field+".end"] = msgClock
	case start == end:
		errs[field] = msgHoursMustDiffer
	default:
		h.start, h.end = start, end
	}
}

func minutes(clock string) (int, bool) {
	if !clockRe.MatchString(clock) {
		return 0, false
	}

	h, _ := strconv.Atoi(clock[:clockHourEnd])
	m, _ := strconv.Atoi(clock[clockMinuteStart:])
	return h*minutesPerHour + m, true
}

func decodeStrict(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}
