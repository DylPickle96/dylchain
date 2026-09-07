package chain

import "testing"

// verifyEvidence accepts a genuine double-vote and rejects the ways a piece
// of evidence can be malformed or forged.
func TestVerifyEvidence(t *testing.T) {
	v := NewValidator(newWallet(t).priv, 1)
	a := newVote(5, []byte("block-a"), v)
	b := newVote(5, []byte("block-b"), v)

	good := Evidence{Offender: v.address, Height: 5, VoteA: a, VoteB: b}
	if !verifyEvidence(good) {
		t.Fatal("a real double-vote should verify as evidence")
	}

	t.Run("same block", func(t *testing.T) {
		e := Evidence{Offender: v.address, Height: 5, VoteA: a, VoteB: newVote(5, []byte("block-a"), v)}
		if verifyEvidence(e) {
			t.Fatal("two votes for the same block are not equivocation")
		}
	})

	t.Run("wrong offender", func(t *testing.T) {
		other := NewValidator(newWallet(t).priv, 1)
		e := Evidence{Offender: other.address, Height: 5, VoteA: a, VoteB: b}
		if verifyEvidence(e) {
			t.Fatal("evidence naming someone who did not sign should not verify")
		}
	})

	t.Run("height mismatch", func(t *testing.T) {
		e := Evidence{Offender: v.address, Height: 5, VoteA: a, VoteB: newVote(6, []byte("block-c"), v)}
		if verifyEvidence(e) {
			t.Fatal("a vote at another height is not equivocation at this one")
		}
	})

	t.Run("tampered signature", func(t *testing.T) {
		bad := b
		bad.signature = append([]byte(nil), b.signature...)
		bad.signature[0] ^= 0xff
		e := Evidence{Offender: v.address, Height: 5, VoteA: a, VoteB: bad}
		if verifyEvidence(e) {
			t.Fatal("evidence containing an unverifiable vote should not verify")
		}
	})
}
