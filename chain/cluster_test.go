package chain

import (
	"bytes"
	"testing"
)

// fourValidators returns a set of four equal-stake validators. With total
// stake 4, the commit threshold (3*accepted > 2*total) needs 3 of them.
func fourValidators(t *testing.T) *ValidatorSet {
	t.Helper()
	return NewValidatorSet(
		NewValidator(newWallet(t).priv, 1),
		NewValidator(newWallet(t).priv, 1),
		NewValidator(newWallet(t).priv, 1),
		NewValidator(newWallet(t).priv, 1),
	)
}

// A cluster of honest validators commits the submitted transactions, and
// every validator ends on the identical chain and ledger.
func TestClusterReachesConsensus(t *testing.T) {
	alice := newWallet(t)
	bob := newWallet(t)
	carol := newWallet(t)

	set := fourValidators(t)
	cl := NewCluster(map[string]uint64{alice.addr: 1000}, set, 0)

	cl.Submit(alice.send(t, bob.addr, 100, 0))
	cl.Submit(alice.send(t, carol.addr, 50, 1))
	cl.Submit(bob.send(t, carol.addr, 30, 0))

	cl.Run(2)

	want := map[string]uint64{alice.addr: 850, bob.addr: 70, carol.addr: 80}
	ref := cl.Chain(0)
	if len(ref.Blocks) != 3 {
		t.Fatalf("chain 0: got %d blocks, want 3 (genesis + 2)", len(ref.Blocks))
	}

	for i := 0; i < cl.Size(); i++ {
		ch := cl.Chain(i)
		if err := ch.Validate(); err != nil {
			t.Errorf("chain %d does not validate: %v", i, err)
		}
		if len(ch.Blocks) != len(ref.Blocks) {
			t.Errorf("chain %d: %d blocks, want %d", i, len(ch.Blocks), len(ref.Blocks))
		}
		tip := ch.Blocks[len(ch.Blocks)-1]
		refTip := ref.Blocks[len(ref.Blocks)-1]
		if !bytes.Equal(tip.Hash(), refTip.Hash()) {
			t.Errorf("chain %d tip hash differs from chain 0", i)
		}
		for addr, w := range want {
			if got := ch.state.Balances[addr]; got != w {
				t.Errorf("chain %d: balance of %s is %d, want %d", i, addr, got, w)
			}
		}
	}
}

// A per-block cap spreads a batch of transactions across several committed
// blocks instead of one.
func TestClusterSpreadsTransactionsAcrossBlocks(t *testing.T) {
	alice := newWallet(t)
	bob := newWallet(t)

	set := fourValidators(t)
	cl := NewCluster(map[string]uint64{alice.addr: 1000}, set, 2)

	for nonce := int64(0); nonce < 6; nonce++ {
		cl.Submit(alice.send(t, bob.addr, 10, nonce))
	}

	cl.Run(3)

	ch := cl.Chain(0)
	if len(ch.Blocks) != 4 {
		t.Fatalf("got %d blocks, want 4 (genesis + 3)", len(ch.Blocks))
	}
	for h := 1; h <= 3; h++ {
		if got := len(ch.Blocks[h].Transactions); got != 2 {
			t.Errorf("block %d: %d transactions, want 2", h, got)
		}
	}
	if got := ch.state.Balances[bob.addr]; got != 60 {
		t.Errorf("bob balance: got %d, want 60", got)
	}
}

// Every committed block mints BlockReward to its proposer, so total supply
// grows by one reward per height and each proposer's balance is exactly the
// rewards it earned.
func TestClusterSupplyGrowsWithMinting(t *testing.T) {
	alice := newWallet(t)
	set := fourValidators(t)
	cl := NewCluster(map[string]uint64{alice.addr: 1000}, set, 0)

	const heights = 6
	cl.Run(heights)

	ch := cl.Chain(0)
	if err := ch.Validate(); err != nil {
		t.Fatalf("chain does not validate: %v", err)
	}

	wantSupply := uint64(1000 + heights*BlockReward)
	if got := ch.state.Supply(); got != wantSupply {
		t.Errorf("supply after %d heights: got %d, want %d", heights, got, wantSupply)
	}

	wantRewards := map[string]uint64{}
	for h := int64(1); h <= heights; h++ {
		wantRewards[set.ProposerForHeight(h).address] += BlockReward
	}
	for addr, want := range wantRewards {
		if got := ch.state.Balances[addr]; got != want {
			t.Errorf("proposer %s: balance %d, want %d in rewards", addr, got, want)
		}
	}
}

// A validator made to double-vote is caught by every node, including its
// own, and the cluster still commits every block on the honest majority.
func TestClusterCatchesDoubleVoter(t *testing.T) {
	alice := newWallet(t)
	set := fourValidators(t)
	cl := NewCluster(map[string]uint64{alice.addr: 1000}, set, 0)
	cl.MakeFaulty(2)
	cl.Submit(alice.send(t, newWallet(t).addr, 10, 0))

	const heights = 8
	cl.Run(heights)

	offender := set.members[2].address

	// The double-vote is caught at least once. Once an offender is slashed,
	// nodes stop recording further evidence for it, so the count is small.
	ev := cl.Evidence()
	if len(ev) == 0 {
		t.Fatal("double-voter went undetected")
	}
	for _, e := range ev {
		if e.Offender != offender {
			t.Errorf("evidence at height %d names %s, want %s", e.Height, e.Offender, offender)
		}
		if !verifyEvidence(e) {
			t.Errorf("evidence at height %d does not verify", e.Height)
		}
	}

	// Every node caught the cheat, the offender's own node included.
	for i := 0; i < cl.Size(); i++ {
		if len(cl.nodes[i].evidence) == 0 {
			t.Errorf("node %d produced no evidence", i)
		}
	}

	// Consensus still completed on every chain.
	for i := 0; i < cl.Size(); i++ {
		ch := cl.Chain(i)
		if len(ch.Blocks) != heights+1 {
			t.Errorf("node %d: %d blocks, want %d", i, len(ch.Blocks), heights+1)
		}
		if err := ch.Validate(); err != nil {
			t.Errorf("node %d chain does not validate: %v", i, err)
		}
	}
}

// A caught double-voter has its stake cut to zero, and the cluster keeps
// producing and validating blocks on the remaining three.
func TestClusterSlashesDoubleVoter(t *testing.T) {
	alice := newWallet(t)
	set := fourValidators(t)
	cl := NewCluster(map[string]uint64{alice.addr: 1000}, set, 0)
	cl.MakeFaulty(2)

	const heights = 8
	cl.Run(heights)

	offender := set.members[2].address
	if got := cl.ValidatorSet().StakeOf(offender); got != 0 {
		t.Errorf("offender stake after run: got %d, want 0", got)
	}
	if got := cl.ValidatorSet().TotalStake(); got != 3 {
		t.Errorf("total stake after slashing 1 of 4: got %d, want 3", got)
	}

	for i := 0; i < cl.Size(); i++ {
		ch := cl.Chain(i)
		if len(ch.Blocks) != heights+1 {
			t.Errorf("node %d: %d blocks, want %d", i, len(ch.Blocks), heights+1)
		}
		if err := ch.Validate(); err != nil {
			t.Errorf("node %d chain does not validate: %v", i, err)
		}
	}
}

// Every committed block names the proposer the set selected for its height
// and carries a signature that verifies.
func TestClusterCommittedBlocksAreSignedByProposer(t *testing.T) {
	alice := newWallet(t)
	bob := newWallet(t)

	set := fourValidators(t)
	cl := NewCluster(map[string]uint64{alice.addr: 1000}, set, 0)
	cl.Submit(alice.send(t, bob.addr, 10, 0))

	cl.Run(5)

	ch := cl.Chain(0)
	for h := int64(1); h <= 5; h++ {
		b := ch.Blocks[h]
		want := set.ProposerForHeight(h).address
		if b.Proposer != want {
			t.Errorf("block %d: proposer %s, want %s", h, b.Proposer, want)
		}
		if err := verifyBlockSignature(b); err != nil {
			t.Errorf("block %d: signature does not verify: %v", h, err)
		}
	}
}

// A large generated cluster with one faulty validator still reaches
// consensus on every node and slashes the offender. Skipped under -short.
func TestClusterLargeWithFault(t *testing.T) {
	if testing.Short() {
		t.Skip("large cluster")
	}
	const (
		n       = 64
		faulty  = 3
		heights = 40
	)
	set := NewValidatorSet(GenerateValidators(n)...)
	cl := NewCluster(nil, set, 0)
	cl.MakeFaulty(faulty)

	cl.Run(heights)

	offender := set.members[faulty].address
	if got := set.StakeOf(offender); got != 0 {
		t.Errorf("offender stake after run: got %d, want 0", got)
	}
	if len(cl.Evidence()) == 0 {
		t.Error("no equivocation evidence from a run with a faulty node")
	}

	ref := cl.Chain(0).Blocks[heights].Hash()
	for i := 0; i < cl.Size(); i++ {
		ch := cl.Chain(i)
		if len(ch.Blocks) != heights+1 {
			t.Errorf("node %d: %d blocks, want %d", i, len(ch.Blocks), heights+1)
		}
		if !bytes.Equal(ch.Blocks[heights].Hash(), ref) {
			t.Errorf("node %d tip hash differs from node 0", i)
		}
		if err := ch.Validate(); err != nil {
			t.Errorf("node %d does not validate: %v", i, err)
		}
	}
}
