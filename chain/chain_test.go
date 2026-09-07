package chain

import (
	"bytes"
	"maps"
	"testing"
)

// A fresh chain has exactly the genesis block, at height 0 with no predecessor.
func TestNewChain(t *testing.T) {
	c := NewChain(nil)

	if len(c.Blocks) != 1 {
		t.Fatalf("new chain: got %d blocks, want 1", len(c.Blocks))
	}
	genesis := c.Blocks[0]
	if genesis.Height != 0 {
		t.Errorf("genesis height: got %d, want 0", genesis.Height)
	}
	if len(genesis.PreviousHash) != 0 {
		t.Errorf("genesis PreviousHash: got %x, want empty", genesis.PreviousHash)
	}
}

// AddBlock links each new block to the hash of the one before it and bumps height by one.
func TestAddBlockLinksAndHeights(t *testing.T) {
	alice := newWallet(t)
	bob := newWallet(t)
	carol := newWallet(t)
	c := NewChain(map[string]uint64{alice.addr: 1000})

	mustAdd(t, &c, alice.send(t, bob.addr, 10, 0))
	mustAdd(t, &c, bob.send(t, carol.addr, 5, 0))
	mustAdd(t, &c, carol.send(t, alice.addr, 1, 0))

	if len(c.Blocks) != 4 {
		t.Fatalf("got %d blocks, want 4 (genesis + 3)", len(c.Blocks))
	}

	for i := 1; i < len(c.Blocks); i++ {
		wantHeight := int64(i)
		if c.Blocks[i].Height != wantHeight {
			t.Errorf("block %d height: got %d, want %d", i, c.Blocks[i].Height, wantHeight)
		}
		if !bytes.Equal(c.Blocks[i].PreviousHash, c.Blocks[i-1].Hash()) {
			t.Errorf("block %d PreviousHash does not match hash of block %d", i, i-1)
		}
	}
}

// AddBlock advances the chain's state as blocks are added.
func TestAddBlockAdvancesState(t *testing.T) {
	alice := newWallet(t)
	bob := newWallet(t)
	c := NewChain(map[string]uint64{alice.addr: 1000})

	mustAdd(t, &c, alice.send(t, bob.addr, 100, 0))
	mustAdd(t, &c, alice.send(t, bob.addr, 50, 1))

	if got := c.state.Balances[alice.addr]; got != 850 {
		t.Errorf("alice balance: got %d, want 850", got)
	}
	if got := c.state.Balances[bob.addr]; got != 150 {
		t.Errorf("bob balance: got %d, want 150", got)
	}
	if got := c.state.Nonces[alice.addr]; got != 2 {
		t.Errorf("alice nonce: got %d, want 2", got)
	}
}

// A block whose transactions do not apply is rejected, and the chain is
// left untouched.
func TestAddBlockRejectsInvalidBlock(t *testing.T) {
	alice := newWallet(t)
	bob := newWallet(t)
	c := NewChain(map[string]uint64{alice.addr: 10})

	err := c.AddBlock([]Transaction{alice.send(t, bob.addr, 100, 0)}) // more than alice has
	if err == nil {
		t.Fatal("overspending block should be rejected")
	}
	if len(c.Blocks) != 1 {
		t.Errorf("rejected block was still appended: chain has %d blocks", len(c.Blocks))
	}
	if c.state.Balances[alice.addr] != 10 {
		t.Errorf("rejected block changed state: alice balance is %d, want 10", c.state.Balances[alice.addr])
	}
}

// A chain that has only been built through AddBlock is valid.
func TestValidateCleanChain(t *testing.T) {
	alice := newWallet(t)
	bob := newWallet(t)
	c := NewChain(map[string]uint64{alice.addr: 1000})

	mustAdd(t, &c, alice.send(t, bob.addr, 10, 0))
	mustAdd(t, &c, alice.send(t, bob.addr, 5, 1))

	if err := c.Validate(); err != nil {
		t.Fatalf("clean chain should validate, got error: %v", err)
	}
}

// Mutating a transaction after the fact breaks the link for the NEXT block,
// because that next block's PreviousHash was computed from the original contents.
func TestValidateDetectsTamperedTransaction(t *testing.T) {
	alice := newWallet(t)
	bob := newWallet(t)
	c := NewChain(map[string]uint64{alice.addr: 1000})

	mustAdd(t, &c, alice.send(t, bob.addr, 10, 0))
	mustAdd(t, &c, alice.send(t, bob.addr, 5, 1))
	mustAdd(t, &c, alice.send(t, bob.addr, 1, 2))

	c.Blocks[1].Transactions[0].Amount = 999999

	err := c.Validate()
	if err == nil {
		t.Fatal("tampered chain should not validate, got nil error")
	}
	t.Logf("got expected error: %v", err)
}

// Changing a transaction in the final block is caught by the per-block
// TxRoot check, even though nothing links back to the tip's hash.
func TestValidateDetectsTamperedTipTransaction(t *testing.T) {
	alice := newWallet(t)
	bob := newWallet(t)
	c := NewChain(map[string]uint64{alice.addr: 1000})

	mustAdd(t, &c, alice.send(t, bob.addr, 10, 0))
	mustAdd(t, &c, alice.send(t, bob.addr, 5, 1))

	c.Blocks[len(c.Blocks)-1].Transactions[0].Amount = 999999

	if err := c.Validate(); err == nil {
		t.Fatal("tampered tip transaction should not validate, got nil error")
	}
}

// A *consistent* rewrite of the tip (transaction changed AND TxRoot
// recomputed to match) used to slip past Validate, because nothing links to
// the tip's hash. Replaying state from genesis now catches it: the altered
// transaction no longer matches its own signature. The new amount stays
// within alice's balance, so it is the signature check, not an overdraft,
// that fails.
func TestValidateCatchesConsistentTipRewrite(t *testing.T) {
	alice := newWallet(t)
	bob := newWallet(t)
	c := NewChain(map[string]uint64{alice.addr: 1000})

	mustAdd(t, &c, alice.send(t, bob.addr, 10, 0))
	mustAdd(t, &c, alice.send(t, bob.addr, 5, 1))

	tip := &c.Blocks[len(c.Blocks)-1]
	tip.Transactions[0].Amount = 900
	tip.TxRoot = merkleRoot(tip.Transactions)

	if err := c.Validate(); err == nil {
		t.Fatal("consistent tip rewrite should fail replay, got nil error")
	}
}

// Validate replays the whole chain and compares the result to the chain's
// own state, so state that has drifted from the blocks is caught even when
// every block is individually well formed.
func TestValidateCatchesStateDrift(t *testing.T) {
	alice := newWallet(t)
	bob := newWallet(t)
	c := NewChain(map[string]uint64{alice.addr: 1000})

	mustAdd(t, &c, alice.send(t, bob.addr, 10, 0))
	mustAdd(t, &c, alice.send(t, bob.addr, 5, 1))

	// Corrupt the in-memory ledger without touching any block.
	c.state.Balances[alice.addr] += 1000

	if err := c.Validate(); err == nil {
		t.Fatal("state that disagrees with the blocks should not validate, got nil error")
	}
}

// ReplayBlocks rebuilds exactly the ledger a live chain holds.
func TestReplayBlocksMatchesLiveState(t *testing.T) {
	alice := newWallet(t)
	bob := newWallet(t)
	c := NewChain(map[string]uint64{alice.addr: 1000})

	mustAdd(t, &c, alice.send(t, bob.addr, 100, 0))
	mustAdd(t, &c, alice.send(t, bob.addr, 50, 1))
	mustAdd(t, &c, bob.send(t, alice.addr, 20, 0))

	replayed, err := ReplayBlocks(c.Blocks)
	if err != nil {
		t.Fatalf("ReplayBlocks: %v", err)
	}
	if !maps.Equal(replayed.Balances, c.state.Balances) {
		t.Errorf("replayed balances %v, want %v", replayed.Balances, c.state.Balances)
	}
	if !maps.Equal(replayed.Nonces, c.state.Nonces) {
		t.Errorf("replayed nonces %v, want %v", replayed.Nonces, c.state.Nonces)
	}
}

// ReplayBlocks rejects a block list that does not start with a genesis
// block.
func TestReplayBlocksRejectsBadGenesis(t *testing.T) {
	if _, err := ReplayBlocks(nil); err == nil {
		t.Error("empty block list should be rejected")
	}

	notGenesis := []Block{{Height: 1}}
	if _, err := ReplayBlocks(notGenesis); err == nil {
		t.Error("first block at height 1 should be rejected")
	}
}

// Rewriting a block's height is caught by the height-sequence check.
func TestValidateDetectsBrokenHeightSequence(t *testing.T) {
	alice := newWallet(t)
	bob := newWallet(t)
	c := NewChain(map[string]uint64{alice.addr: 1000})

	mustAdd(t, &c, alice.send(t, bob.addr, 10, 0))
	mustAdd(t, &c, alice.send(t, bob.addr, 5, 1))

	c.Blocks[1].Height = 99

	if err := c.Validate(); err == nil {
		t.Fatal("broken height sequence should not validate, got nil error")
	}
}

// CommitBlock accepts a well-formed signed candidate, links it, advances
// state, and leaves a chain that still validates.
func TestCommitBlockAppendsAndAdvances(t *testing.T) {
	alice := newWallet(t)
	bob := newWallet(t)
	proposer := newWallet(t)
	c := NewChain(map[string]uint64{alice.addr: 1000})

	b := proposer.proposeBlock(t, &c, alice.send(t, bob.addr, 100, 0))
	if err := c.CommitBlock(b); err != nil {
		t.Fatalf("CommitBlock rejected a valid candidate: %v", err)
	}

	if len(c.Blocks) != 2 {
		t.Fatalf("got %d blocks, want 2 (genesis + 1)", len(c.Blocks))
	}
	if got := c.state.Balances[bob.addr]; got != 100 {
		t.Errorf("bob balance: got %d, want 100", got)
	}
	if err := c.Validate(); err != nil {
		t.Errorf("committed chain should validate: %v", err)
	}
}

// CommitBlock rejects a candidate whose checks fail, and leaves the chain
// untouched.
func TestCommitBlockRejects(t *testing.T) {
	alice := newWallet(t)
	bob := newWallet(t)
	proposer := newWallet(t)

	cases := []struct {
		name   string
		mutate func(*Block)
	}{
		{"wrong height", func(b *Block) { b.Height = 99 }},
		{"broken link", func(b *Block) { b.PreviousHash = []byte("nope") }},
		{"tx root mismatch", func(b *Block) { b.TxRoot = merkleRoot(nil) }},
		{"no proposer", func(b *Block) { b.Proposer = ""; b.Signature = nil }},
		{"bad signature", func(b *Block) { b.Signature = []byte("garbage") }},
		{"tampered after signing", func(b *Block) { b.CreatedAt++ }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := NewChain(map[string]uint64{alice.addr: 1000})
			b := proposer.proposeBlock(t, &c, alice.send(t, bob.addr, 10, 0))
			tc.mutate(&b)

			if err := c.CommitBlock(b); err == nil {
				t.Fatal("expected CommitBlock to reject the candidate, got nil")
			}
			if len(c.Blocks) != 1 {
				t.Errorf("rejected candidate was appended: chain has %d blocks", len(c.Blocks))
			}
		})
	}
}

// A consensus block's proposer signature covers its whole header, so a
// later rewrite of a header field that nothing links to is caught. This is
// the gap that stays open on the unsigned AddBlock path.
func TestValidateCatchesTamperedConsensusHeader(t *testing.T) {
	alice := newWallet(t)
	bob := newWallet(t)
	proposer := newWallet(t)
	c := NewChain(map[string]uint64{alice.addr: 1000})

	b := proposer.proposeBlock(t, &c, alice.send(t, bob.addr, 10, 0))
	if err := c.CommitBlock(b); err != nil {
		t.Fatalf("CommitBlock: %v", err)
	}

	c.Blocks[1].CreatedAt++ // value field, only this chain's copy

	if err := c.Validate(); err == nil {
		t.Fatal("tampered consensus header should fail validation, got nil error")
	}
}

// AddBlock on a zero-value Chain panics rather than silently building a
// chain with no genesis.
func TestAddBlockPanicsWithoutGenesis(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("AddBlock on empty chain should panic, it did not")
		}
	}()

	var c Chain
	_ = c.AddBlock(txs(tx("alice", "bob", 1, 0)))
}
