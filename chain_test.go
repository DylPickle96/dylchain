package blockchain

import (
	"bytes"
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
// recomputed to match) is still NOT caught, because no later block links
// to the tip's hash. This is the residual cost of deriving the block hash
// rather than storing it, and it closes once consensus signs blocks.
func TestValidateDoesNotCatchConsistentTipRewrite(t *testing.T) {
	alice := newWallet(t)
	bob := newWallet(t)
	c := NewChain(map[string]uint64{alice.addr: 1000})

	mustAdd(t, &c, alice.send(t, bob.addr, 10, 0))
	mustAdd(t, &c, alice.send(t, bob.addr, 5, 1))

	tip := &c.Blocks[len(c.Blocks)-1]
	tip.Transactions[0].Amount = 999999
	tip.TxRoot = merkleRoot(tip.Transactions)

	if err := c.Validate(); err != nil {
		t.Fatalf("consistent tip rewrite is undetectable, but Validate returned: %v", err)
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
