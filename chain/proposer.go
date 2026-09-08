package chain

// slashDelay is how many heights after the equivocation height a slash
// takes effect. Every honest node has recorded the evidence by then (a
// double-vote's two votes are in flight within a height of each other), so
// they all apply the stake change at the same height and compute the same
// proposer and the same commit threshold for every height. Applying it
// sooner risks two nodes disagreeing on a height's proposer and stalling.
const slashDelay = 2

// election is a node's private, incremental copy of the stake-weighted
// priority accumulator, plus the stake view the tally uses. Every node
// advances it one step per height in the same order, applies the same
// slashes at the same effective heights, and re-reads delegated stake from
// its own committed state each height, so they all elect the same proposer
// and weigh votes the same way without communicating.
//
// It is advanced from the node goroutine only; no locking.
type election struct {
	addresses  []string
	origStake  []uint64 // genesis self-stake, never changes
	delegated  []uint64 // total delegated, refreshed from committed state each height
	slashed    []bool   // true from a slash's effective height until its heal
	priorities []int64  // the accumulator, indexed like addresses
}

// newElection snapshots the set's members and their self-stakes. Delegated
// stake starts at zero and is filled in by syncDelegations before the
// first height.
func newElection(set *ValidatorSet) *election {
	set.mu.RLock()
	defer set.mu.RUnlock()
	e := &election{
		addresses:  make([]string, len(set.members)),
		origStake:  make([]uint64, len(set.members)),
		delegated:  make([]uint64, len(set.members)),
		slashed:    make([]bool, len(set.members)),
		priorities: make([]int64, len(set.members)),
	}
	for i, m := range set.members {
		e.addresses[i] = m.address
		e.origStake[i] = m.stake
	}
	return e
}

// effective is a validator's voting weight this height: self-stake plus
// delegations, or zero while slashed.
func (e *election) effective(i int) uint64 {
	if e.slashed[i] {
		return 0
	}
	return e.origStake[i] + e.delegated[i]
}

// syncDelegations re-reads how much is bonded behind each validator from a
// committed state. Every node runs this over the same state each height, so
// a delegate or undelegate in block h changes the weights all nodes use
// from height h+1.
func (e *election) syncDelegations(s State) {
	tally := make(map[string]uint64, len(s.Delegations))
	for _, byValidator := range s.Delegations {
		for validator, amount := range byValidator {
			tally[validator] += amount
		}
	}
	for i, addr := range e.addresses {
		e.delegated[i] = tally[addr]
	}
}

// slash drops a validator's weight to zero from the next next() on.
func (e *election) slash(addr string) {
	for i, a := range e.addresses {
		if a == addr {
			e.slashed[i] = true
			return
		}
	}
}

// restore undoes a slash: the validator counts again, and its accumulator
// priority is set to the lowest among the validators that still have
// weight, so it rejoins the rotation at the back rather than proposing
// several blocks in a row to work off a stale priority. Every node calls
// this at the same height, so they stay in step.
func (e *election) restore(addr string) {
	idx := -1
	for i, a := range e.addresses {
		if a == addr {
			idx = i
			break
		}
	}
	if idx < 0 {
		return
	}
	e.slashed[idx] = false

	low := int64(0)
	first := true
	for i := range e.addresses {
		if i == idx || e.effective(i) == 0 {
			continue
		}
		if first || e.priorities[i] < low {
			low = e.priorities[i]
			first = false
		}
	}
	e.priorities[idx] = low
}

// next advances one height and returns that height's proposer address:
// every validator's priority rises by its weight, the highest with weight
// left proposes (ties by index), and the winner drops by the total weight.
func (e *election) next() string {
	var total int64
	for i := range e.addresses {
		total += int64(e.effective(i))
	}
	for i := range e.addresses {
		e.priorities[i] += int64(e.effective(i))
	}
	winner := -1
	for i := range e.addresses {
		if e.effective(i) == 0 {
			continue
		}
		if winner < 0 || e.priorities[i] > e.priorities[winner] {
			winner = i
		}
	}
	if winner < 0 {
		panic("no validator with stake to propose")
	}
	e.priorities[winner] -= total
	return e.addresses[winner]
}

// totalStake is the sum of effective weights, for the commit threshold.
func (e *election) totalStake() uint64 {
	var t uint64
	for i := range e.addresses {
		t += e.effective(i)
	}
	return t
}

// stakeOf is a voter's effective weight, or zero if it is not a member.
func (e *election) stakeOf(addr string) uint64 {
	for i, a := range e.addresses {
		if a == addr {
			return e.effective(i)
		}
	}
	return 0
}

// contains reports whether addr is a member.
func (e *election) contains(addr string) bool {
	for _, a := range e.addresses {
		if a == addr {
			return true
		}
	}
	return false
}
