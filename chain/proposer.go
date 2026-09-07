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
// advances it one step per height in the same order and applies the same
// slashes at the same effective heights, so they all elect the same
// proposer and weigh votes the same way without communicating.
//
// It is advanced from the node goroutine only; no locking.
type election struct {
	addresses  []string
	stake      []uint64 // effective stake, zeroed when a slash takes effect
	priorities []int64  // the accumulator, indexed like addresses
}

// newElection snapshots the set's members and their stakes.
func newElection(set *ValidatorSet) *election {
	set.mu.RLock()
	defer set.mu.RUnlock()
	e := &election{
		addresses:  make([]string, len(set.members)),
		stake:      make([]uint64, len(set.members)),
		priorities: make([]int64, len(set.members)),
	}
	for i, m := range set.members {
		e.addresses[i] = m.address
		e.stake[i] = m.stake
	}
	return e
}

// slash zeroes a validator's effective stake from the next next() on.
func (e *election) slash(addr string) {
	for i, a := range e.addresses {
		if a == addr {
			e.stake[i] = 0
			return
		}
	}
}

// next advances one height and returns that height's proposer address:
// every validator's priority rises by its stake, the highest with stake
// left proposes (ties by index), and the winner drops by the total stake.
func (e *election) next() string {
	var total int64
	for _, s := range e.stake {
		total += int64(s)
	}
	for i := range e.stake {
		e.priorities[i] += int64(e.stake[i])
	}
	winner := -1
	for i := range e.stake {
		if e.stake[i] == 0 {
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

// totalStake is the sum of effective stakes, for the commit threshold.
func (e *election) totalStake() uint64 {
	var t uint64
	for _, s := range e.stake {
		t += s
	}
	return t
}

// stakeOf is a voter's effective stake, or zero if it is not a member.
func (e *election) stakeOf(addr string) uint64 {
	for i, a := range e.addresses {
		if a == addr {
			return e.stake[i]
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
