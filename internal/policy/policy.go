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

var (
	mccRe      = regexp.MustCompile(`^[0-9]{4}$`)
	countryRe  = regexp.MustCompile(`^[A-Z]{2}$`)
	currencyRe = regexp.MustCompile(`^[A-Z]{3}$`)
	clockRe    = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

	fallbackModes = map[string]bool{"fail_closed": true, "fail_open_limited": true}
)

// Parse decodes a document sent by a client. Unknown fields are rejected, so a
// typo like "blocked_mcc" fails loudly instead of silently disabling a control.
// Call Validate afterwards.
func Parse(data []byte) (Document, error) {
	var d Document
	if err := decodeStrict(data, &d); err != nil {
		return Document{}, err
	}
	return d, nil
}

// Load decodes and validates a document read back from storage.
func Load(data []byte) (Document, error) {
	var d Document
	if err := json.Unmarshal(data, &d); err != nil {
		return Document{}, fmt.Errorf("decode policy: %w", err)
	}
	if errs := d.Validate(); len(errs) > 0 {
		return Document{}, fmt.Errorf("stored policy %s v%d is invalid: %v", d.CompanyID, d.Version, errs)
	}
	return d, nil
}

// Validate returns field → problem. When it returns no errors the document is
// ready to use (time zone and hours are resolved).
func (d *Document) Validate() map[string]string {
	errs := map[string]string{}
	if d.CompanyID == "" {
		errs["company_id"] = "required"
	}

	loc, err := time.LoadLocation(d.Timezone)
	if err != nil {
		errs["timezone"] = "unknown IANA time zone"
	}
	d.loc = loc

	validateControls(&d.Controls, "controls.", errs)

	if r := d.Risk; r != nil && !(0 < r.FlagAt && r.FlagAt < r.DeclineAt && r.DeclineAt <= 100) {
		errs["risk"] = "need 0 < flag_at < decline_at <= 100"
	}
	if f := d.Fallback; f != nil {
		if !fallbackModes[f.Mode] {
			errs["fallback.mode"] = "must be fail_closed or fail_open_limited"
		}
		if f.MaxAmountMinor < 0 {
			errs["fallback.max_amount_minor"] = "must not be negative"
		}
	}
	return errs
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
	if len(overrides) == 0 || string(overrides) == "null" {
		return company, nil
	}
	var o Controls
	if err := decodeStrict(overrides, &o); err != nil {
		return Controls{}, fmt.Errorf("decode card overrides: %w", err)
	}
	errs := map[string]string{}
	validateControls(&o, "overrides.", errs)
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
	m := t.Hour()*60 + t.Minute()
	if h.start < h.end {
		return h.start <= m && m < h.end
	}
	return m >= h.start || m < h.end
}

func validateControls(c *Controls, prefix string, errs map[string]string) {
	for i, mcc := range c.BlockedMCCs {
		if !mccRe.MatchString(mcc) {
			errs[fmt.Sprintf("%sblocked_mccs[%d]", prefix, i)] = "must be 4 digits"
		}
	}
	for i, cc := range c.AllowedCountries {
		if !countryRe.MatchString(cc) {
			errs[fmt.Sprintf("%sallowed_countries[%d]", prefix, i)] = "must be a 2-letter uppercase ISO code"
		}
	}
	for i, mode := range c.AllowedEntryModes {
		if !domain.EntryModes[mode] {
			errs[fmt.Sprintf("%sallowed_entry_modes[%d]", prefix, i)] = "unknown entry mode"
		}
	}
	if m := c.MaxPerTransaction; m != nil {
		if m.AmountMinor <= 0 {
			errs[prefix+"max_per_transaction.amount_minor"] = "must be greater than 0"
		}
		if !currencyRe.MatchString(m.Currency) {
			errs[prefix+"max_per_transaction.currency"] = "must be a 3-letter uppercase ISO code"
		}
	}
	if h := c.AllowedHours; h != nil {
		start, okStart := minutes(h.Start)
		end, okEnd := minutes(h.End)
		switch {
		case !okStart:
			errs[prefix+"allowed_hours.start"] = "must be HH:MM (24h)"
		case !okEnd:
			errs[prefix+"allowed_hours.end"] = "must be HH:MM (24h)"
		case start == end:
			errs[prefix+"allowed_hours"] = "start and end must differ"
		default:
			h.start, h.end = start, end
		}
	}
}

func minutes(clock string) (int, bool) {
	if !clockRe.MatchString(clock) {
		return 0, false
	}
	h, _ := strconv.Atoi(clock[:2])
	m, _ := strconv.Atoi(clock[3:])
	return h*60 + m, true
}

func decodeStrict(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}
