package dyl

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

// ProposerForHeight walks the members in order and wraps, starting from the
// first member at height 1.
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
