package chain

import "testing"

// NewValidator derives the same address as a wallet holding the same key.
func TestNewValidatorDerivesAddress(t *testing.T) {
	w := newWallet(t)
	v := NewValidator(w.priv, 10)

	if v.Address() != w.addr {
		t.Errorf("validator address %s, want %s", v.Address(), w.addr)
	}
}

// TotalStake is the sum of every member's stake.
func TestValidatorSetTotalStake(t *testing.T) {
	set := NewValidatorSet(
		NewValidator(newWallet(t).priv, 10),
		NewValidator(newWallet(t).priv, 25),
		NewValidator(newWallet(t).priv, 5),
	)

	if got := set.TotalStake(); got != 40 {
		t.Errorf("total stake: got %d, want 40", got)
	}
}

// With equal stakes, ProposerForHeight reduces to plain round-robin:
// members in order, wrapping, starting from the first at height 1.
func TestProposerRotation(t *testing.T) {
	a := NewValidator(newWallet(t).priv, 1)
	b := NewValidator(newWallet(t).priv, 1)
	c := NewValidator(newWallet(t).priv, 1)
	set := NewValidatorSet(a, b, c)

	want := []string{a.address, b.address, c.address, a.address, b.address, c.address, a.address}
	for i, wantAddr := range want {
		h := int64(i + 1)
		if got := set.ProposerForHeight(h).address; got != wantAddr {
			t.Errorf("height %d: proposer %s, want %s", h, got, wantAddr)
		}
	}
}

// With unequal stakes, ProposerForHeight weights how often each validator
// proposes by its share of total stake, interleaved rather than in a run.
func TestProposerStakeWeighted(t *testing.T) {
	a := NewValidator(newWallet(t).priv, 3)
	b := NewValidator(newWallet(t).priv, 1)
	c := NewValidator(newWallet(t).priv, 1)
	set := NewValidatorSet(a, b, c) // total stake 5

	// One cycle is smoothed, not an "a a a b c" run.
	wantCycle := []string{a.address, b.address, a.address, c.address, a.address}
	for i, want := range wantCycle {
		if got := set.ProposerForHeight(int64(i + 1)).address; got != want {
			t.Errorf("height %d: proposer %s, want %s", i+1, got, want)
		}
	}

	// Over whole cycles, each validator's count matches its stake.
	cycles := 4
	counts := map[string]int{}
	for h := int64(1); h <= int64(cycles)*int64(set.TotalStake()); h++ {
		counts[set.ProposerForHeight(h).address]++
	}
	for _, v := range []Validator{a, b, c} {
		if want := int(v.stake) * cycles; counts[v.address] != want {
			t.Errorf("%s proposed %d times over %d cycles, want %d", v.address, counts[v.address], cycles, want)
		}
	}
}

// ProposerForHeight is a pure function of (height, set): the same height
// always gives the same answer, and the schedule repeats every TotalStake
// heights.
func TestProposerForHeightDeterministic(t *testing.T) {
	set := NewValidatorSet(
		NewValidator(newWallet(t).priv, 2),
		NewValidator(newWallet(t).priv, 1),
	)
	period := int64(set.TotalStake())
	for h := int64(1); h <= 3*period; h++ {
		first := set.ProposerForHeight(h).address
		if again := set.ProposerForHeight(h).address; again != first {
			t.Fatalf("height %d: %s then %s on a repeat call", h, first, again)
		}
		if wrapped := set.ProposerForHeight(h + period).address; wrapped != first {
			t.Errorf("height %d and %d disagree (%s vs %s), schedule should repeat every %d",
				h, h+period, first, wrapped, period)
		}
	}
}

// ProposerForHeight on a set with no members panics rather than dividing by
// zero.
func TestProposerForHeightPanicsOnEmptySet(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("ProposerForHeight on an empty set should panic, it did not")
		}
	}()

	NewValidatorSet().ProposerForHeight(1)
}

// StakeOf and Contains answer for a member and for a stranger.
func TestValidatorSetLookup(t *testing.T) {
	member := NewValidator(newWallet(t).priv, 30)
	stranger := newWallet(t)
	set := NewValidatorSet(member, NewValidator(newWallet(t).priv, 70))

	if got := set.StakeOf(member.address); got != 30 {
		t.Errorf("StakeOf(member): got %d, want 30", got)
	}
	if got := set.StakeOf(stranger.addr); got != 0 {
		t.Errorf("StakeOf(stranger): got %d, want 0", got)
	}
	if !set.Contains(member.address) {
		t.Error("Contains(member): got false, want true")
	}
	if set.Contains(stranger.addr) {
		t.Error("Contains(stranger): got true, want false")
	}
}
