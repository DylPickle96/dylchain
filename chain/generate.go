package chain

import (
	"crypto/ed25519"
	"fmt"
	"hash/fnv"
)

// monikers are display names for generated validators, in rank order. None
// is a real operator. Past the end of the list a validator gets "val-NNN".
var monikers = []string{
	"Northwind Staking", "Lumen Labs", "Halcyon Node", "Meridian Validation",
	"Foxglove", "Stakehaus", "Orbital Systems", "Tidewater",
	"Blue Ridge Nodes", "Kestrel", "Sable Infrastructure", "Ironclad Validators",
	"Saltmarsh Collective", "Aurora Stake", "Copperline", "Driftwood DAO",
	"Pine & Pixel", "Quartz Capital", "Vanta Node", "Riverbend",
	"Nightjar", "Cinder Labs", "Hollow Oak", "Gravity Well",
	"Peregrine Staking", "Basalt", "Moonrake", "Solstice Systems",
	"Tallgrass", "Umbra Validation", "Wren & Co", "Zephyr Nodes",
	"Cobalt Harbor", "Larkspur", "Anvil Point", "Mistral Infra",
	"Juniper Stake", "Ember Collective", "Sequoia Node", "Glasswing",
	"Bramble", "Ridgeway Validators", "Nimbus Ops", "Onyx Stake",
	"Harbor Light", "Pale Fire", "Thistle", "Ironwood",
}

const (
	// topStake is rank 0's stake in udyl: 2.4 million DYL.
	topStake = 2_400_000 * baseUnitsPerCoin
	// floorStake is the smallest generated stake: 5,000 DYL.
	floorStake = 5_000 * baseUnitsPerCoin
)

// GenerateValidators makes n validators with fresh keys, a named moniker,
// and a skewed stake distribution: each rank holds roughly 88% of the one
// above it, with a little deterministic jitter so the numbers do not look
// machine-made. Stakes stay strictly decreasing by rank, so rank order and
// stake order agree. It is for demos and load tests, where hand-building a
// large set is impractical.
func GenerateValidators(n int) []Validator {
	out := make([]Validator, n)
	for i := 0; i < n; i++ {
		_, priv, err := ed25519.GenerateKey(nil)
		if err != nil {
			panic(err) // crypto/rand failure
		}
		v := NewValidator(priv, generatedStake(i))
		if i < len(monikers) {
			v.moniker = monikers[i]
		} else {
			v.moniker = fmt.Sprintf("val-%03d", i)
		}
		out[i] = v
	}
	return out
}

// generatedStake is rank i's stake: a geometric decay from topStake with a
// jitter of up to 4% either way, seeded from the rank so it is stable
// across runs. The 12% step between ranks is larger than twice the jitter,
// which keeps the sequence strictly decreasing.
func generatedStake(rank int) uint64 {
	stake := float64(topStake)
	for j := 0; j < rank; j++ {
		stake *= 0.88
	}
	h := fnv.New32a()
	fmt.Fprintf(h, "stake-%d", rank)
	jitter := (float64(h.Sum32()%8001)/8000.0)*0.08 - 0.04
	stake *= 1 + jitter
	if stake < floorStake {
		return floorStake
	}
	// round to whole DYL so the display is tidy
	return uint64(stake/baseUnitsPerCoin) * baseUnitsPerCoin
}
