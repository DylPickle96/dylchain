package chain

import (
	"crypto/ed25519"
	"fmt"
)

// GenerateValidators makes n validators with fresh keys and a skewed stake
// distribution: rank 0 holds the most, and stake halves every eight ranks
// down to a floor of 1000. Each gets a "val-NNN" moniker. It is for demos
// and load tests, where hand-building a large set is impractical.
func GenerateValidators(n int) []Validator {
	out := make([]Validator, n)
	for i := 0; i < n; i++ {
		_, priv, err := ed25519.GenerateKey(nil)
		if err != nil {
			panic(err) // crypto/rand failure
		}
		stake := uint64(1_000_000)
		for j := 0; j < i/8; j++ {
			stake /= 2
			if stake < 1_000 {
				stake = 1_000
				break
			}
		}
		v := NewValidator(priv, stake)
		v.moniker = fmt.Sprintf("val-%03d", i)
		out[i] = v
	}
	return out
}
