package dyl

import (
	"math"
	"testing"
)

// baseUnitsPerCoin must stay equal to 10^Precision.
func TestBaseUnitsMatchPrecision(t *testing.T) {
	want := uint64(math.Pow10(Precision))
	if baseUnitsPerCoin != want {
		t.Errorf("baseUnitsPerCoin = %d, want 10^%d = %d", baseUnitsPerCoin, Precision, want)
	}
}

func TestFormatAmount(t *testing.T) {
	cases := []struct {
		base uint64
		want string
	}{
		{0, "0 DYL"},
		{baseUnitsPerCoin, "1 DYL"},
		{12_500_000, "12.5 DYL"},
		{1, "0.000001 DYL"},
		{1_000_001, "1.000001 DYL"},
		{2_100_000, "2.1 DYL"},
	}

	for _, c := range cases {
		if got := FormatAmount(c.base); got != c.want {
			t.Errorf("FormatAmount(%d) = %q, want %q", c.base, got, c.want)
		}
	}
}
