package chain

import "testing"

// Generated stakes strictly decrease by rank until the floor, no single
// validator dominates, and every validator gets a distinct moniker.
func TestGenerateValidators(t *testing.T) {
	vs := GenerateValidators(20)
	var total uint64
	names := map[string]bool{}
	for i, v := range vs {
		total += v.stake
		if v.moniker == "" || names[v.moniker] {
			t.Errorf("rank %d: moniker %q empty or reused", i, v.moniker)
		}
		names[v.moniker] = true
		if i > 0 && v.stake >= vs[i-1].stake {
			t.Errorf("rank %d stake %d is not below rank %d stake %d", i, v.stake, i-1, vs[i-1].stake)
		}
		if v.stake%baseUnitsPerCoin != 0 {
			t.Errorf("rank %d stake %d is not a whole number of DYL", i, v.stake)
		}
	}
	if top := float64(vs[0].stake) / float64(total); top > 0.2 {
		t.Errorf("top validator holds %.0f%% of stake, want under 20%%", top*100)
	}
	// The floor kicks in far down the list and holds there.
	deep := GenerateValidators(80)
	if deep[79].stake != floorStake {
		t.Errorf("rank 79 stake %d, want the floor %d", deep[79].stake, floorStake)
	}
	if deep[79].moniker != "val-079" {
		t.Errorf("rank 79 moniker %q, want val-079", deep[79].moniker)
	}
}
