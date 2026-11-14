package domain

import (
	"fmt"
	"strings"
)

const (
	minorPerUnit   = 100
	digitsPerGroup = 3
	signNegative   = "-"
)

// FormatMoney writes minor units as "1,350.00 USD", so people never read
// cents as whole units in a reason or an explanation.
func FormatMoney(amountMinor int64, currency string) string {
	sign := ""

	if amountMinor < 0 {
		sign = signNegative
		amountMinor = -amountMinor
	}

	units, cents := amountMinor/minorPerUnit, amountMinor%minorPerUnit
	digits := fmt.Sprint(units)

	var grouped strings.Builder

	for index, digit := range digits {
		if index > 0 && (len(digits)-index)%digitsPerGroup == 0 {
			grouped.WriteByte(',')
		}

		grouped.WriteRune(digit)
	}

	return fmt.Sprintf("%s%s.%02d %s", sign, grouped.String(), cents, currency)
}
