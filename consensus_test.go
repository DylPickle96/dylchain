package dyl

import "testing"

// A vote signed by a validator verifies against that validator's address.
func TestVoteRoundTrip(t *testing.T) {
	v := NewValidator(newWallet(t).priv, 1)
	vt := newVote(5, []byte("block-hash"), v)

	if !verifyVote(vt) {
		t.Fatal("a freshly signed vote should verify")
	}
}

// A vote does not verify once any signed field is changed.
func TestVoteRejectsTampering(t *testing.T) {
	v := NewValidator(newWallet(t).priv, 1)

	t.Run("wrong voter", func(t *testing.T) {
		vt := newVote(5, []byte("block-hash"), v)
		vt.voter = NewValidator(newWallet(t).priv, 1).address
		if verifyVote(vt) {
			t.Fatal("vote verified under a different voter address")
		}
	})

	t.Run("changed height", func(t *testing.T) {
		vt := newVote(5, []byte("block-hash"), v)
		vt.height = 6
		if verifyVote(vt) {
			t.Fatal("vote verified after its height changed")
		}
	})

	t.Run("changed block hash", func(t *testing.T) {
		vt := newVote(5, []byte("block-hash"), v)
		vt.blockHash = []byte("other-hash")
		if verifyVote(vt) {
			t.Fatal("vote verified after its block hash changed")
		}
	})
}

// msgHeight reads the height out of either message kind.
func TestMsgHeight(t *testing.T) {
	if got := msgHeight(proposalMsg{block: Block{Height: 7}}); got != 7 {
		t.Errorf("proposalMsg height: got %d, want 7", got)
	}
	if got := msgHeight(voteMsg{vote: vote{height: 9}}); got != 9 {
		t.Errorf("voteMsg height: got %d, want 9", got)
	}
}
