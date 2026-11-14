package policy

import (
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const fullPolicy = `{
  "company_id": "acme-mx",
  "version": 1,
  "timezone": "America/Mexico_City",
  "controls": {
    "blocked_mccs": ["7995", "5933"],
    "allowed_countries": ["MX", "US", "CO"],
    "max_per_transaction": { "amount_minor": 50000, "currency": "USD" },
    "allowed_entry_modes": ["chip", "contactless", "ecommerce", "wallet"],
    "allowed_hours": { "start": "06:00", "end": "23:00" }
  },
  "velocity": [{ "id": "card-testing", "scope": "card", "window": "10m", "max_count": 5 }],
  "risk": { "flag_at": 40, "decline_at": 70 },
  "fallback": { "mode": "fail_open_limited", "max_amount_minor": 20000 }
}`

func newValidDoc(t *testing.T) Document {
	t.Helper()
	doc, err := Parse([]byte(fullPolicy))
	require.NoError(t, err)
	return doc
}

func parseClock(clock string) time.Time {
	tm, _ := time.Parse("15:04", clock)
	return tm
}

func TestParseAcceptsTheREADMEPolicy(t *testing.T) {
	c := require.New(t)

	doc := newValidDoc(t)

	c.Empty(doc.Validate())
	c.Equal("acme-mx", doc.CompanyID)
	c.Equal(&Money{AmountMinor: 50000, Currency: "USD"}, doc.Controls.MaxPerTransaction)
	c.Len(doc.Velocity, 1)
	c.Equal("card-testing", doc.Velocity[0].ID)
}

func TestParseRejectsUnknownFields(t *testing.T) {
	c := require.New(t)

	_, err := Parse([]byte(`{"controls": {"blocked_mcc": ["7995"]}}`))

	c.ErrorContains(err, "blocked_mcc")
}

func TestParseRejectsInvalidJSON(t *testing.T) {
	c := require.New(t)

	_, err := Parse([]byte(`{"controls":`))

	c.ErrorIs(err, io.ErrUnexpectedEOF)
}

func TestValidateRequiresTheCompanyID(t *testing.T) {
	c := require.New(t)
	doc := newValidDoc(t)
	doc.CompanyID = ""

	c.Equal(map[string]string{"company_id": "required"}, doc.Validate())
}

func TestValidateRejectsAnUnknownTimezone(t *testing.T) {
	c := require.New(t)
	doc := newValidDoc(t)
	doc.Timezone = "Mars/Olympus"

	c.Equal(map[string]string{"timezone": "unknown IANA time zone"}, doc.Validate())
}

func TestValidateRejectsAnInvalidBlockedMCC(t *testing.T) {
	c := require.New(t)
	doc := newValidDoc(t)
	doc.Controls.BlockedMCCs = []string{"7995", "79A5"}

	c.Equal(map[string]string{"controls.blocked_mccs[1]": "must be 4 digits"}, doc.Validate())
}

func TestValidateRejectsAnInvalidCountry(t *testing.T) {
	c := require.New(t)
	doc := newValidDoc(t)
	doc.Controls.AllowedCountries = []string{"mx"}

	c.Equal(map[string]string{"controls.allowed_countries[0]": "must be a 2-letter uppercase ISO code"}, doc.Validate())
}

func TestValidateRejectsAnUnknownEntryMode(t *testing.T) {
	c := require.New(t)
	doc := newValidDoc(t)
	doc.Controls.AllowedEntryModes = []string{"chip", "telepathy"}

	c.Equal(map[string]string{"controls.allowed_entry_modes[1]": "unknown entry mode"}, doc.Validate())
}

func TestValidateRejectsANonPositiveTransactionLimit(t *testing.T) {
	c := require.New(t)
	doc := newValidDoc(t)
	doc.Controls.MaxPerTransaction = &Money{AmountMinor: 0, Currency: "usd"}

	c.Equal(map[string]string{
		"controls.max_per_transaction.amount_minor": "must be greater than 0",
		"controls.max_per_transaction.currency":     "must be a 3-letter uppercase ISO code",
	}, doc.Validate())
}

func TestValidateRejectsABadStartHour(t *testing.T) {
	c := require.New(t)
	doc := newValidDoc(t)
	doc.Controls.AllowedHours = &AllowedHours{Start: "24:00", End: "06:00"}

	c.Equal(map[string]string{"controls.allowed_hours.start": "must be HH:MM (24h)"}, doc.Validate())
}

func TestValidateRejectsABadEndHour(t *testing.T) {
	c := require.New(t)
	doc := newValidDoc(t)
	doc.Controls.AllowedHours = &AllowedHours{Start: "06:00", End: "6pm"}

	c.Equal(map[string]string{"controls.allowed_hours.end": "must be HH:MM (24h)"}, doc.Validate())
}

func TestValidateRejectsAnEmptyHoursWindow(t *testing.T) {
	c := require.New(t)
	doc := newValidDoc(t)
	doc.Controls.AllowedHours = &AllowedHours{Start: "06:00", End: "06:00"}

	c.Equal(map[string]string{"controls.allowed_hours": "start and end must differ"}, doc.Validate())
}

func TestValidateRejectsUnorderedRiskThresholds(t *testing.T) {
	c := require.New(t)
	doc := newValidDoc(t)
	doc.Risk = &Risk{FlagAt: 70, DeclineAt: 40}

	c.Equal(map[string]string{"risk": "need 0 < flag_at < decline_at <= 100"}, doc.Validate())
}

func TestValidateRejectsAnUnknownFallbackMode(t *testing.T) {
	c := require.New(t)
	doc := newValidDoc(t)
	doc.Fallback = &Fallback{Mode: "fail_open", MaxAmountMinor: -1}

	c.Equal(map[string]string{
		"fallback.mode":             "must be fail_closed or fail_open_limited",
		"fallback.max_amount_minor": "must not be negative",
	}, doc.Validate())
}

func TestValidateAcceptsAMinimalPolicy(t *testing.T) {
	c := require.New(t)
	doc := Document{CompanyID: "acme-mx"}

	c.Empty(doc.Validate())
	c.Equal(time.UTC, doc.GetLocation())
}

func TestLocationIsTheCompanyTimezone(t *testing.T) {
	c := require.New(t)
	doc := newValidDoc(t)

	c.Empty(doc.Validate())

	c.Equal("America/Mexico_City", doc.GetLocation().String())
}

func TestLocationDefaultsToUTCBeforeValidation(t *testing.T) {
	c := require.New(t)

	c.Equal(time.UTC, Document{}.GetLocation())
}

func TestLoadReturnsAValidatedDocument(t *testing.T) {
	c := require.New(t)

	doc, err := Load([]byte(fullPolicy))

	c.NoError(err)
	c.Equal("America/Mexico_City", doc.GetLocation().String())
	c.True(doc.Controls.AllowedHours.Contains(parseClock("12:00")))
}

func TestLoadToleratesUnknownFields(t *testing.T) {
	c := require.New(t)

	doc, err := Load([]byte(`{"company_id":"acme-mx","version":2,"added_later":true}`))

	c.NoError(err)
	c.Equal(2, doc.Version)
}

func TestLoadRejectsAnInvalidStoredDocument(t *testing.T) {
	c := require.New(t)

	_, err := Load([]byte(`{"company_id":"acme-mx","timezone":"Mars/Olympus"}`))

	c.ErrorContains(err, "stored policy acme-mx")
}

func TestLoadRejectsInvalidJSON(t *testing.T) {
	c := require.New(t)

	_, err := Load([]byte(`not json`))

	c.ErrorContains(err, "decode policy")
}

func TestAllowedHoursDaytimeWindow(t *testing.T) {
	c := require.New(t)
	doc := Document{CompanyID: "acme-mx", Controls: Controls{AllowedHours: &AllowedHours{Start: "06:00", End: "23:00"}}}
	c.Empty(doc.Validate())
	hours := doc.Controls.AllowedHours

	c.False(hours.Contains(parseClock("05:59")))
	c.True(hours.Contains(parseClock("06:00")))
	c.True(hours.Contains(parseClock("22:59")))
	c.False(hours.Contains(parseClock("23:00")), "end is exclusive")
}

func TestAllowedHoursWindowCrossingMidnight(t *testing.T) {
	c := require.New(t)
	doc := Document{CompanyID: "acme-mx", Controls: Controls{AllowedHours: &AllowedHours{Start: "22:00", End: "06:00"}}}
	c.Empty(doc.Validate())
	hours := doc.Controls.AllowedHours

	c.True(hours.Contains(parseClock("23:30")))
	c.True(hours.Contains(parseClock("05:59")))
	c.False(hours.Contains(parseClock("06:00")))
	c.False(hours.Contains(parseClock("12:00")))
}

func TestEffectiveWithoutOverridesReturnsTheCompanyControls(t *testing.T) {
	c := require.New(t)
	company := newValidDoc(t).Controls

	got, err := MergeOverrides(company, nil)

	c.NoError(err)
	c.Equal(company, got)
}

func TestEffectiveTreatsJSONNullAsNoOverrides(t *testing.T) {
	c := require.New(t)
	company := newValidDoc(t).Controls

	got, err := MergeOverrides(company, json.RawMessage("null"))

	c.NoError(err)
	c.Equal(company, got)
}

func TestEffectiveReplacesOnlyTheOverriddenFields(t *testing.T) {
	c := require.New(t)
	company := newValidDoc(t).Controls

	got, err := MergeOverrides(company, json.RawMessage(`{"max_per_transaction": {"amount_minor": 100000, "currency": "USD"}}`))

	c.NoError(err)
	c.Equal(&Money{AmountMinor: 100000, Currency: "USD"}, got.MaxPerTransaction)
	c.Equal(company.BlockedMCCs, got.BlockedMCCs)
	c.Equal(company.AllowedCountries, got.AllowedCountries)
	c.Equal(&Money{AmountMinor: 50000, Currency: "USD"}, company.MaxPerTransaction, "company controls must not change")
}

func TestEffectiveEmptyListRemovesTheRestriction(t *testing.T) {
	c := require.New(t)
	company := newValidDoc(t).Controls

	got, err := MergeOverrides(company, json.RawMessage(`{"allowed_countries": []}`))

	c.NoError(err)
	c.NotNil(got.AllowedCountries)
	c.Empty(got.AllowedCountries)
}

func TestEffectiveOverridesEveryField(t *testing.T) {
	c := require.New(t)
	company := newValidDoc(t).Controls

	got, err := MergeOverrides(company, json.RawMessage(`{
		"blocked_mccs": ["4829"], "allowed_countries": ["CO"],
		"allowed_entry_modes": ["chip"], "allowed_hours": {"start": "00:00", "end": "12:00"}}`))

	c.NoError(err)
	c.Equal([]string{"4829"}, got.BlockedMCCs)
	c.Equal([]string{"CO"}, got.AllowedCountries)
	c.Equal([]string{"chip"}, got.AllowedEntryModes)
	c.True(got.AllowedHours.Contains(parseClock("03:00")))
	c.False(got.AllowedHours.Contains(parseClock("13:00")))
}

func TestEffectiveRejectsUnknownOverrideFields(t *testing.T) {
	c := require.New(t)

	_, err := MergeOverrides(Controls{}, json.RawMessage(`{"max_per_txn": 1}`))

	c.ErrorContains(err, "decode card overrides")
}

func TestEffectiveRejectsInvalidOverrideValues(t *testing.T) {
	c := require.New(t)

	_, err := MergeOverrides(Controls{}, json.RawMessage(`{"blocked_mccs": ["abc"]}`))

	c.ErrorContains(err, "overrides.blocked_mccs[0]")
}
