package chain

import (
	"crypto/ed25519"
	"fmt"
	"hash/fnv"
)

// monikers are display names for generated validators, in rank order: the
// Greek pantheon, Olympians first, then Titans and older powers, so the
// largest stake goes to Zeus. None is a real operator. Past the end of the
// list a validator gets "val-NNN".
var monikers = []string{
	"Zeus", "Hera", "Poseidon", "Demeter",
	"Athena", "Apollo", "Artemis", "Ares",
	"Aphrodite", "Hephaestus", "Hermes", "Hestia",
	"Hades", "Dionysus", "Persephone", "Hecate",
	"Helios", "Selene", "Eos", "Nike",
	"Iris", "Nemesis", "Tyche", "Pan",
	"Eros", "Nyx", "Gaia", "Uranus",
	"Cronus", "Rhea", "Oceanus", "Tethys",
	"Hyperion", "Theia", "Coeus", "Phoebe",
	"Prometheus", "Atlas", "Eris", "Hypnos",
	"Thanatos", "Morpheus", "Boreas", "Zephyros",
	"Notos", "Triton", "Amphitrite", "Leto",
}

const (
	// topStake is rank 0's stake: 130,000 DYL. The 20-validator demo set
	// bonds about 1,000,000 DYL in total, roughly the circulating supply,
	// so a faucet-sized delegation is a visible fraction of a validator.
	topStake = 130_000 * BaseUnitsPerCoin
	// floorStake is the smallest generated stake: 300 DYL. It only bites
	// far down a large load-test set; the demo's 20 never reach it.
	floorStake = 300 * BaseUnitsPerCoin
)

// GenerateValidators makes n validators with fresh keys, a named moniker,
// and a skewed stake distribution: each rank holds roughly 88% of the one
// above it, with a little deterministic jitter so the numbers do not look
// machine-made. Stakes stay strictly decreasing by rank, so rank order and
// stake order agree. It is for demos and load tests, where hand-building a
// large set is impractical.
func GenerateValidators(n int) []Validator {
	seeds := make([][]byte, n)
	for i := range seeds {
		_, priv, err := ed25519.GenerateKey(nil)
		if err != nil {
			panic(err) // crypto/rand failure
		}
		seeds[i] = priv.Seed()
	}
	return ValidatorsFromSeeds(seeds)
}

// ValidatorsFromSeeds builds the same skewed, named set as GenerateValidators
// but from fixed 32-byte ed25519 seeds, so a restarted process keeps the
// same validator identities. Rank i takes seeds[i].
func ValidatorsFromSeeds(seeds [][]byte) []Validator {
	out := make([]Validator, len(seeds))
	for i, seed := range seeds {
		v := NewValidator(ed25519.NewKeyFromSeed(seed), generatedStake(i))
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
	return uint64(stake/BaseUnitsPerCoin) * BaseUnitsPerCoin
}
