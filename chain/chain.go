package chain

import (
	"bytes"
	"fmt"
	"maps"
	"time"
)

// Chain is an ordered list of blocks starting with a genesis block, plus
// the ledger state that results from applying them. state is unexported so
// it can only move forward through AddBlock or CommitBlock.
type Chain struct {
	Blocks []Block
	state  State
}

// NewChain returns a chain containing only its genesis block, so a Chain
// never exists without one.
func NewChain(alloc map[string]uint64) Chain {
	chain, err := newChainFromGenesis(genesisBlock(alloc, nil))
	if err != nil {
		panic(fmt.Sprintf("invalid genesis: %v", err))
	}
	return chain
}

// newChainFromGenesis wraps an already-built genesis block in a Chain,
// deriving the starting state by replaying it. Consensus validators pass
// the same genesis block here so their chains link identically.
func newChainFromGenesis(genesis Block) (Chain, error) {
	state, err := ReplayBlocks([]Block{genesis})
	if err != nil {
		return Chain{}, err
	}
	return Chain{Blocks: []Block{genesis}, state: state}, nil
}

// AddBlock appends a block carrying tx, linking it to the current tip and
// incrementing the height. The caller supplies only the payload; the
// fields that must stay consistent with the rest of the chain are derived
// here.
func (c *Chain) AddBlock(tx []Transaction) error {
	if len(c.Blocks) == 0 {
		panic("no genesis block, use NewChain()")
	}
	previousBlock := c.Blocks[len(c.Blocks)-1]
	newBlock := Block{
		Transactions: tx,
		TxRoot:       merkleRoot(tx),
		PreviousHash: previousBlock.Hash(),
		CreatedAt:    time.Now().Unix(),
		Height:       previousBlock.Height + 1,
	}
	next, err := Apply(c.state, newBlock)
	if err != nil {
		return fmt.Errorf("apply block %d: %w", newBlock.Height, err)
	}
	c.state = next
	c.Blocks = append(c.Blocks, newBlock)
	return nil
}

// Validate checks the chain's internal consistency: per block that the
// Merkle root matches its transactions and any proposer signature verifies,
// per adjacent pair that the previous-hash links and the height increments
// by one, and finally that replaying every block from genesis reproduces
// the chain's own state.
func (c *Chain) Validate() error {
	// Per block: the transaction body matches the root in the header.
	for i, b := range c.Blocks {
		if b.Proposer != "" {
			if err := verifyBlockSignature(b); err != nil {
				return fmt.Errorf("block %d: %w", i, err)
			}
		}
		if !bytes.Equal(b.TxRoot, merkleRoot(b.Transactions)) {
			return fmt.Errorf("block %d: TxRoot does not match the Merkle root of its transactions", i)
		}
	}
	// Per adjacent pair: the hash links and the height increments by one.
	for i := 0; i < len(c.Blocks)-1; i++ {
		if !bytes.Equal(c.Blocks[i].Hash(), c.Blocks[i+1].PreviousHash) {
			return fmt.Errorf("block %d: PreviousHash does not match hash of block %d", i+1, i)
		}
		if c.Blocks[i].Height+1 != c.Blocks[i+1].Height {
			return fmt.Errorf("block %d: height is %d, expected %d", i+1, c.Blocks[i+1].Height, c.Blocks[i].Height+1)
		}
	}
	state, err := ReplayBlocks(c.Blocks)
	if err != nil {
		return fmt.Errorf("chain state does not replay from genesis: %w", err)
	}
	if !maps.Equal(state.Balances, c.state.Balances) {
		return fmt.Errorf("replayed balances do not match chain state")
	}
	if !maps.Equal(state.Nonces, c.state.Nonces) {
		return fmt.Errorf("replayed nonces do not match chain state")
	}
	if !delegationsEqual(state.Delegations, c.state.Delegations) {
		return fmt.Errorf("replayed delegations do not match chain state")
	}
	return nil

}

// CommitBlock appends a block that consensus has already produced and voted
// on. Unlike AddBlock it does not build the block: it re-runs every check
// through checkCandidate and advances state, so a node that only receives
// committed blocks stays correct.
func (c *Chain) CommitBlock(b Block) error {
	next, err := c.checkCandidate(b)
	if err != nil {
		return fmt.Errorf("commit block %d: %w", b.Height, err)
	}
	c.state = next
	c.Blocks = append(c.Blocks, b)
	return nil
}

// checkCandidate runs every validity check for a proposed block without
// mutating the chain, returning the state that committing it would produce.
// A validator calls this before voting; CommitBlock calls it again before
// appending.
func (c *Chain) checkCandidate(b Block) (State, error) {
	tip := c.Blocks[len(c.Blocks)-1]
	if b.Height != tip.Height+1 {
		return State{}, fmt.Errorf("candidate block height does not follow previous tip")
	}
	if !bytes.Equal(b.PreviousHash, tip.Hash()) {
		return State{}, fmt.Errorf("candidate block previous hash does not equal tip's hash")
	}
	if !bytes.Equal(b.TxRoot, merkleRoot(b.Transactions)) {
		return State{}, fmt.Errorf("transaction root does not equal computed root")
	}
	if b.Proposer == "" {
		return State{}, fmt.Errorf("block must have proposer")
	}
	if err := verifyBlockSignature(b); err != nil {
		return State{}, fmt.Errorf("issue verifying block signature: %w", err)
	}
	next, err := Apply(c.state, b)
	if err != nil {
		return State{}, fmt.Errorf("cannot apply block: %w", err)
	}
	return next, nil
}
