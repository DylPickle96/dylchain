package chain

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// signableBytes is the exact cross-language signing contract: the four
// authorised fields concatenated with 0x00 separators and 8-byte
// big-endian integers, Signature excluded.
func TestSignableBytes(t *testing.T) {
	tx := Transaction{
		From:      "dylAAAA",
		To:        "dylBBBB",
		Amount:    258,
		Nonce:     1,
		Signature: []byte("ignored"),
	}

	want := []byte("dylAAAA\x00dylBBBB\x00")
	want = binary.BigEndian.AppendUint64(want, 258)
	want = binary.BigEndian.AppendUint64(want, 1)

	if got := tx.signableBytes(); !bytes.Equal(got, want) {
		t.Errorf("signableBytes\n got %x\nwant %x", got, want)
	}

	// The signature field does not affect it.
	tx2 := tx
	tx2.Signature = nil
	if !bytes.Equal(tx.signableBytes(), tx2.signableBytes()) {
		t.Error("signableBytes depends on the Signature field")
	}

	// Splitting a field across the separator changes the bytes.
	a := Transaction{From: "ab", To: "c"}
	b := Transaction{From: "a", To: "bc"}
	if bytes.Equal(a.signableBytes(), b.signableBytes()) {
		t.Error(`("ab","c") and ("a","bc") collide`)
	}
}
