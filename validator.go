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
