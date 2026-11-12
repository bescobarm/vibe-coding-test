package domain

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func validRequest() AuthRequest {
	return AuthRequest{
		AuthorizationID: "auth_1", CardID: "card_123", AmountMinor: 4500, Currency: "USD",
		Merchant:  Merchant{ID: "m_1", MCC: "5734", Country: "US"},
		EntryMode: "chip",
	}
}

func TestValidateAcceptsAValidRequest(t *testing.T) {
	c := require.New(t)

	c.Empty(validRequest().Validate())
}

func TestValidateRejectsMissingAuthorizationID(t *testing.T) {
	c := require.New(t)
	r := validRequest()
	r.AuthorizationID = ""

	c.Equal(map[string]string{"authorization_id": "required"}, r.Validate())
}

func TestValidateRejectsMissingCardID(t *testing.T) {
	c := require.New(t)
	r := validRequest()
	r.CardID = ""

	c.Equal(map[string]string{"card_id": "required"}, r.Validate())
}

func TestValidateRejectsZeroAmount(t *testing.T) {
	c := require.New(t)
	r := validRequest()
	r.AmountMinor = 0

	c.Equal(map[string]string{"amount_minor": "must be greater than 0"}, r.Validate())
}

func TestValidateRejectsNegativeAmount(t *testing.T) {
	c := require.New(t)
	r := validRequest()
	r.AmountMinor = -1

	c.Equal(map[string]string{"amount_minor": "must be greater than 0"}, r.Validate())
}

func TestValidateRejectsLowercaseCurrency(t *testing.T) {
	c := require.New(t)
	r := validRequest()
	r.Currency = "usd"

	c.Equal(map[string]string{"currency": "must be a 3-letter uppercase ISO code"}, r.Validate())
}

func TestValidateRejectsMCCWithLetters(t *testing.T) {
	c := require.New(t)
	r := validRequest()
	r.Merchant.MCC = "57A4"

	c.Equal(map[string]string{"merchant.mcc": "must be 4 digits"}, r.Validate())
}

func TestValidateRejectsShortMCC(t *testing.T) {
	c := require.New(t)
	r := validRequest()
	r.Merchant.MCC = "573"

	c.Equal(map[string]string{"merchant.mcc": "must be 4 digits"}, r.Validate())
}

func TestValidateRejectsThreeLetterCountry(t *testing.T) {
	c := require.New(t)
	r := validRequest()
	r.Merchant.Country = "USA"

	c.Equal(map[string]string{"merchant.country": "must be a 2-letter uppercase ISO code"}, r.Validate())
}

func TestValidateRejectsUnknownEntryMode(t *testing.T) {
	c := require.New(t)
	r := validRequest()
	r.EntryMode = "telepathy"

	c.Equal(map[string]string{"entry_mode": "unknown entry mode"}, r.Validate())
}

func TestValidateReportsEveryInvalidField(t *testing.T) {
	c := require.New(t)

	errs := AuthRequest{}.Validate()

	c.Len(errs, 7)
}
