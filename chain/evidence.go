package chain

import "bytes"

type Evidence struct {
	Offender string
	Height   int64
	VoteA    vote
	VoteB    vote
}

func verifyEvidence(e Evidence) bool {
	return e.VoteA.voter == e.Offender && e.VoteB.voter == e.Offender &&
		e.VoteA.height == e.Height && e.VoteB.height == e.Height &&
		!bytes.Equal(e.VoteA.blockHash, e.VoteB.blockHash) &&
		verifyVote(e.VoteA) && verifyVote(e.VoteB)
}
