package chain

import (
	"bytes"
	"testing"
)

// Applying a block splits its BlockReward between the proposer's genesis
// self-stake and everyone delegated to it, pro rata, and the delegate
// takes effect in the same block it lands in.
func TestApplyRewardSplit(t *testing.T) {
	alice := newWallet(t)
	v := newWallet(t)
	other := newWallet(t)

	gen := genesisBlock(
		map[string]uint64{alice.addr: 1000},
		map[string]uint64{v.addr: 300, other.addr: 300},
	)
	st, err := ReplayBlocks([]Block{gen})
	if err != nil {
		t.Fatal(err)
	}

	b1 := Block{Height: 1, Proposer: v.addr, Transactions: []Transaction{
		alice.delegate(t, v.addr, 100, 0),
	}}
	st, err = Apply(st, b1)
	if err != nil {
		t.Fatalf("apply b1: %v", err)
	}

	// v's weight for the split is 300 self + 100 delegated = 400.
	aliceShare := mulDiv(BlockReward, 100, 400)
	if got, want := st.Balances[alice.addr], uint64(900)+aliceShare; got != want {
		t.Errorf("alice balance %d, want %d (900 left after delegating + %d reward)", got, want, aliceShare)
	}
	if got, want := st.Balances[v.addr], BlockReward-aliceShare; got != want {
		t.Errorf("proposer balance %d, want %d (reward minus the delegator share)", got, want)
	}
	if got := st.Delegations[alice.addr][v.addr]; got != 100 {
		t.Errorf("bonded %d, want 100", got)
	}
	if got, want := st.Supply(), uint64(1000)+BlockReward; got != want {
		t.Errorf("supply %d, want %d", got, want)
	}

	// Undelegating returns the tokens to the balance and clears the bond.
	b2 := Block{Height: 2, Proposer: other.addr, Transactions: []Transaction{
		alice.undelegate(t, v.addr, 100, 1),
	}}
	st, err = Apply(st, b2)
	if err != nil {
		t.Fatalf("apply b2: %v", err)
	}
	if got, want := st.Balances[alice.addr], uint64(1000)+aliceShare; got != want {
		t.Errorf("alice balance after undelegate %d, want %d", got, want)
	}
	if _, ok := st.Delegations[alice.addr]; ok {
		t.Errorf("delegation entry not cleared: %v", st.Delegations[alice.addr])
	}
	if got, want := st.Supply(), uint64(1000)+2*BlockReward; got != want {
		t.Errorf("supply after undelegate %d, want %d", got, want)
	}
}

// Undelegating more than is bonded rejects the whole block.
func TestApplyRejectsOverUndelegate(t *testing.T) {
	alice := newWallet(t)
	v := newWallet(t)
	gen := genesisBlock(map[string]uint64{alice.addr: 1000}, map[string]uint64{v.addr: 10})
	st, _ := ReplayBlocks([]Block{gen})

	b := Block{Height: 1, Proposer: v.addr, Transactions: []Transaction{
		alice.undelegate(t, v.addr, 50, 0), // nothing bonded
	}}
	if _, err := Apply(st, b); err == nil {
		t.Fatal("Apply accepted an undelegate with no bond")
	}
}

// A delegate signature does not verify as a transfer and vice versa.
func TestApplyKindIsSigned(t *testing.T) {
	alice := newWallet(t)
	v := newWallet(t)
	gen := genesisBlock(map[string]uint64{alice.addr: 1000}, map[string]uint64{v.addr: 10})
	st, _ := ReplayBlocks([]Block{gen})

	del := alice.delegate(t, v.addr, 100, 0)
	forged := del
	forged.Kind = KindTransfer // reuse the delegate signature as a transfer

	b := Block{Height: 1, Proposer: v.addr, Transactions: []Transaction{forged}}
	if _, err := Apply(st, b); err == nil {
		t.Fatal("a delegate signature verified as a transfer")
	}
}

// syncDelegations folds bonded stake into a validator's election weight.
func TestElectionSyncDelegations(t *testing.T) {
	set := NewValidatorSet(
		NewValidator(newWallet(t).priv, 10),
		NewValidator(newWallet(t).priv, 10),
		NewValidator(newWallet(t).priv, 10),
	)
	e := newElection(set)
	target := set.members[2].address

	e.syncDelegations(State{Delegations: map[string]map[string]uint64{
		"delegator-a": {target: 40},
		"delegator-b": {target: 10},
	}})

	if got := e.stakeOf(target); got != 60 {
		t.Errorf("weight after delegation %d, want 60 (10 self + 50 bonded)", got)
	}
	if got := e.totalStake(); got != 80 {
		t.Errorf("total weight %d, want 80", got)
	}

	// With 60 of 80 it should now propose most heights.
	wins := 0
	for i := 0; i < 100; i++ {
		if e.next() == target {
			wins++
		}
	}
	if wins < 60 {
		t.Errorf("target won %d of 100 heights, want at least 60", wins)
	}
}

// Over a live run a delegation raises a validator's block share, pays the
// delegator a cut of those blocks, and total supply still grows by exactly
// one BlockReward per height.
func TestClusterDelegationEarns(t *testing.T) {
	set := fourValidators(t) // self-stake 1 each
	alice := newWallet(t)
	const start = 1_000_000
	cl := NewCluster(map[string]uint64{alice.addr: start}, set, 0)

	v := set.members[0].address
	if err := cl.Submit(alice.delegate(t, v, 3, 0)); err != nil { // v: weight 4 vs 1
		t.Fatal(err)
	}

	const heights = 40
	cl.Run(heights)

	if got := cl.Delegations(alice.addr)[v]; got != 3 {
		t.Fatalf("bonded %d, want 3", got)
	}
	bal, _ := cl.Account(alice.addr)
	if bal <= start-3 {
		t.Errorf("alice balance %d, want more than %d: she earned nothing from her delegation", bal, start-3)
	}

	snap := cl.Snapshot()
	if want := uint64(start) + heights*BlockReward; snap.Supply != want {
		t.Errorf("supply %d, want %d", snap.Supply, want)
	}
	var v0 ValidatorInfo
	for _, info := range snap.Validators {
		if info.Address == v {
			v0 = info
		}
	}
	if v0.Delegated != 3 {
		t.Errorf("snapshot delegated %d, want 3", v0.Delegated)
	}
	if v0.Proposed <= heights/4 {
		t.Errorf("delegated validator proposed %d of %d, want more than an equal share", v0.Proposed, heights)
	}

	for i := 0; i < cl.Size(); i++ {
		if err := cl.Chain(i).Validate(); err != nil {
			t.Errorf("node %d chain does not validate: %v", i, err)
		}
	}
}

// Delegating and later undelegating the same amount is a round trip: the
// balance comes back, the bond is gone, supply is untouched, and every
// node's chain still replays.
func TestClusterDelegationRoundTrip(t *testing.T) {
	set := fourValidators(t)
	alice := newWallet(t)
	const start = 500_000
	cl := NewCluster(map[string]uint64{alice.addr: start}, set, 0)

	v := set.members[1].address
	if err := cl.Submit(alice.delegate(t, v, 1000, 0)); err != nil {
		t.Fatal(err)
	}
	if err := cl.Submit(alice.undelegate(t, v, 1000, 1)); err != nil {
		t.Fatal(err)
	}

	const heights = 20
	cl.Run(heights)

	if got := cl.Delegations(alice.addr); got != nil {
		t.Errorf("delegations after round trip: %v, want none", got)
	}
	bal, nonce := cl.Account(alice.addr)
	if bal < start {
		t.Errorf("alice balance %d, want at least %d back", bal, start)
	}
	if nonce != 2 {
		t.Errorf("alice nonce %d, want 2", nonce)
	}
	if want := uint64(start) + heights*BlockReward; cl.Snapshot().Supply != want {
		t.Errorf("supply %d, want %d", cl.Snapshot().Supply, want)
	}

	ref := cl.Chain(0).Blocks[heights].Hash()
	for i := 0; i < cl.Size(); i++ {
		if err := cl.Chain(i).Validate(); err != nil {
			t.Errorf("node %d does not validate: %v", i, err)
		}
		if !bytes.Equal(cl.Chain(i).Blocks[heights].Hash(), ref) {
			t.Errorf("node %d tip differs from node 0", i)
		}
	}
}
