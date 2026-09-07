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

// clone returns a deep copy of the maps, so callers can mutate freely.
func (s State) clone() State {
	next := State{
		Balances: make(map[string]uint64, len(s.Balances)),
		Nonces:   make(map[string]int64, len(s.Nonces)),
	}
	maps.Copy(next.Balances, s.Balances)
	maps.Copy(next.Nonces, s.Nonces)
	return next
}

// Apply returns the state that results from applying block to state. It
// works on a copy, so state is never mutated. It is all-or-nothing: the
// first invalid transaction rejects the whole block and no partial effect
// is applied.
func Apply(state State, block Block) (State, error) {
	next := state.clone()
	for i, tx := range block.Transactions {
		if err := applyTx(&next, tx); err != nil {
			return State{}, fmt.Errorf("tx %d: %w", i, err)
		}
	}
	if block.Proposer != "" {
		next.Balances[block.Proposer] += BlockReward
	}
	return next, nil
}

// applyTx checks one transaction against s and, if it is valid, mutates s
// in place. On any error s is left untouched: every write happens after
// every check.
func applyTx(s *State, tx Transaction) error {
	// 1. Structural: is this even a well-formed transfer?
	if tx.From == "" {
		return fmt.Errorf("empty sender")
	}
	if tx.From == tx.To {
		return fmt.Errorf("self-send from %s", tx.From)
	}
	if tx.Amount == 0 {
		return fmt.Errorf("zero amount")
	}
	if _, err := pubKeyFromAddress(tx.To); err != nil {
		return fmt.Errorf("bad recipient %s: %w", tx.To, err)
	}

	// 2. Authenticity: did the holder of From's private key authorise these
	// exact fields? Check this before touching account state, so an
	// unauthenticated transaction never influences which balances we read.
	pubKey, err := pubKeyFromAddress(tx.From)
	if err != nil {
		return fmt.Errorf("cannot derive public key for %s: %w", tx.From, err)
	}
	if !ed25519.Verify(pubKey, tx.signableBytes(), tx.Signature) {
		return fmt.Errorf("invalid signature for %s", tx.From)
	}

	// 3. Account rules: right nonce, enough balance?
	if tx.Nonce != s.Nonces[tx.From] {
		return fmt.Errorf("nonce is %d, expected %d for %s", tx.Nonce, s.Nonces[tx.From], tx.From)
	}
	if s.Balances[tx.From] < tx.Amount {
		return fmt.Errorf("insufficient balance for %s: have %d, need %d", tx.From, s.Balances[tx.From], tx.Amount)
	}

	// 4. Apply.
	s.Balances[tx.From] -= tx.Amount
	s.Balances[tx.To] += tx.Amount
	s.Nonces[tx.From]++
	return nil
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
