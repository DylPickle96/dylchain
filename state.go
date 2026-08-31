package blockchain

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
	return next, nil
}
