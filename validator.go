package dyl

import "crypto/ed25519"

type Validator struct {
	address string
	privKey ed25519.PrivateKey
	stake   uint64
}

func NewValidator(privKey ed25519.PrivateKey, stake uint64) Validator {
	pubKey := privKey.Public().(ed25519.PublicKey)
	return Validator{
		address: deriveAddress(pubKey),
		privKey: privKey,
		stake:   stake,
	}
}

func (v Validator) Address() string {
	return v.address
}

type ValidatorSet struct {
	members []Validator
}

func NewValidatorSet(members ...Validator) *ValidatorSet {
	return &ValidatorSet{members: members}
}

func (vs *ValidatorSet) TotalStake() uint64 {
	var staked uint64
	for _, v := range vs.members {
		staked += v.stake
	}
	return staked
}

func (vs *ValidatorSet) ProposerForHeight(h int64) Validator {
	if len(vs.members) == 0 {
		panic("no members in validator set to propose")
	}
	n := int64(len(vs.members))
	return vs.members[((h-1)%n+n)%n]
}

func (vs *ValidatorSet) StakeOf(addr string) uint64 {
	for _, v := range vs.members {
		if v.address == addr {
			return v.stake
		}
	}
	return 0
}

func (vs *ValidatorSet) Contains(addr string) bool {
	for _, v := range vs.members {
		if v.address == addr {
			return true
		}
	}
	return false
}
