package dyl

import (
	"fmt"
	"sync"
)

// Cluster runs a set of validators through one-step-vote BFT consensus in a
// single process, connected by an in-memory bus. It is the happy path only:
// every validator is honest, online, and fast enough. No proposer timeouts,
// no round changes, no equivocation handling.

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
