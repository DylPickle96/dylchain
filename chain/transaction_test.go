package chain

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// signableBytes is the exact cross-language signing contract: kind, from,
// to concatenated with 0x00 separators, then amount and nonce as 8-byte
// big-endian integers, Signature excluded.
func TestSignableBytes(t *testing.T) {
	tx := Transaction{
		Kind:      KindDelegate,
		From:      "dylAAAA",
		To:        "dylBBBB",
		Amount:    258,
		Nonce:     1,
		Signature: []byte("ignored"),
	}

	want := []byte("delegate\x00dylAAAA\x00dylBBBB\x00")
	want = binary.BigEndian.AppendUint64(want, 258)
	want = binary.BigEndian.AppendUint64(want, 1)

	if got := tx.signableBytes(); !bytes.Equal(got, want) {
		t.Errorf("signableBytes\n got %x\nwant %x", got, want)
	}

	// A transfer has the empty kind, so its bytes start with a 0x00.
	transfer := tx
	transfer.Kind = KindTransfer
	if got := transfer.signableBytes(); got[0] != 0 {
		t.Errorf("transfer signableBytes should start with the empty-kind separator, got %x", got[:1])
	}

	// The signature field does not affect it.
	tx2 := tx
	tx2.Signature = nil
	if !bytes.Equal(tx.signableBytes(), tx2.signableBytes()) {
		t.Error("signableBytes depends on the Signature field")
	}

	// Kind is signed: a delegate and a transfer with the same fields differ.
	if bytes.Equal(tx.signableBytes(), transfer.signableBytes()) {
		t.Error("kind does not affect signableBytes")
	}

	// Splitting a field across a separator changes the bytes.
	a := Transaction{From: "ab", To: "c"}
	b := Transaction{From: "a", To: "bc"}
	if bytes.Equal(a.signableBytes(), b.signableBytes()) {
		t.Error(`("ab","c") and ("a","bc") collide`)
	}
	c := Transaction{Kind: "x", From: "", To: "y"}
	d := Transaction{Kind: "", From: "x", To: "y"}
	if bytes.Equal(c.signableBytes(), d.signableBytes()) {
		t.Error(`("x","","y") and ("","x","y") collide`)
	}
}

// Hash is the Merkle leaf: it changes with every field including the
// signature, and a single-transaction block's root is exactly that hash.
func TestTransactionHash(t *testing.T) {
	a := Transaction{From: "dylA", To: "dylB", Amount: 1, Nonce: 0, Signature: []byte{1}}
	b := a
	b.Signature = []byte{2}
	if bytes.Equal(a.Hash(), b.Hash()) {
		t.Error("hash ignores the signature")
	}
	if len(a.Hash()) != 32 {
		t.Errorf("hash length %d, want 32", len(a.Hash()))
	}
	if !bytes.Equal(merkleRoot([]Transaction{a}), a.Hash()) {
		t.Error("single-tx merkle root is not the tx hash")
	}
}
