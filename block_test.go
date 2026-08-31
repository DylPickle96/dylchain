package blockchain

import (
	"bytes"
	"testing"
)

// helper: a one-transaction batch, so tests read clearly.
func txs(t ...Transaction) []Transaction { return t }

func tx(from, to string, amount uint64, nonce int64) Transaction {
	return Transaction{From: from, To: to, Amount: amount, Nonce: nonce}
}

// A fresh chain has exactly the genesis block, at height 0 with no predecessor.
func TestNewChain(t *testing.T) {
	c := NewChain()

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
	c := NewChain()
	c.AddBlock(txs(tx("alice", "bob", 10, 0)))
	c.AddBlock(txs(tx("bob", "carol", 5, 0)))
	c.AddBlock(txs(tx("carol", "alice", 1, 0)))

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

// A chain that has only been built through AddBlock is valid.
func TestValidateCleanChain(t *testing.T) {
	c := NewChain()
	c.AddBlock(txs(tx("alice", "bob", 10, 0)))
	c.AddBlock(txs(tx("bob", "carol", 5, 0)))

	if err := c.Validate(); err != nil {
		t.Fatalf("clean chain should validate, got error: %v", err)
	}
}

// Mutating a transaction after the fact breaks the link for the NEXT block,
// because that next block's PreviousHash was computed from the original contents.
func TestValidateDetectsTamperedTransaction(t *testing.T) {
	c := NewChain()
	c.AddBlock(txs(tx("alice", "bob", 10, 0)))
	c.AddBlock(txs(tx("bob", "carol", 5, 0)))
	c.AddBlock(txs(tx("carol", "alice", 1, 0)))

	c.Blocks[1].Transactions[0].Amount = 999999

	err := c.Validate()
	if err == nil {
		t.Fatal("tampered chain should not validate, got nil error")
	}
	t.Logf("got expected error: %v", err)
}

// Tampering with the final block is NOT caught by Validate, because no later
// block links back to it. This is the cost of deriving the hash instead of
// storing it on the block. Locked in so the behavior is intentional.
func TestValidateDoesNotCatchTamperedTip(t *testing.T) {
	c := NewChain()
	c.AddBlock(txs(tx("alice", "bob", 10, 0)))
	c.AddBlock(txs(tx("bob", "carol", 5, 0)))

	c.Blocks[len(c.Blocks)-1].Transactions[0].Amount = 999999

	if err := c.Validate(); err != nil {
		t.Fatalf("tip tampering is currently undetectable, but Validate returned: %v", err)
	}
}

// Rewriting a block's height is caught by the height-sequence check.
func TestValidateDetectsBrokenHeightSequence(t *testing.T) {
	c := NewChain()
	c.AddBlock(txs(tx("alice", "bob", 10, 0)))
	c.AddBlock(txs(tx("bob", "carol", 5, 0)))

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
	c.AddBlock(txs(tx("alice", "bob", 1, 0)))
}
