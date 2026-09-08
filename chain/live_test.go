package chain

import (
	"context"
	"testing"
	"time"
)

// runUntilHeight runs cl until its snapshot reaches h, then cancels and
// waits for every node to unwind.
func runUntilHeight(t *testing.T, cl *Cluster, h int64) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		deadline := time.Now().Add(10 * time.Second)
		for cl.Snapshot().Height < h {
			if time.Now().After(deadline) {
				cancel()
				return
			}
			time.Sleep(time.Millisecond)
		}
		cancel()
	}()
	cl.RunContext(ctx)
}

// RunContext keeps producing blocks and stops cleanly when the context is
// cancelled.
func TestRunContextStopsCleanly(t *testing.T) {
	set := fourValidators(t)
	cl := NewCluster(nil, set, 0)

	done := make(chan struct{})
	go func() {
		runUntilHeight(t, cl, 5)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("RunContext did not return after cancel")
	}

	if got := cl.Snapshot().Height; got < 5 {
		t.Fatalf("stopped at height %d, expected at least 5", got)
	}
}

// Snapshot and Account reflect committed state: supply grows by a reward
// per block, the proposer counts add up, and a funded account reads back.
func TestSnapshotAndAccount(t *testing.T) {
	alice := newWallet(t)
	set := fourValidators(t)
	cl := NewCluster(map[string]uint64{alice.addr: 1_000_000}, set, 0)

	// Account works before the first block, from the genesis seed.
	if bal, nonce := cl.Account(alice.addr); bal != 1_000_000 || nonce != 0 {
		t.Fatalf("pre-run account: balance %d nonce %d, want 1000000 / 0", bal, nonce)
	}

	runUntilHeight(t, cl, 6)
	s := cl.Snapshot()

	if s.Height < 6 {
		t.Fatalf("snapshot height %d, want >= 6", s.Height)
	}
	if want := uint64(1_000_000) + uint64(s.Height)*BlockReward; s.Supply != want {
		t.Errorf("supply %d, want %d", s.Supply, want)
	}
	if len(s.Validators) != 4 {
		t.Errorf("got %d validators in snapshot, want 4", len(s.Validators))
	}
	total := 0
	for _, v := range s.Validators {
		total += v.Proposed
	}
	if int64(total) != s.Height {
		t.Errorf("proposer counts sum to %d, want %d", total, s.Height)
	}
	if len(s.Blocks) == 0 || s.Blocks[len(s.Blocks)-1].Height != s.Height {
		t.Errorf("recent blocks do not end at the current height")
	}
	if bal, _ := cl.Account(alice.addr); bal != 1_000_000 {
		t.Errorf("alice balance after empty blocks: %d, want unchanged", bal)
	}
}

// Submit accepts a well-signed transaction and rejects malformed or badly
// signed ones.
func TestSubmitValidatesSignature(t *testing.T) {
	alice := newWallet(t)
	bob := newWallet(t)
	cl := NewCluster(map[string]uint64{alice.addr: 1000}, fourValidators(t), 0)

	if err := cl.Submit(alice.send(t, bob.addr, 10, 0)); err != nil {
		t.Fatalf("valid transaction rejected: %v", err)
	}

	bad := alice.send(t, bob.addr, 10, 1)
	bad.Signature[0] ^= 0xff
	if err := cl.Submit(bad); err == nil {
		t.Error("transaction with a broken signature was accepted")
	}

	unsigned := tx(alice.addr, bob.addr, 10, 2)
	if err := cl.Submit(unsigned); err == nil {
		t.Error("unsigned transaction was accepted")
	}

	if err := cl.Submit(tx(alice.addr, alice.addr, 10, 0)); err == nil {
		t.Error("self-send was accepted")
	}

	toJunk := Transaction{From: alice.addr, To: "not-an-address", Amount: 10, Nonce: 3}
	toJunk.Signature = toJunk.Sign(alice.priv)
	if err := cl.Submit(toJunk); err == nil {
		t.Error("transaction to a malformed address was accepted")
	}
}

// A node keeps only a bounded window of past votes for equivocation
// detection, so memory does not grow with chain length.
func TestSeenVotesPruned(t *testing.T) {
	cl := NewCluster(nil, fourValidators(t), 0)
	cl.Run(30)

	for i := 0; i < cl.Size(); i++ {
		if got := len(cl.nodes[i].seenVotes); got > voteMemory+1 {
			t.Errorf("node %d retains %d heights of votes, want <= %d", i, got, voteMemory+1)
		}
	}
}

// A well-signed transaction that cannot apply (here, a nonce far in the
// future) is dropped by the proposer without stalling consensus, and a good
// transaction alongside it still lands.
func TestProposerDropsInapplicableTx(t *testing.T) {
	alice := newWallet(t)
	bob := newWallet(t)
	cl := NewCluster(map[string]uint64{alice.addr: 1000}, fourValidators(t), 0)

	if err := cl.Submit(alice.send(t, bob.addr, 100, 0)); err != nil {
		t.Fatal(err)
	}
	if err := cl.Submit(alice.send(t, bob.addr, 100, 99)); err != nil { // nonce 99: never applies
		t.Fatal(err)
	}

	runUntilHeight(t, cl, 4)

	if bal, _ := cl.Account(bob.addr); bal != 100 {
		t.Errorf("bob balance %d, want 100 (good tx landed, bad tx dropped)", bal)
	}
	for i := 0; i < cl.Size(); i++ {
		if err := cl.Chain(i).Validate(); err != nil {
			t.Errorf("node %d chain does not validate: %v", i, err)
		}
	}
}

// Subscribers get a block event per height and a slash event when a faulty
// validator is caught.
func TestEventStream(t *testing.T) {
	set := fourValidators(t)
	cl := NewCluster(nil, set, 0)
	cl.MakeFaulty(2)

	events := cl.Subscribe()
	defer cl.Unsubscribe(events)

	var blocks, slashes int
	offender := set.members[2].address
	done := make(chan struct{})
	go func() {
		for e := range events {
			switch e.Kind {
			case "block":
				blocks++
			case "slash":
				if e.Validator == offender {
					slashes++
				}
			}
			if blocks >= 5 && slashes >= 1 {
				close(done)
				return
			}
		}
	}()

	go runUntilHeight(t, cl, 12)

	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatalf("only saw %d block events and %d slash events", blocks, slashes)
	}
}

// A paced (server) node keeps only a bounded window of blocks in memory;
// an unpaced one (test mode) keeps every block so Validate can replay.
func TestBlockRetention(t *testing.T) {
	unpaced := NewCluster(nil, fourValidators(t), 0)
	unpaced.Run(600)
	if got := len(unpaced.Chain(0).Blocks); got != 601 {
		t.Errorf("unpaced node kept %d blocks, want 601 (genesis + 600)", got)
	}

	paced := NewCluster(nil, fourValidators(t), 0)
	paced.PaceBlocks(time.Millisecond)
	runUntilHeight(t, paced, 2*blockRetention+40)

	got := len(paced.Chain(0).Blocks)
	if got >= 2*blockRetention || got < blockRetention {
		t.Errorf("paced node kept %d blocks, want in [%d, %d)", got, blockRetention, 2*blockRetention)
	}
}

// recordHalt surfaces in Snapshot and on the event stream, so a node that
// stops on an impossible state is visible instead of crashing the process.
func TestRecordHalt(t *testing.T) {
	cl := NewCluster(nil, fourValidators(t), 0)
	events := cl.Subscribe()
	defer cl.Unsubscribe(events)

	cl.recordHalt("dylabc", "rejected the block at height 5: boom")

	if h := cl.Snapshot().Halts; len(h) != 1 || h[0] != "dylabc: rejected the block at height 5: boom" {
		t.Errorf("Snapshot halts = %v", h)
	}
	select {
	case e := <-events:
		if e.Kind != "halt" || e.Validator != "dylabc" {
			t.Errorf("halt event = %+v", e)
		}
	case <-time.After(time.Second):
		t.Error("no halt event on the stream")
	}
}

// Snapshot carries a bounded feed of applied transactions with their hash,
// height, and the block's time, plus each block's hash and the genesis
// time.
func TestSnapshotTxFeed(t *testing.T) {
	alice := newWallet(t)
	bob := newWallet(t)
	cl := NewCluster(map[string]uint64{alice.addr: 1_000_000}, fourValidators(t), 0)

	if g := cl.Snapshot().Genesis; g == 0 {
		t.Fatal("snapshot genesis time is zero")
	}
	for i := int64(0); i < 3; i++ {
		if err := cl.Submit(alice.send(t, bob.addr, 10, i)); err != nil {
			t.Fatal(err)
		}
	}

	runUntilHeight(t, cl, 4)
	s := cl.Snapshot()

	if len(s.Txs) != 3 {
		t.Fatalf("got %d txs in the feed, want 3", len(s.Txs))
	}
	seen := map[string]bool{}
	for i, tx := range s.Txs {
		if tx.From != alice.addr || tx.To != bob.addr || tx.Amount != 10 {
			t.Errorf("tx %d: %+v", i, tx)
		}
		if len(tx.Hash) != 64 || seen[tx.Hash] {
			t.Errorf("tx %d: bad or duplicate hash %q", i, tx.Hash)
		}
		seen[tx.Hash] = true
		var blk *BlockInfo
		for j := range s.Blocks {
			if s.Blocks[j].Height == tx.Height {
				blk = &s.Blocks[j]
			}
		}
		if blk == nil {
			t.Errorf("tx %d: height %d not in the recent blocks", i, tx.Height)
		} else if blk.Time != tx.Time {
			t.Errorf("tx %d: time %d, block time %d", i, tx.Time, blk.Time)
		}
	}
	for _, b := range s.Blocks {
		if len(b.Hash) != 64 {
			t.Errorf("block %d: hash %q", b.Height, b.Hash)
		}
	}
}

// The transaction feed is bounded at recentTxs, keeping the newest.
func TestSnapshotTxFeedBounded(t *testing.T) {
	alice := newWallet(t)
	bob := newWallet(t)
	cl := NewCluster(map[string]uint64{alice.addr: 1_000_000}, fourValidators(t), 0)

	n := int64(recentTxs + 5)
	for i := int64(0); i < n; i++ {
		if err := cl.Submit(alice.send(t, bob.addr, 1, i)); err != nil {
			t.Fatal(err)
		}
	}
	runUntilHeight(t, cl, 3)
	s := cl.Snapshot()

	if len(s.Txs) != recentTxs {
		t.Fatalf("feed holds %d, want %d", len(s.Txs), recentTxs)
	}
	if bal, _ := cl.Account(bob.addr); bal != uint64(n) {
		t.Fatalf("bob has %d, want %d: not every tx applied", bal, n)
	}
	for i := 1; i < len(s.Txs); i++ {
		if s.Txs[i].Height < s.Txs[i-1].Height {
			t.Fatal("feed is not in height order")
		}
	}
}
