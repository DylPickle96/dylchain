package chain

import (
	"crypto/ed25519"
	"fmt"
	"maps"
)

// State is the ledger derived from replaying blocks: a balance and a
// nonce per address.
type State struct {
	Balances map[string]uint64
	Nonces   map[string]int64
}

// Supply is the total amount in circulation: the sum of every balance.
// Nothing is ever burned, so it only grows, by BlockReward per committed
// block.
func (s State) Supply() uint64 {
	var supply uint64
	for _, balance := range s.Balances {
		supply += balance
	}
	return supply
}

// NewState builds a starting ledger from a genesis allocation of address
// to initial balance. All nonces start at zero.
func NewState(alloc map[string]uint64) State {
	s := State{
		Balances: make(map[string]uint64, len(alloc)),
		Nonces:   make(map[string]int64),
	}
	maps.Copy(s.Balances, alloc)
	return s
}

// Apply returns the state that results from applying block to state. It
// works on a copy, so state is never mutated. It is all-or-nothing: the
// first invalid transaction rejects the whole block and no partial effect
// is applied.
func Apply(state State, block Block) (State, error) {
	next := State{
		Balances: make(map[string]uint64, len(state.Balances)),
		Nonces:   make(map[string]int64, len(state.Nonces)),
	}
	maps.Copy(next.Balances, state.Balances)
	maps.Copy(next.Nonces, state.Nonces)
	for i, tx := range block.Transactions {
		// 1. Structural checks: is this even a well-formed transfer?
		if tx.From == "" {
			return State{}, fmt.Errorf("tx %d: empty sender", i)
		}
		if tx.From == tx.To {
			return State{}, fmt.Errorf("tx %d: self-send from %s", i, tx.From)
		}
		if tx.Amount == 0 {
			return State{}, fmt.Errorf("tx %d: zero amount", i)
		}

		// 2. Authenticity: did the holder of From's private key actually
		// authorise these exact fields? Verify this before consulting any
		// account state, so an unauthenticated transaction never influences
		// which error we report or which balances we read.
		pubKey, err := pubKeyFromAddress(tx.From)
		if err != nil {
			return State{}, fmt.Errorf("tx %d: cannot derive public key for %s: %w", i, tx.From, err)
		}
		if !ed25519.Verify(pubKey, tx.signableBytes(), tx.Signature) {
			return State{}, fmt.Errorf("tx %d: invalid signature for %s", i, tx.From)
		}

		// 3. Account rules: does the authenticated sender have the right
		// nonce and enough balance?
		if tx.Nonce != next.Nonces[tx.From] {
			return State{}, fmt.Errorf("tx %d: nonce is %d, expected %d for %s", i, tx.Nonce, next.Nonces[tx.From], tx.From)
		}
		if next.Balances[tx.From] < tx.Amount {
			return State{}, fmt.Errorf("tx %d: insufficient balance for %s: have %d, need %d", i, tx.From, next.Balances[tx.From], tx.Amount)
		}

		// 4. Apply.
		next.Balances[tx.From] -= tx.Amount
		next.Balances[tx.To] += tx.Amount
		next.Nonces[tx.From]++
	}
	if block.Proposer != "" {
		next.Balances[block.Proposer] += BlockReward
	}
	return next, nil
}

// ReplayBlocks rebuilds ledger state from a block list that starts with a
// genesis block: the genesis Alloc seeds the balances and every later
// block is applied in order. It is how a node holding only the blocks (from
// sync, or a fresh chain in NewChain) recovers the state a live chain
// carries. It assumes the list is already structurally valid; Validate is
// what checks links and signatures.
func ReplayBlocks(blocks []Block) (State, error) {
	if len(blocks) == 0 {
		return State{}, fmt.Errorf("block length cannot be zero")
	}
	if blocks[0].Height != 0 {
		return State{}, fmt.Errorf("genesis block height must be zero")
	}
	if len(blocks[0].Transactions) != 0 {
		return State{}, fmt.Errorf("genesis block carries transactions")
	}
	state := NewState(blocks[0].Alloc)
	for _, block := range blocks[1:] {
		next, err := Apply(state, block)
		if err != nil {
			return State{}, err
		}
		state = next
	}
	return state, nil
}
