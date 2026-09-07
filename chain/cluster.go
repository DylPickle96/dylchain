package chain

import (
	"fmt"
	"sync"
)

// Cluster runs a set of validators through one-step-vote BFT consensus in a
// single process, connected by an in-memory bus. Validators are honest,
// online, and fast unless MakeFaulty marks one. No proposer timeouts, no
// round changes. Equivocation is detected (see Evidence) but not yet acted
// on.
type Cluster struct {
	set     *ValidatorSet
	mempool *Mempool
	nodes   []*node
}

// NewCluster wires up a cluster: one genesis block built from alloc and
// shared by every validator's chain, one shared mempool, and a bus joining
// every node. maxBlockTxs caps how many transactions a proposer puts in a
// block; zero means no cap.
//
// The genesis block must be built once and handed to every
// newChainFromGenesis call. Building it per node would give each a
// different genesis hash (genesisBlock stamps time.Now), and block 1 would
// then fail to link on every node but the proposer.
func NewCluster(alloc map[string]uint64, set *ValidatorSet, maxBlockTxs int) *Cluster {
	genesis := genesisBlock(alloc)
	mempool := NewMempool()
	b := newBus(len(set.members))

	c := &Cluster{set: set, mempool: mempool}
	for i, v := range set.members {
		chain, err := newChainFromGenesis(genesis)
		if err != nil {
			panic(fmt.Sprintf("cluster genesis: %v", err))
		}
		c.nodes = append(c.nodes, &node{
			self:        v,
			set:         set,
			chain:       &chain,
			mempool:     mempool,
			inbox:       b.inboxes[i],
			bus:         b,
			maxBlockTxs: maxBlockTxs,
			seenVotes:   make(map[int64]map[string]vote),
		})
	}
	return c
}

// Submit adds a transaction to the shared mempool for a future proposer to
// include.
func (c *Cluster) Submit(tx Transaction) {
	c.mempool.Add(tx)
}

// Run drives every validator through the given number of heights, one
// goroutine per node, and returns once all of them have committed that
// many blocks.
func (c *Cluster) Run(heights int64) {
	var wg sync.WaitGroup
	for _, n := range c.nodes {
		wg.Add(1)
		go func(n *node) {
			defer wg.Done()
			n.run(heights)
		}(n)
	}
	wg.Wait()
}

// Size is the number of validators in the cluster.
func (c *Cluster) Size() int {
	return len(c.nodes)
}

// Chain returns the chain held by validator i, for inspection after a run.
func (c *Cluster) Chain(i int) *Chain {
	return c.nodes[i].chain
}

// MakeFaulty makes validator i double-vote every height. Stage 8 wires the
// UI fault button to this.
func (c *Cluster) MakeFaulty(i int) {
	c.nodes[i].byzantine = true
}

// Evidence returns the equivocation evidence the cluster has gathered, one
// entry per (offender, height), each re-verified. Call it after Run: it
// reads node state without locking.
func (c *Cluster) Evidence() []Evidence {
	seen := make(map[string]bool)
	var out []Evidence
	for _, n := range c.nodes {
		for _, e := range n.evidence {
			key := fmt.Sprintf("%s@%d", e.Offender, e.Height)
			if seen[key] || !verifyEvidence(e) {
				continue
			}
			seen[key] = true
			out = append(out, e)
		}
	}
	return out
}

// ValidatorSet returns the cluster's set, for reading stakes after a run.
func (c *Cluster) ValidatorSet() *ValidatorSet {
	return c.set
}
