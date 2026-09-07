package dyl

import "crypto/ed25519"

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
	members []Validator
}

// NewValidatorSet groups validators in the given order.
func NewValidatorSet(members ...Validator) *ValidatorSet {
	return &ValidatorSet{members: members}
}

// TotalStake is the sum of every member's stake.
func (vs *ValidatorSet) TotalStake() uint64 {
	var staked uint64
	for _, v := range vs.members {
		staked += v.stake
	}
	return staked
}

// ProposerForHeight returns the proposer for a height using CometBFT-style
// priority accumulation: each height every validator's priority rises by
// its stake, the highest goes, then drops by the total stake. It is
// recomputed from height 1 on every call, so it stays a pure function of
// the height and the set. Equal stakes reduce to plain round-robin; ties
// break by member index. O(height * len(members)) per call.
func (vs *ValidatorSet) ProposerForHeight(height int64) Validator {
	if len(vs.members) == 0 {
		panic("no members in validator set to propose")
	}
	total := int64(vs.TotalStake())
	priorities := make([]int64, len(vs.members))
	winner := 0
	for h := int64(1); h <= height; h++ {
		for i := range vs.members {
			priorities[i] += int64(vs.members[i].stake)
		}
		winner = 0
		for i := range priorities {
			if priorities[i] > priorities[winner] { // ties: lowest index
				winner = i
			}
		}
		priorities[winner] -= total
	}
	return vs.members[winner]
}

// StakeOf returns the stake of the member with this address, or zero if it
// is not in the set.
func (vs *ValidatorSet) StakeOf(addr string) uint64 {
	for _, v := range vs.members {
		if v.address == addr {
			return v.stake
		}
	}
	return 0
}

// Contains reports whether an address belongs to the set.
func (vs *ValidatorSet) Contains(addr string) bool {
	for _, v := range vs.members {
		if v.address == addr {
			return true
		}
	}
	return false
}
