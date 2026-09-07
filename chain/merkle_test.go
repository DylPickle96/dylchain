package chain

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
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

func numberedTxs(n int) []Transaction {
	out := make([]Transaction, n)
	for i := 0; i < n; i++ {
		out[i] = tx("alice", "bob", uint64(i+1), int64(i))
	}
	return out
}

func TestMerkleProofEmpty(t *testing.T) {
	if _, err := merkleProof(nil, 0); err == nil {
		t.Error("empty txs: expected error")
	}
	if _, err := merkleProof([]Transaction{}, 0); err == nil {
		t.Error("zero-length txs: expected error")
	}
}

func TestMerkleProofBadIndex(t *testing.T) {
	txs := numberedTxs(2)
	for _, index := range []int{-1, 2, 3} {
		if _, err := merkleProof(txs, index); err == nil {
			t.Errorf("index %d: expected error", index)
		}
	}
}

func TestMerkleProofSingleEmpty(t *testing.T) {
	a := tx("alice", "bob", 10, 0)
	proof, err := merkleProof([]Transaction{a}, 0)
	if err != nil {
		t.Fatalf("merkleProof: %v", err)
	}
	if len(proof) != 0 {
		t.Errorf("single-tx proof: got %d siblings, want none", len(proof))
	}
	if !verifyMerkleProof(leafHash(t, a), 0, proof, merkleRoot([]Transaction{a})) {
		t.Error("empty proof should verify against the leaf root")
	}
}

// Three leaves, last index: sibling is the leaf itself, then AB.
func TestMerkleProofOddLastKnownValue(t *testing.T) {
	a := tx("alice", "bob", 10, 0)
	b := tx("bob", "carol", 5, 0)
	c := tx("carol", "alice", 1, 0)
	txs := []Transaction{a, b, c}

	proof, err := merkleProof(txs, 2)
	if err != nil {
		t.Fatalf("merkleProof: %v", err)
	}
	la, lb, lc := leafHash(t, a), leafHash(t, b), leafHash(t, c)
	want := [][]byte{lc, nodeHash(la, lb)}
	if len(proof) != len(want) {
		t.Fatalf("proof length: got %d, want %d", len(proof), len(want))
	}
	for i := range want {
		if !bytes.Equal(proof[i], want[i]) {
			t.Errorf("proof[%d]: got %x, want %x", i, proof[i], want[i])
		}
	}
}

func TestMerkleProofVerifiesEveryIndex(t *testing.T) {
	for n := 1; n <= 5; n++ {
		txs := numberedTxs(n)
		root := merkleRoot(txs)
		t.Run(fmt.Sprintf("%d txs", n), func(t *testing.T) {
			for i := range txs {
				proof, err := merkleProof(txs, i)
				if err != nil {
					t.Fatalf("index %d: %v", i, err)
				}
				if !verifyMerkleProof(leafHash(t, txs[i]), i, proof, root) {
					t.Errorf("index %d: proof did not verify", i)
				}
			}
		})
	}
}

func TestMerkleProofAlteredSiblingFails(t *testing.T) {
	txs := numberedTxs(4)
	proof, err := merkleProof(txs, 1)
	if err != nil {
		t.Fatalf("merkleProof: %v", err)
	}
	if len(proof) == 0 {
		t.Fatal("expected at least one sibling")
	}
	proof[0] = bytes.Clone(proof[0])
	proof[0][0] ^= 1
	if verifyMerkleProof(leafHash(t, txs[1]), 1, proof, merkleRoot(txs)) {
		t.Error("proof with an altered sibling verified")
	}
}

func TestMerkleProofWrongRootFails(t *testing.T) {
	txs := numberedTxs(3)
	proof, err := merkleProof(txs, 0)
	if err != nil {
		t.Fatalf("merkleProof: %v", err)
	}
	wrong := bytes.Clone(merkleRoot(txs))
	wrong[0] ^= 1
	if verifyMerkleProof(leafHash(t, txs[0]), 0, proof, wrong) {
		t.Error("proof verified against a wrong root")
	}
}

func TestMerkleProofWrongIndexFails(t *testing.T) {
	txs := numberedTxs(4)
	proof, err := merkleProof(txs, 1)
	if err != nil {
		t.Fatalf("merkleProof: %v", err)
	}
	if verifyMerkleProof(leafHash(t, txs[1]), 0, proof, merkleRoot(txs)) {
		t.Error("proof for index 1 verified when checked as index 0")
	}
}

func TestMerkleProofAgainstBlockTxRoot(t *testing.T) {
	txs := numberedTxs(5)
	b := Block{
		Transactions: txs,
		TxRoot:       merkleRoot(txs),
	}
	for i := range b.Transactions {
		proof, err := merkleProof(b.Transactions, i)
		if err != nil {
			t.Fatalf("index %d: %v", i, err)
		}
		if !verifyMerkleProof(leafHash(t, b.Transactions[i]), i, proof, b.TxRoot) {
			t.Errorf("index %d: proof did not match Block.TxRoot", i)
		}
	}
}
