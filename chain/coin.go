package chain

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

	// BaseUnitsPerCoin is 10^Precision, the number of BaseDenom in one
	// DisplayDenom. Use it to write an amount in whole coins. Kept as a
	// literal and checked against Precision in the tests.
	BaseUnitsPerCoin = 1_000_000

	// BlockReward is the amount minted to a block's proposer when the block
	// is applied, in BaseDenom. Genesis and AddBlock blocks carry no
	// proposer and mint nothing.
	//
	// It is deliberately generous. This chain has no value and the demo is
	// more interesting when supply visibly moves and a delegator's share of
	// a block is large enough to watch. Nothing here is a claim about a
	// sensible issuance schedule.
	BlockReward = 100 * BaseUnitsPerCoin
)

// FormatAmount renders a base-unit amount as a DisplayDenom string, e.g.
// 12_500_000 becomes "12.5 DYL". Trailing zeros in the fraction are
// trimmed, and a whole amount omits the fraction entirely.
func FormatAmount(base uint64) string {
	whole := base / BaseUnitsPerCoin
	frac := base % BaseUnitsPerCoin
	if frac == 0 {
		return fmt.Sprintf("%d %s", whole, DisplayDenom)
	}
	fracStr := strings.TrimRight(fmt.Sprintf("%0*d", Precision, frac), "0")
	return fmt.Sprintf("%d.%s %s", whole, fracStr, DisplayDenom)
}
