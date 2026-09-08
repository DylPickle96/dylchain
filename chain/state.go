package chain

import (
	"crypto/ed25519"
	"fmt"
	"maps"
	"math/bits"
	"sort"
)

// State is the ledger derived from replaying blocks: a balance and a nonce
// per address, plus the stake each holder has bonded behind each validator.
type State struct {
	Balances map[string]uint64
	Nonces   map[string]int64

	// Delegations is delegator address -> validator address -> bonded
	// amount. Bonded tokens leave Balances but are still supply; undelegate
	// moves them back. A real chain would add an unbonding period here.
	Delegations map[string]map[string]uint64

	// validatorBase is validator address -> genesis self-stake. It seeds
	// consensus weight and the reward split and never changes after
	// genesis, so it is shared, not deep-copied, and not part of Supply.
	validatorBase map[string]uint64
}

// Supply is every token that exists: the sum of balances and of all bonded
// delegations. It only grows, by BlockReward per committed block; delegate
// and undelegate move tokens between the two buckets without changing it.
func (s State) Supply() uint64 {
	var supply uint64
	for _, balance := range s.Balances {
		supply += balance
	}
	for _, byValidator := range s.Delegations {
		for _, amount := range byValidator {
			supply += amount
		}
	}
	return supply
}

// NewState builds a starting ledger from a genesis allocation of address to
// initial balance. All nonces start at zero and nothing is delegated.
func NewState(alloc map[string]uint64) State {
	s := State{
		Balances:    make(map[string]uint64, len(alloc)),
		Nonces:      make(map[string]int64),
		Delegations: make(map[string]map[string]uint64),
	}
	maps.Copy(s.Balances, alloc)
	return s
}

// clone returns a deep copy of the mutable maps, so callers can mutate
// freely. validatorBase is immutable and shared.
func (s State) clone() State {
	next := State{
		Balances:      make(map[string]uint64, len(s.Balances)),
		Nonces:        make(map[string]int64, len(s.Nonces)),
		Delegations:   make(map[string]map[string]uint64, len(s.Delegations)),
		validatorBase: s.validatorBase,
	}
	maps.Copy(next.Balances, s.Balances)
	maps.Copy(next.Nonces, s.Nonces)
	for delegator, byValidator := range s.Delegations {
		next.Delegations[delegator] = maps.Clone(byValidator)
	}
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
		distributeReward(&next, block.Proposer)
	}
	return next, nil
}

// applyTx checks one transaction against s and, if it is valid, mutates s
// in place. On any error s is left untouched: every write happens after
// every check.
func applyTx(s *State, tx Transaction) error {
	// 1. Structural: is this even well formed?
	if tx.From == "" {
		return fmt.Errorf("empty sender")
	}
	if tx.Amount == 0 {
		return fmt.Errorf("zero amount")
	}
	if _, err := pubKeyFromAddress(tx.To); err != nil {
		return fmt.Errorf("bad recipient %s: %w", tx.To, err)
	}
	if tx.Kind == KindTransfer && tx.From == tx.To {
		return fmt.Errorf("self-send from %s", tx.From)
	}

	// 2. Authenticity: did the holder of From's key authorise these exact
	// fields? Check before touching account state, so an unauthenticated
	// transaction never influences which balances we read.
	pubKey, err := pubKeyFromAddress(tx.From)
	if err != nil {
		return fmt.Errorf("cannot derive public key for %s: %w", tx.From, err)
	}
	if !ed25519.Verify(pubKey, tx.signableBytes(), tx.Signature) {
		return fmt.Errorf("invalid signature for %s", tx.From)
	}

	// 3. Nonce: same for every kind.
	if tx.Nonce != s.Nonces[tx.From] {
		return fmt.Errorf("nonce is %d, expected %d for %s", tx.Nonce, s.Nonces[tx.From], tx.From)
	}

	// 4. Kind-specific rules and effect.
	switch tx.Kind {
	case KindTransfer:
		if s.Balances[tx.From] < tx.Amount {
			return fmt.Errorf("insufficient balance for %s: have %d, need %d", tx.From, s.Balances[tx.From], tx.Amount)
		}
		s.Balances[tx.From] -= tx.Amount
		s.Balances[tx.To] += tx.Amount

	case KindDelegate:
		if s.Balances[tx.From] < tx.Amount {
			return fmt.Errorf("insufficient balance to delegate for %s: have %d, need %d", tx.From, s.Balances[tx.From], tx.Amount)
		}
		s.Balances[tx.From] -= tx.Amount
		if s.Delegations[tx.From] == nil {
			s.Delegations[tx.From] = make(map[string]uint64)
		}
		s.Delegations[tx.From][tx.To] += tx.Amount

	case KindUndelegate:
		bonded := s.Delegations[tx.From][tx.To]
		if bonded < tx.Amount {
			return fmt.Errorf("not enough bonded to %s for %s: have %d, need %d", tx.To, tx.From, bonded, tx.Amount)
		}
		if bonded == tx.Amount {
			delete(s.Delegations[tx.From], tx.To)
			if len(s.Delegations[tx.From]) == 0 {
				delete(s.Delegations, tx.From)
			}
		} else {
			s.Delegations[tx.From][tx.To] -= tx.Amount
		}
		s.Balances[tx.From] += tx.Amount

	default:
		return fmt.Errorf("unknown transaction kind %q", tx.Kind)
	}

	s.Nonces[tx.From]++
	return nil
}

// distributeReward splits one BlockReward for a committed block between the
// proposer and everyone delegated to it, in proportion to stake: the
// proposer's genesis self-stake against each delegator's bonded amount. The
// rounding dust from integer division goes to the proposer. Delegators are
// paid in sorted order so every node produces byte-identical state.
func distributeReward(s *State, proposer string) {
	self := s.validatorBase[proposer]

	delegators := make([]string, 0)
	for delegator, byValidator := range s.Delegations {
		if byValidator[proposer] > 0 {
			delegators = append(delegators, delegator)
		}
	}
	sort.Strings(delegators)

	var delegated uint64
	for _, d := range delegators {
		delegated += s.Delegations[d][proposer]
	}

	total := self + delegated
	if total == 0 {
		s.Balances[proposer] += BlockReward
		return
	}

	var paid uint64
	for _, d := range delegators {
		share := mulDiv(BlockReward, s.Delegations[d][proposer], total)
		s.Balances[d] += share
		paid += share
	}
	s.Balances[proposer] += BlockReward - paid
}

// delegationsEqual reports whether two delegator -> validator -> amount
// maps hold the same entries.
func delegationsEqual(a, b map[string]map[string]uint64) bool {
	if len(a) != len(b) {
		return false
	}
	for delegator, byValidator := range a {
		if !maps.Equal(byValidator, b[delegator]) {
			return false
		}
	}
	return true
}

// mulDiv returns a*b/c computed at 128-bit width, so a*b does not overflow.
// c must be non-zero and, as callers guarantee, at least b, which keeps the
// quotient under 2^64 and bits.Div64 from panicking.
func mulDiv(a, b, c uint64) uint64 {
	hi, lo := bits.Mul64(a, b)
	q, _ := bits.Div64(hi, lo, c)
	return q
}

// ReplayBlocks rebuilds ledger state from a block list that starts with a
// genesis block: the genesis Alloc seeds the balances, its Validators seed
// the reward-split weights, and every later block is applied in order. It
// is how a node holding only the blocks recovers the state a live chain
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
	state.validatorBase = blocks[0].Validators
	for _, block := range blocks[1:] {
		next, err := Apply(state, block)
		if err != nil {
			return State{}, err
		}
		state = next
	}
	return state, nil
}
