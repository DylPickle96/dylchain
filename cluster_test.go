package dyl

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
