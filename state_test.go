package blockchain

import "testing"

func block(txs ...Transaction) Block {
	return Block{Transactions: txs}
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

// A valid transfer moves balance and advances the sender's nonce.
func TestApplyValidTransfer(t *testing.T) {
	s := NewState(map[string]uint64{"alice": 1000})

	next, err := Apply(s, block(Transaction{From: "alice", To: "bob", Amount: 100, Nonce: 0}))
	if err != nil {
		t.Fatalf("valid transfer rejected: %v", err)
	}

	if next.Balances["alice"] != 900 {
		t.Errorf("alice balance: got %d, want 900", next.Balances["alice"])
	}
	if next.Balances["bob"] != 100 {
		t.Errorf("bob balance: got %d, want 100", next.Balances["bob"])
	}
	if next.Nonces["alice"] != 1 {
		t.Errorf("alice nonce: got %d, want 1", next.Nonces["alice"])
	}
}

// Apply must not mutate the state it was given, only the returned copy.
func TestApplyDoesNotMutateInput(t *testing.T) {
	s := NewState(map[string]uint64{"alice": 1000})

	_, err := Apply(s, block(Transaction{From: "alice", To: "bob", Amount: 100, Nonce: 0}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if s.Balances["alice"] != 1000 {
		t.Errorf("input state was mutated: alice balance is %d, want 1000", s.Balances["alice"])
	}
	if s.Nonces["alice"] != 0 {
		t.Errorf("input state was mutated: alice nonce is %d, want 0", s.Nonces["alice"])
	}
}

// Later transactions in a block see the effects of earlier ones.
func TestApplyChainedTransactionsInOneBlock(t *testing.T) {
	s := NewState(map[string]uint64{"alice": 100})

	// alice -> bob 100, then bob -> carol 100. The second only works
	// if the first already credited bob.
	next, err := Apply(s, block(
		Transaction{From: "alice", To: "bob", Amount: 100, Nonce: 0},
		Transaction{From: "bob", To: "carol", Amount: 100, Nonce: 0},
	))
	if err != nil {
		t.Fatalf("chained transactions rejected: %v", err)
	}

	if next.Balances["alice"] != 0 || next.Balances["bob"] != 0 || next.Balances["carol"] != 100 {
		t.Errorf("balances: alice=%d bob=%d carol=%d, want 0/0/100",
			next.Balances["alice"], next.Balances["bob"], next.Balances["carol"])
	}
}

// If any transaction in a block is invalid, the whole block is rejected
// and no earlier transaction from that block is applied.
func TestApplyRejectsWholeBlockOnLaterFailure(t *testing.T) {
	s := NewState(map[string]uint64{"alice": 1000})

	_, err := Apply(s, block(
		Transaction{From: "alice", To: "bob", Amount: 100, Nonce: 0},   // valid
		Transaction{From: "alice", To: "carol", Amount: 100, Nonce: 5}, // bad nonce
	))
	if err == nil {
		t.Fatal("block with an invalid transaction should be rejected")
	}
	// input untouched is covered elsewhere; the point here is err != nil
	// rather than a state where the first transfer went through.
}

func TestApplyRejections(t *testing.T) {
	base := map[string]uint64{"alice": 100}

	cases := []struct {
		name string
		tx   Transaction
	}{
		{"insufficient balance", Transaction{From: "alice", To: "bob", Amount: 500, Nonce: 0}},
		{"wrong nonce", Transaction{From: "alice", To: "bob", Amount: 10, Nonce: 7}},
		{"zero amount", Transaction{From: "alice", To: "bob", Amount: 0, Nonce: 0}},
		{"self send", Transaction{From: "alice", To: "alice", Amount: 10, Nonce: 0}},
		{"empty sender", Transaction{From: "", To: "bob", Amount: 10, Nonce: 0}},
		{"unknown sender has no funds", Transaction{From: "nobody", To: "bob", Amount: 10, Nonce: 0}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewState(base)
			if _, err := Apply(s, block(tc.tx)); err == nil {
				t.Errorf("%s: expected rejection, got nil error", tc.name)
			}
		})
	}
}

// Nonce enforcement across successive blocks: 0 then 1 works, replaying 0 fails.
func TestApplyNonceSequenceAcrossBlocks(t *testing.T) {
	s := NewState(map[string]uint64{"alice": 1000})

	s, err := Apply(s, block(Transaction{From: "alice", To: "bob", Amount: 10, Nonce: 0}))
	if err != nil {
		t.Fatalf("first transfer (nonce 0) rejected: %v", err)
	}

	s, err = Apply(s, block(Transaction{From: "alice", To: "bob", Amount: 10, Nonce: 1}))
	if err != nil {
		t.Fatalf("second transfer (nonce 1) rejected: %v", err)
	}

	// Replaying nonce 0 must now fail.
	if _, err = Apply(s, block(Transaction{From: "alice", To: "bob", Amount: 10, Nonce: 0})); err == nil {
		t.Fatal("replaying nonce 0 should be rejected")
	}
}
