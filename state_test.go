package blockchain

import (
	"crypto/ed25519"
	"testing"
)

func block(txs ...Transaction) Block {
	return Block{Transactions: txs}
}

// wallet is a test fixture: a key pair plus its derived address.
// The private key never leaves the test, exactly as in a real system.
type wallet struct {
	pub  ed25519.PublicKey
	priv ed25519.PrivateKey
	addr string
}

func newWallet(t *testing.T) wallet {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return wallet{pub: pub, priv: priv, addr: deriveAddress(pub)}
}

// send builds a transaction from this wallet and signs it.
func (w wallet) send(t *testing.T, to string, amount uint64, nonce int64) Transaction {
	t.Helper()
	tx := Transaction{From: w.addr, To: to, Amount: amount, Nonce: nonce}
	tx.Signature = tx.Sign(w.priv)
	return tx
}

// NewState seeds balances from the allocation and leaves nonces empty
// (a missing nonce reads as 0, which is the correct starting value).
func TestNewStateSeedsBalances(t *testing.T) {
	s := NewState(map[string]uint64{"alice": 1000, "bob": 250})

	if s.Balances["alice"] != 1000 {
		t.Errorf("alice balance: got %d, want 1000", s.Balances["alice"])
	}
	if s.Balances["bob"] != 250 {
		t.Errorf("bob balance: got %d, want 250", s.Balances["bob"])
	}
	if s.Nonces["alice"] != 0 {
		t.Errorf("alice nonce: got %d, want 0", s.Nonces["alice"])
	}
	if len(s.Nonces) != 0 {
		t.Errorf("nonces map should start empty, got %d entries", len(s.Nonces))
	}
}

// Mutating the allocation map after construction must not affect the state.
func TestNewStateCopiesAllocation(t *testing.T) {
	alloc := map[string]uint64{"alice": 1000}
	s := NewState(alloc)

	alloc["alice"] = 999999
	alloc["mallory"] = 1

	if s.Balances["alice"] != 1000 {
		t.Errorf("state balance changed with the alloc map: got %d, want 1000", s.Balances["alice"])
	}
	if _, ok := s.Balances["mallory"]; ok {
		t.Error("new alloc key leaked into state")
	}
}

// A valid signed transfer moves balance and advances the sender's nonce.
func TestApplyValidTransfer(t *testing.T) {
	alice := newWallet(t)
	bob := newWallet(t)
	s := NewState(map[string]uint64{alice.addr: 1000})

	next, err := Apply(s, block(alice.send(t, bob.addr, 100, 0)))
	if err != nil {
		t.Fatalf("valid transfer rejected: %v", err)
	}

	if next.Balances[alice.addr] != 900 {
		t.Errorf("alice balance: got %d, want 900", next.Balances[alice.addr])
	}
	if next.Balances[bob.addr] != 100 {
		t.Errorf("bob balance: got %d, want 100", next.Balances[bob.addr])
	}
	if next.Nonces[alice.addr] != 1 {
		t.Errorf("alice nonce: got %d, want 1", next.Nonces[alice.addr])
	}
}

// Apply must not mutate the state it was given, only the returned copy.
func TestApplyDoesNotMutateInput(t *testing.T) {
	alice := newWallet(t)
	bob := newWallet(t)
	s := NewState(map[string]uint64{alice.addr: 1000})

	if _, err := Apply(s, block(alice.send(t, bob.addr, 100, 0))); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if s.Balances[alice.addr] != 1000 {
		t.Errorf("input state was mutated: alice balance is %d, want 1000", s.Balances[alice.addr])
	}
	if s.Nonces[alice.addr] != 0 {
		t.Errorf("input state was mutated: alice nonce is %d, want 0", s.Nonces[alice.addr])
	}
}

// Later transactions in a block see the effects of earlier ones.
func TestApplyChainedTransactionsInOneBlock(t *testing.T) {
	alice := newWallet(t)
	bob := newWallet(t)
	carol := newWallet(t)
	s := NewState(map[string]uint64{alice.addr: 100})

	// alice -> bob 100, then bob -> carol 100. The second only works
	// if the first already credited bob.
	next, err := Apply(s, block(
		alice.send(t, bob.addr, 100, 0),
		bob.send(t, carol.addr, 100, 0),
	))
	if err != nil {
		t.Fatalf("chained transactions rejected: %v", err)
	}

	if next.Balances[alice.addr] != 0 || next.Balances[bob.addr] != 0 || next.Balances[carol.addr] != 100 {
		t.Errorf("balances: alice=%d bob=%d carol=%d, want 0/0/100",
			next.Balances[alice.addr], next.Balances[bob.addr], next.Balances[carol.addr])
	}
}

// If any transaction in a block is invalid, the whole block is rejected
// and no earlier transaction from that block is applied.
func TestApplyRejectsWholeBlockOnLaterFailure(t *testing.T) {
	alice := newWallet(t)
	bob := newWallet(t)
	carol := newWallet(t)
	s := NewState(map[string]uint64{alice.addr: 1000})

	_, err := Apply(s, block(
		alice.send(t, bob.addr, 100, 0),   // valid
		alice.send(t, carol.addr, 100, 5), // signed, but wrong nonce
	))
	if err == nil {
		t.Fatal("block with an invalid transaction should be rejected")
	}
}

// Account-rule rejections: the transaction is correctly signed, but breaks
// a nonce or balance rule, or is structurally invalid.
func TestApplyAccountRejections(t *testing.T) {
	alice := newWallet(t)
	bob := newWallet(t)
	stranger := newWallet(t) // has a valid key but no allocation

	cases := []struct {
		name string
		tx   Transaction
	}{
		{"insufficient balance", alice.send(t, bob.addr, 500, 0)},
		{"wrong nonce", alice.send(t, bob.addr, 10, 7)},
		{"zero amount", alice.send(t, bob.addr, 0, 0)},
		{"self send", alice.send(t, alice.addr, 10, 0)},
		{"unknown sender has no funds", stranger.send(t, bob.addr, 10, 0)},
		{"empty sender", Transaction{From: "", To: bob.addr, Amount: 10, Nonce: 0}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewState(map[string]uint64{alice.addr: 100})
			if _, err := Apply(s, block(tc.tx)); err == nil {
				t.Errorf("%s: expected rejection, got nil error", tc.name)
			}
		})
	}
}

// Signature rejections: the transaction fails authentication.
func TestApplySignatureRejections(t *testing.T) {
	alice := newWallet(t)
	bob := newWallet(t)
	mallory := newWallet(t)

	t.Run("missing signature", func(t *testing.T) {
		s := NewState(map[string]uint64{alice.addr: 100})
		tx := Transaction{From: alice.addr, To: bob.addr, Amount: 10, Nonce: 0}
		if _, err := Apply(s, block(tx)); err == nil {
			t.Error("unsigned transaction should be rejected")
		}
	})

	t.Run("garbage signature", func(t *testing.T) {
		s := NewState(map[string]uint64{alice.addr: 100})
		tx := Transaction{From: alice.addr, To: bob.addr, Amount: 10, Nonce: 0}
		tx.Signature = make([]byte, ed25519.SignatureSize) // right length, all zero
		if _, err := Apply(s, block(tx)); err == nil {
			t.Error("garbage signature should be rejected")
		}
	})

	t.Run("signed by wrong key", func(t *testing.T) {
		s := NewState(map[string]uint64{alice.addr: 100})
		tx := Transaction{From: alice.addr, To: bob.addr, Amount: 10, Nonce: 0}
		tx.Signature = tx.Sign(mallory.priv) // mallory signs a tx claiming to be from alice
		if _, err := Apply(s, block(tx)); err == nil {
			t.Error("transaction signed by the wrong key should be rejected")
		}
	})

	t.Run("tampered after signing", func(t *testing.T) {
		s := NewState(map[string]uint64{alice.addr: 1000})
		tx := alice.send(t, bob.addr, 10, 0)
		tx.Amount = 900 // change the amount after the signature was produced
		if _, err := Apply(s, block(tx)); err == nil {
			t.Error("transaction altered after signing should be rejected")
		}
	})
}

// Nonce enforcement across successive blocks: 0 then 1 works, replaying 0 fails.
func TestApplyNonceSequenceAcrossBlocks(t *testing.T) {
	alice := newWallet(t)
	bob := newWallet(t)
	s := NewState(map[string]uint64{alice.addr: 1000})

	s, err := Apply(s, block(alice.send(t, bob.addr, 10, 0)))
	if err != nil {
		t.Fatalf("first transfer (nonce 0) rejected: %v", err)
	}

	s, err = Apply(s, block(alice.send(t, bob.addr, 10, 1)))
	if err != nil {
		t.Fatalf("second transfer (nonce 1) rejected: %v", err)
	}

	// A freshly signed transaction that reuses nonce 0 must still be rejected.
	if _, err = Apply(s, block(alice.send(t, bob.addr, 10, 0))); err == nil {
		t.Fatal("replaying nonce 0 should be rejected")
	}
}
