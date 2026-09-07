package chain

import "testing"

// Stepping an election matches ProposerForHeight height by height on an
// unslashed set: the incremental accumulator and the recompute-from-1
// reference are the same algorithm.
func TestElectionMatchesReference(t *testing.T) {
	set := NewValidatorSet(GenerateValidators(11)...) // skewed stakes
	e := newElection(set)

	for h := int64(1); h <= 200; h++ {
		got := e.next()
		want := set.ProposerForHeight(h).address
		if got != want {
			t.Fatalf("height %d: election %s, reference %s", h, got, want)
		}
	}
}

// A slash takes effect on the exact height it is scheduled for, and not
// before: past heights keep their proposer, and the slashed validator is
// skipped from the effective height on.
func TestElectionSlashIsNotRetroactive(t *testing.T) {
	set := NewValidatorSet(
		NewValidator(newWallet(t).priv, 1),
		NewValidator(newWallet(t).priv, 1),
		NewValidator(newWallet(t).priv, 1),
	)
	target := set.members[1].address

	before := newElection(set)
	after := newElection(set)

	var beforeSeq, afterSeq []string
	for h := int64(1); h <= 12; h++ {
		beforeSeq = append(beforeSeq, before.next())
		if h == 5 {
			after.slash(target) // effective from height 5
		}
		afterSeq = append(afterSeq, after.next())
	}

	// Heights 1-4 are computed before the slash on both, so they match.
	for h := 0; h < 4; h++ {
		if beforeSeq[h] != afterSeq[h] {
			t.Errorf("height %d changed by a later slash: %s vs %s", h+1, beforeSeq[h], afterSeq[h])
		}
	}
	// From height 5 the slashed validator never proposes.
	for h := 4; h < 12; h++ {
		if afterSeq[h] == target {
			t.Errorf("slashed validator proposed at height %d", h+1)
		}
	}
}
