package dyl

import (
	"fmt"
	"strings"
)

const (
	// BaseDenom is the smallest unit of the native coin. Every balance and
	// transaction amount is a count of this unit.
	BaseDenom = "udyl"

	// DisplayDenom is the human-facing unit. One DisplayDenom is
	// 10^Precision BaseDenom.
	DisplayDenom = "DYL"

	// Precision is the number of decimal places between BaseDenom and
	// DisplayDenom.
	Precision = 6

	// baseUnitsPerCoin is 10^Precision. Kept as a literal and checked
	// against Precision in the tests.
	baseUnitsPerCoin = 1_000_000
)

// FormatAmount renders a base-unit amount as a DisplayDenom string, e.g.
// 12_500_000 becomes "12.5 DYL". Trailing zeros in the fraction are
// trimmed, and a whole amount omits the fraction entirely.
func FormatAmount(base uint64) string {
	whole := base / baseUnitsPerCoin
	frac := base % baseUnitsPerCoin
	if frac == 0 {
		return fmt.Sprintf("%d %s", whole, DisplayDenom)
	}
	fracStr := strings.TrimRight(fmt.Sprintf("%0*d", Precision, frac), "0")
	return fmt.Sprintf("%d.%s %s", whole, fracStr, DisplayDenom)
}
