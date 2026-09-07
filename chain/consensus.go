package chain

import (
	"bytes"
	"crypto/ed25519"
	"encoding/binary"
	"fmt"
	"time"
)

// This file implements one-step-vote BFT consensus for a single process.

// message is anything that travels on the bus.
type message interface {
	isMessage()
}

// proposalMsg carries a proposed block; voteMsg carries one validator's
// vote. These are the only two things that cross the bus.
type proposalMsg struct {
	block Block
}
type voteMsg struct {
	vote vote
}

// isMessage marks the two types allowed on the bus. It carries no data.
func (proposalMsg) isMessage() {}
func (voteMsg) isMessage()     {}

// vote is a validator's signed statement that it accepts a specific block
// at a specific height. One-step consensus has a single kind of vote: no
// separate prevote and precommit.
type vote struct {
	height    int64
	blockHash []byte
	voter     string
	signature []byte
}

// voteSignableBytes is the deterministic payload a vote signature covers:
// the height as 8 big-endian bytes, followed by the block hash.
func voteSignableBytes(height int64, blockHash []byte) []byte {
	b := make([]byte, 8, 8+len(blockHash))
	binary.BigEndian.PutUint64(b, uint64(height))
	return append(b, blockHash...)
}

// newVote signs voteSignableBytes(height, blockHash) with v's key and
// returns the vote.
func newVote(height int64, blockHash []byte, v Validator) vote {
	return vote{
		height:    height,
		blockHash: blockHash,
		voter:     v.address,
		signature: ed25519.Sign(v.privKey, voteSignableBytes(height, blockHash)),
	}
}

// verifyVote recovers the voter's public key from its address and checks
// the signature over voteSignableBytes.
func verifyVote(v vote) bool {
	pub, err := pubKeyFromAddress(v.voter)
	if err != nil {
		return false
	}
	return ed25519.Verify(pub, voteSignableBytes(v.height, v.blockHash), v.signature)
}

// bus is an in-process broadcast network: every message sent is delivered
// to every node's inbox. Inboxes are buffered well past what one run
// produces, so a broadcast never blocks the sender.
type bus struct {
	inboxes []chan message
}

// newBus builds a bus with one buffered inbox per node. Each height puts
// about one proposal plus one vote per node into every inbox, so the buffer
// scales with the node count to leave a node room to fall several heights
// behind before a broadcast blocks.
func newBus(nodes int) *bus {
	buf := 1024
	if n := 16 * nodes; n > buf {
		buf = n
	}
	b := &bus{inboxes: make([]chan message, nodes)}
	for i := range b.inboxes {
		b.inboxes[i] = make(chan message, buf)
	}
	return b
}

// broadcast delivers m to every inbox.
func (b *bus) broadcast(m message) {
	for _, inbox := range b.inboxes {
		inbox <- m
	}
}

// node is one validator running the consensus loop against its own copy of
// the chain.
type node struct {
	self        Validator
	set         *ValidatorSet
	chain       *Chain
	mempool     *Mempool
	inbox       chan message
	bus         *bus
	maxBlockTxs int
	pending     []message
	seenVotes   map[int64]map[string]vote
	evidence    []Evidence
	byzantine   bool
}

// run drives the node through heights 1..heights, one block per height.
func (n *node) run(heights int64) {
	for h := int64(1); h <= heights; h++ {
		n.runHeight(h)
		n.prunePending(h)
	}
}

// runHeight is one round of one-step consensus: propose if it is our turn,
// wait for the proposal and vote for it, tally votes by stake until more
// than two thirds of stake accepts, then commit. Commit on
// 3*accepted > 2*total (strictly more than two thirds, integer-safe).
func (n *node) runHeight(h int64) {
	// 1. If it is our turn, build a block and broadcast it.
	if n.set.ProposerForHeight(h).address == n.self.address {
		n.bus.broadcast(proposalMsg{block: n.propose(h)})
	}

	// 2. Wait for this height's proposal and check it against our chain.
	candidate := n.waitFor(func(m message) bool {
		pm, ok := m.(proposalMsg)
		return ok && pm.block.Height == h
	}).(proposalMsg).block

	if _, err := n.chain.checkCandidate(candidate); err != nil {
		panic(fmt.Sprintf("validator %s rejected the block at height %d: %v", n.self.address, h, err))
	}

	// 3. Broadcast our vote for it.
	blockHash := candidate.Hash()
	n.bus.broadcast(voteMsg{vote: newVote(h, blockHash, n.self)})
	if n.byzantine {
		n.bus.broadcast(voteMsg{vote: newVote(h, []byte("equivocation"), n.self)})
	}

	// 4. Tally votes by stake until strictly more than two thirds accepts.
	counted := make(map[string]bool)
	var accepted uint64
	total := n.set.TotalStake()
	for 3*accepted <= 2*total {
		v := n.waitFor(func(m message) bool {
			vm, ok := m.(voteMsg)
			if !ok {
				return false
			}
			return vm.vote.height == h &&
				bytes.Equal(vm.vote.blockHash, blockHash) &&
				!counted[vm.vote.voter]
		}).(voteMsg).vote

		if !n.set.Contains(v.voter) || !verifyVote(v) {
			continue
		}
		counted[v.voter] = true
		accepted += n.set.StakeOf(v.voter)
	}

	// 5. Commit the block the set agreed on.
	if err := n.chain.CommitBlock(candidate); err != nil {
		panic(fmt.Sprintf("validator %s failed to commit block %d: %v", n.self.address, h, err))
	}
}

// propose drains the mempool, builds a block for height h off the chain's
// tip, signs its header, and returns it. Only the proposer calls this, and
// it broadcasts the whole block, so every node commits identical bytes.
func (n *node) propose(h int64) Block {
	txs := n.mempool.Drain(n.maxBlockTxs)
	tip := n.chain.Blocks[len(n.chain.Blocks)-1]
	b := Block{
		Transactions: txs,
		TxRoot:       merkleRoot(txs),
		PreviousHash: tip.Hash(),
		CreatedAt:    time.Now().Unix(),
		Height:       h,
		Proposer:     n.self.address,
	}
	b.Signature = b.sign(n.self.privKey)
	return b
}

// waitFor returns the first message, from the stash or the inbox, that
// satisfies match. Messages that do not match are stashed in n.pending for
// a later call, which is how a node tolerates a vote arriving before its
// proposal, or a message for the next height arriving early.
func (n *node) waitFor(match func(message) bool) message {
	// 1. already stashed something that fits?
	for i, m := range n.pending {
		if match(m) {
			n.pending = append(n.pending[:i], n.pending[i+1:]...)
			return m
		}
	}
	// 2. otherwise pull from the inbox until something fits,
	//    stashing everything that doesn't
	for {
		m := <-n.inbox
		n.observe(m)
		if match(m) {
			return m
		}
		n.pending = append(n.pending, m)
	}
}

// prunePending drops stashed messages for heights at or below h, which are
// decided and will never be waited on again.
func (n *node) prunePending(h int64) {
	kept := n.pending[:0]
	for _, m := range n.pending {
		if msgHeight(m) > h {
			kept = append(kept, m)
		}
	}
	n.pending = kept
}

// observe runs on every message a node pulls off the inbox. It keeps the
// first vote it saw from each (height, voter); a second, conflicting vote
// is equivocation, which it verifies, records as Evidence, and slashes the
// offender for.
func (n *node) observe(m message) {
	vm, ok := m.(voteMsg)
	if !ok {
		return
	}
	v := vm.vote
	byHeight := n.seenVotes[v.height]
	if byHeight == nil {
		byHeight = map[string]vote{}
		n.seenVotes[v.height] = byHeight
	}
	prev, seen := byHeight[v.voter]
	if !seen {
		byHeight[v.voter] = v
		return
	}
	if !bytes.Equal(prev.blockHash, v.blockHash) {
		e := Evidence{Offender: v.voter, Height: v.height, VoteA: prev, VoteB: v}
		if verifyEvidence(e) {
			n.evidence = append(n.evidence, e)
			n.set.Slash(e.Offender)
		}
	}

}

// msgHeight is the height a message concerns, for prunePending.
func msgHeight(m message) int64 {
	switch v := m.(type) {
	case proposalMsg:
		return v.block.Height
	case voteMsg:
		return v.vote.height
	default:
		return 0
	}
}
