// Package domain holds the types shared by every stage of the engine.
// Amounts are always int64 minor units, never floats (D-007).
package domain

import (
	"encoding/json"
	"errors"
	"regexp"
	"time"
)

// ErrNotFound is returned by stores when a record does not exist.
var ErrNotFound = errors.New("not found")

// ErrConflict is returned when a write loses a race with another write.
var ErrConflict = errors.New("conflict")

type Decision string

const (
	Approve Decision = "APPROVE"
	Decline Decision = "DECLINE"
)

// Reason codes returned to the processor and stored in the audit log.
const (
	ReasonCardInactive         = "CARD_INACTIVE"
	ReasonEntryModeNotAllowed  = "ENTRY_MODE_NOT_ALLOWED"
	ReasonMCCBlocked           = "MCC_BLOCKED"
	ReasonCountryNotAllowed    = "COUNTRY_NOT_ALLOWED"
	ReasonOverTransactionLimit = "OVER_TRANSACTION_LIMIT"
	ReasonOutsideAllowedHours  = "OUTSIDE_ALLOWED_HOURS"
)

type Reason struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Merchant struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	MCC     string `json:"mcc"`
	Country string `json:"country"`
}

// AuthRequest is the real-time authorization webhook payload, modeled on
// Stripe Issuing and Lithic ASA.
type AuthRequest struct {
	AuthorizationID      string    `json:"authorization_id"`
	CardID               string    `json:"card_id"`
	AmountMinor          int64     `json:"amount_minor"`
	Currency             string    `json:"currency"`
	Merchant             Merchant  `json:"merchant"`
	EntryMode            string    `json:"entry_mode"`
	IsAmountControllable bool      `json:"is_amount_controllable"`
	NetworkRiskScore     *int      `json:"network_risk_score,omitempty"`
	CreatedAt            time.Time `json:"created_at"`
}

// AuthResponse is what the engine answers. It is stored as-is and returned
// unchanged when the same authorization_id is replayed.
type AuthResponse struct {
	AuthorizationID     string   `json:"authorization_id"`
	Decision            Decision `json:"decision"`
	ApprovedAmountMinor int64    `json:"approved_amount_minor"`
	RiskScore           int      `json:"risk_score"`
	Reasons             []Reason `json:"reasons"`
	PolicyVersion       int      `json:"policy_version"`
	FallbackUsed        bool     `json:"fallback_used"`
	LatencyMS           float64  `json:"latency_ms"`
}

type CardStatus string

const (
	CardActive     CardStatus = "active"
	CardFrozen     CardStatus = "frozen"
	CardTerminated CardStatus = "terminated"
)

type Card struct {
	ID           string
	CompanyID    string
	CardholderID string
	Status       CardStatus
	Overrides    json.RawMessage // card-level controls that replace the company's, field by field
}

// AuditRecord is the full stored record of one authorization.
type AuditRecord struct {
	AuthorizationID string          `json:"authorization_id"`
	Status          string          `json:"status"`
	Request         json.RawMessage `json:"request"`
	Response        json.RawMessage `json:"response"`
	Explanation     json.RawMessage `json:"explanation,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
}

var (
	currencyRe = regexp.MustCompile(`^[A-Z]{3}$`)
	mccRe      = regexp.MustCompile(`^[0-9]{4}$`)
	countryRe  = regexp.MustCompile(`^[A-Z]{2}$`)
)

// EntryModes lists the accepted values of entry_mode.
var EntryModes = map[string]bool{
	"chip": true, "contactless": true, "ecommerce": true,
	"manual": true, "wallet": true, "swipe": true,
}

// Validate returns a map of field → problem. An empty map means the request is valid.
// A malformed request is rejected with HTTP 400; it is never turned into a decision.
func (r AuthRequest) Validate() map[string]string {
	errs := map[string]string{}
	if r.AuthorizationID == "" {
		errs["authorization_id"] = "required"
	}
	if r.CardID == "" {
		errs["card_id"] = "required"
	}
	if r.AmountMinor <= 0 {
		errs["amount_minor"] = "must be greater than 0"
	}
	if !currencyRe.MatchString(r.Currency) {
		errs["currency"] = "must be a 3-letter uppercase ISO code"
	}
	if !mccRe.MatchString(r.Merchant.MCC) {
		errs["merchant.mcc"] = "must be 4 digits"
	}
	if !countryRe.MatchString(r.Merchant.Country) {
		errs["merchant.country"] = "must be a 2-letter uppercase ISO code"
	}
	if !EntryModes[r.EntryMode] {
		errs["entry_mode"] = "unknown entry mode"
	}
	return errs
}
