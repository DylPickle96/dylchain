package chain

import (
	"crypto/ed25519"
	"sync"
)

// Validator is one consensus participant: an address, the signing key
// behind it, and a stake weight. In a real network a node would hold only
// its own key; here one process simulates every validator, so the key
// travels with the rest.
type Validator struct {
	address string
	privKey ed25519.PrivateKey
	stake   uint64
}

// NewValidator derives a validator from a private key and a stake weight.
func NewValidator(privKey ed25519.PrivateKey, stake uint64) Validator {
	pubKey := privKey.Public().(ed25519.PublicKey)
	return Validator{
		address: deriveAddress(pubKey),
		privKey: privKey,
		stake:   stake,
	}
}

// Address is the validator's chain address.
func (v Validator) Address() string {
	return v.address
}

// ValidatorSet is an ordered group of validators. The order fixes the
// proposer rotation; the stakes fix the commit threshold.
type ValidatorSet struct {
	mu      sync.RWMutex
	members []Validator
}

// NewValidatorSet groups validators in the given order.
func NewValidatorSet(members ...Validator) *ValidatorSet {
	return &ValidatorSet{members: members}
}

// totalStake sums the members' stakes. The caller must already hold the
// lock; TotalStake is the exported, locked wrapper.
func (vs *ValidatorSet) totalStake() uint64 {
	var t uint64
	for _, v := range vs.members {
		t += v.stake
	}
	return t
}

// TotalStake is the sum of every member's stake.
func (vs *ValidatorSet) TotalStake() uint64 {
	vs.mu.RLock()
	defer vs.mu.RUnlock()
	return vs.totalStake()
}

// ProposerForHeight returns the proposer for a height using CometBFT-style
// priority accumulation: each height every validator's priority rises by
// its stake, the highest goes, then drops by the total stake. It is
// recomputed from height 1 on every call, so it stays a pure function of
// the height and the set. Equal stakes reduce to plain round-robin; ties
// break by member index. O(height * len(members)) per call.
func (vs *ValidatorSet) ProposerForHeight(height int64) Validator {
	vs.mu.RLock()
	defer vs.mu.RUnlock()

	total := int64(vs.totalStake())
	priorities := make([]int64, len(vs.members)) // indexed by member index
	winner := -1
	for h := int64(1); h <= height; h++ {
		for i := range vs.members {
			priorities[i] += int64(vs.members[i].stake) // slashed add 0
		}
		winner = -1
		for i := range vs.members {
			if vs.members[i].stake == 0 {
				continue
			}
			if winner < 0 || priorities[i] > priorities[winner] {
				winner = i
			}
		}
		if winner < 0 {
			panic("no validator with stake to propose")
		}
		priorities[winner] -= total
	}
	return vs.members[winner]
}

// StakeOf returns the stake of the member with this address, or zero if it
// is not in the set.
func (vs *ValidatorSet) StakeOf(addr string) uint64 {
	vs.mu.RLock()
	defer vs.mu.RUnlock()
	for _, v := range vs.members {
		if v.address == addr {
			return v.stake
		}
	}
	return 0
}

// Contains reports whether an address belongs to the set.
func (vs *ValidatorSet) Contains(addr string) bool {
	vs.mu.RLock()
	defer vs.mu.RUnlock()
	for _, v := range vs.members {
		if v.address == addr {
			return true
		}
	}
	return false
}

// Slash cuts a validator's stake to zero, permanently. A second call for
// the same address is a no-op. Safe for concurrent use.
func (vs *ValidatorSet) Slash(addr string) {
	vs.mu.Lock()
	defer vs.mu.Unlock()

	for i := range vs.members {
		if vs.members[i].address == addr {
			vs.members[i].stake = 0
			return
		}
	}
}
