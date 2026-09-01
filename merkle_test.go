package blockchain

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"testing"
)

// These two mirror the leaf and node hashing by hand, with the prefix
// bytes hardcoded rather than read from the constants, so a change to the
// construction (prefixes, order, structure) breaks the known-value tests
// below instead of silently changing every root.
func leafHash(t *testing.T, tx Transaction) []byte {
	t.Helper()
	data, err := json.Marshal(tx)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	h := sha256.New()
	h.Write([]byte{0x00})
	h.Write(data)
	return h.Sum(nil)
}

func nodeHash(l, r []byte) []byte {
	h := sha256.New()
	h.Write([]byte{0x01})
	h.Write(l)
	h.Write(r)
	return h.Sum(nil)
}

func TestMerkleRootEmpty(t *testing.T) {
	root := merkleRoot(nil)
	if !bytes.Equal(root, make([]byte, sha256.Size)) {
		t.Errorf("empty root: got %x, want 32 zero bytes", root)
	}
	if !bytes.Equal(merkleRoot([]Transaction{}), merkleRoot(nil)) {
		t.Error("nil and empty slice should give the same root")
	}
}

func TestMerkleRootSingle(t *testing.T) {
	a := tx("alice", "bob", 10, 0)

	got := merkleRoot([]Transaction{a})
	want := leafHash(t, a) // a lone transaction's root is just its leaf hash

	if !bytes.Equal(got, want) {
		t.Errorf("single root: got %x, want %x", got, want)
	}
}

func TestMerkleRootPair(t *testing.T) {
	a := tx("alice", "bob", 10, 0)
	b := tx("bob", "carol", 5, 0)

	got := merkleRoot([]Transaction{a, b})
	want := nodeHash(leafHash(t, a), leafHash(t, b))

	if !bytes.Equal(got, want) {
		t.Errorf("pair root: got %x, want %x", got, want)
	}
}

// Three leaves: the last is duplicated to pair with itself, then the two
// parents are combined.
func TestMerkleRootOddDuplicatesLast(t *testing.T) {
	a := tx("alice", "bob", 10, 0)
	b := tx("bob", "carol", 5, 0)
	c := tx("carol", "alice", 1, 0)

	got := merkleRoot([]Transaction{a, b, c})

	la, lb, lc := leafHash(t, a), leafHash(t, b), leafHash(t, c)
	want := nodeHash(nodeHash(la, lb), nodeHash(lc, lc))

	if !bytes.Equal(got, want) {
		t.Errorf("odd root: got %x, want %x", got, want)
	}
}

func TestMerkleRootChangesWhenTransactionChanges(t *testing.T) {
	a := tx("alice", "bob", 10, 0)
	b := tx("bob", "carol", 5, 0)

	before := merkleRoot([]Transaction{a, b})
	b.Amount = 6
	after := merkleRoot([]Transaction{a, b})

	if bytes.Equal(before, after) {
		t.Error("root did not change when a transaction was modified")
	}
}

func TestMerkleRootDependsOnOrder(t *testing.T) {
	a := tx("alice", "bob", 10, 0)
	b := tx("bob", "carol", 5, 0)

	ab := merkleRoot([]Transaction{a, b})
	ba := merkleRoot([]Transaction{b, a})

	if bytes.Equal(ab, ba) {
		t.Error("root should depend on transaction order")
	}
}

func TestMerkleRootThreeVsFourDiffer(t *testing.T) {
	a := tx("alice", "bob", 10, 0)
	b := tx("bob", "carol", 5, 0)
	c := tx("carol", "alice", 1, 0)
	d := tx("dave", "erin", 2, 0)

	three := merkleRoot([]Transaction{a, b, c})
	four := merkleRoot([]Transaction{a, b, c, d})

	if bytes.Equal(three, four) {
		t.Error("padding a 3-transaction block with a real 4th transaction should change the root")
	}
}

func TestMerkleRootDeterministic(t *testing.T) {
	txs := []Transaction{
		tx("alice", "bob", 10, 0),
		tx("bob", "carol", 5, 0),
		tx("carol", "alice", 1, 0),
	}

	if !bytes.Equal(merkleRoot(txs), merkleRoot(txs)) {
		t.Error("merkleRoot is not deterministic for the same input")
	}
}
