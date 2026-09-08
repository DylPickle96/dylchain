package chain

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// With a heal delay set, a slashed validator gets its stake back, stops
// equivocating, and the snapshot stops marking it slashed. The cluster
// keeps producing valid blocks throughout.
func TestClusterHealsSlashedValidator(t *testing.T) {
	set := fourValidators(t)
	cl := NewCluster(nil, set, 0)
	cl.SetHealDelay(5)

	offender := set.members[2].address
	want := set.StakeOf(offender)
	if want == 0 {
		t.Fatal("offender starts with no stake")
	}

	events := cl.Subscribe()
	defer cl.Unsubscribe(events)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var sawSlash, sawHeal atomic.Bool
	go func() {
		defer cancel()
		height := func() int64 { return cl.Snapshot().Height }
		for height() < 2 {
			time.Sleep(time.Millisecond)
		}
		cl.MakeFaulty(2)
		deadline := time.After(10 * time.Second)
		for {
			select {
			case e := <-events:
				if e.Validator != offender {
					continue
				}
				switch e.Kind {
				case "slash":
					sawSlash.Store(true)
				case "heal":
					sawHeal.Store(true)
					// The event fires from the first node past the heal
					// height; wait for the offender's own node to clear
					// its flag too before stopping the run.
					for i := 0; i < 2000 && cl.Faulty(2); i++ {
						time.Sleep(time.Millisecond)
					}
					return
				}
			case <-deadline:
				return
			}
		}
	}()
	cl.RunContext(ctx)

	if !sawSlash.Load() {
		t.Error("no slash event for the offender")
	}
	if !sawHeal.Load() {
		t.Fatal("offender was never healed")
	}
	if got := set.StakeOf(offender); got != want {
		t.Errorf("stake after heal: got %d, want %d", got, want)
	}
	if got := set.TotalStake(); got != 4 {
		t.Errorf("total stake after heal: got %d, want 4", got)
	}
	if cl.Faulty(2) {
		t.Error("byzantine flag still set after heal")
	}
	if cl.Snapshot().Validators[2].Slashed {
		t.Error("snapshot still marks the healed validator slashed")
	}
	for i := 0; i < cl.Size(); i++ {
		if err := cl.Chain(i).Validate(); err != nil {
			t.Errorf("node %d chain does not validate: %v", i, err)
		}
	}
}

// A validator that has healed can be faulted and slashed a second time:
// the heal clears the per-node "already handled" guard.
func TestHealedValidatorCanBeCaughtAgain(t *testing.T) {
	set := fourValidators(t)
	cl := NewCluster(nil, set, 0)
	cl.SetHealDelay(5)

	offender := set.members[2].address

	events := cl.Subscribe()
	defer cl.Unsubscribe(events)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var slashes atomic.Int64
	go func() {
		defer cancel()
		height := func() int64 { return cl.Snapshot().Height }
		for height() < 2 {
			time.Sleep(time.Millisecond)
		}
		cl.MakeFaulty(2)
		refaulted := false
		deadline := time.After(20 * time.Second)
		for {
			select {
			case e := <-events:
				if e.Validator != offender {
					continue
				}
				switch e.Kind {
				case "slash":
					if slashes.Add(1) >= 2 {
						// Wait for the second slash to actually take effect.
						for i := 0; i < 5000 && set.StakeOf(offender) != 0; i++ {
							time.Sleep(time.Millisecond)
						}
						return
					}
				case "heal":
					if refaulted {
						continue
					}
					refaulted = true
					// Let every node cross the heal height and clear its
					// per-node guards, then fault the validator again.
					done := height() + 4
					for height() < done {
						time.Sleep(time.Millisecond)
					}
					cl.MakeFaulty(2)
				}
			case <-deadline:
				return
			}
		}
	}()
	cl.RunContext(ctx)

	if got := slashes.Load(); got < 2 {
		t.Fatalf("offender slashed %d times, want it caught again after healing", got)
	}
	if got := set.StakeOf(offender); got != 0 {
		t.Errorf("stake after the second slash: got %d, want 0", got)
	}
}

// Without a heal delay a slash is permanent: no heal event, stake stays
// zero for the rest of the run.
func TestHealDelayZeroKeepsSlashPermanent(t *testing.T) {
	set := fourValidators(t)
	cl := NewCluster(nil, set, 0)

	events := cl.Subscribe()
	defer cl.Unsubscribe(events)

	cl.MakeFaulty(2)
	cl.Run(20)

	if got := set.StakeOf(set.members[2].address); got != 0 {
		t.Errorf("stake without a heal delay: got %d, want 0 (permanent)", got)
	}
	for {
		select {
		case e := <-events:
			if e.Kind == "heal" {
				t.Fatalf("healed at height %d with no heal delay set", e.Height)
			}
		default:
			return
		}
	}
}

// election.restore puts a slashed validator's stake back and lets it be
// elected again, at the back of the rotation.
func TestElectionRestore(t *testing.T) {
	set := NewValidatorSet(
		NewValidator(newWallet(t).priv, 10),
		NewValidator(newWallet(t).priv, 10),
		NewValidator(newWallet(t).priv, 10),
	)
	e := newElection(set)
	target := set.members[1].address

	for i := 0; i < 30; i++ {
		e.next()
	}
	e.slash(target)
	if e.totalStake() != 20 {
		t.Fatalf("total stake after slash: got %d, want 20", e.totalStake())
	}
	for i := 0; i < 30; i++ {
		if got := e.next(); got == target {
			t.Fatal("slashed validator was still elected")
		}
	}

	e.restore(target)
	if e.totalStake() != 30 {
		t.Fatalf("total stake after restore: got %d, want 30", e.totalStake())
	}
	seen := false
	for i := 0; i < 30 && !seen; i++ {
		if e.next() == target {
			seen = true
		}
	}
	if !seen {
		t.Error("restored validator was never elected again")
	}
}
