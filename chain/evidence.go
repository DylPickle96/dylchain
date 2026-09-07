package chain

import "bytes"

// Evidence is two votes signed by the same validator at the same height for
// different blocks: proof that Offender equivocated. It is what slashing
// acts on.
type Evidence struct {
	Offender string
	Height   int64
	VoteA    vote
	VoteB    vote
}

// verifyEvidence confirms the evidence genuinely shows equivocation: both
// votes validly signed by Offender, both at Height, and for different
// blocks.
func verifyEvidence(e Evidence) bool {
	return e.VoteA.voter == e.Offender && e.VoteB.voter == e.Offender &&
		e.VoteA.height == e.Height && e.VoteB.height == e.Height &&
		!bytes.Equal(e.VoteA.blockHash, e.VoteB.blockHash) &&
		verifyVote(e.VoteA) && verifyVote(e.VoteB)
}
