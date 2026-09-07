package chain

import "testing"

// A block signed by its proposer verifies against the proposer's address.
func TestBlockSignatureRoundTrip(t *testing.T) {
	proposer := newWallet(t)
	b := Block{Height: 1, CreatedAt: 100, Proposer: proposer.addr}
	b.Signature = b.sign(proposer.priv)

	if err := verifyBlockSignature(b); err != nil {
		t.Fatalf("a freshly signed block should verify: %v", err)
	}
}

// The signature covers the whole header, so changing any header field
// after signing breaks it.
func TestVerifyBlockSignatureRejectsTamperedHeader(t *testing.T) {
	proposer := newWallet(t)
	b := Block{Height: 1, CreatedAt: 100, Proposer: proposer.addr}
	b.Signature = b.sign(proposer.priv)

	b.CreatedAt = 101

	if err := verifyBlockSignature(b); err == nil {
		t.Fatal("a tampered header should fail signature verification, got nil")
	}
}

// A signature made with one key does not verify under another address.
func TestVerifyBlockSignatureRejectsWrongProposer(t *testing.T) {
	signer := newWallet(t)
	other := newWallet(t)
	b := Block{Height: 1, CreatedAt: 100, Proposer: other.addr}
	b.Signature = b.sign(signer.priv)

	if err := verifyBlockSignature(b); err == nil {
		t.Fatal("signature by a different key should not verify, got nil")
	}
}

// A proposer field that is not a valid address is rejected before any
// signature check.
func TestVerifyBlockSignatureRejectsBadProposerAddress(t *testing.T) {
	b := Block{Height: 1, Proposer: "not-an-address", Signature: []byte("x")}

	if err := verifyBlockSignature(b); err == nil {
		t.Fatal("a malformed proposer address should be rejected, got nil")
	}
}
