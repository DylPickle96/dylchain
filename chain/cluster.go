package chain

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// recentBlocks and recentTxs are how many committed blocks and applied
// transactions a Cluster keeps for its Snapshot feeds.
const (
	recentBlocks = 30
	recentTxs    = 40
)

// Cluster runs a set of validators through one-step-vote BFT consensus in a
// single process, connected by an in-memory bus. Validators are honest,
// online, and fast unless MakeFaulty marks one. No proposer timeouts, no
// round changes. Equivocation is detected (see Evidence), slashed, and
// surfaced through Snapshot and the event stream.
type Cluster struct {
	set     *ValidatorSet
	mempool *Mempool
	nodes   []*node

	// live state, updated as blocks commit, read by Snapshot and Account
	mu       sync.Mutex
	genesis  int64 // genesis block time, unix seconds
	height   int64
	supply   uint64
	recent   []BlockInfo
	txs      []TxInfo
	proposed map[string]int
	slashed  map[string]bool
	halts    []string // "<address>: <reason>" for nodes that stopped on an impossible state
	balances map[string]uint64
	nonces   map[string]int64
	subs     []chan Event
}

// BlockInfo is the summary of one committed block, for the Snapshot feed.
type BlockInfo struct {
	Height   int64  `json:"height"`
	Hash     string `json:"hash"` // hex
	Proposer string `json:"proposer"`
	Txs      int    `json:"txs"`
	Time     int64  `json:"time"`
}

// TxInfo is one applied transaction, for the Snapshot feed.
type TxInfo struct {
	Hash   string `json:"hash"` // hex
	Height int64  `json:"height"`
	From   string `json:"from"`
	To     string `json:"to"`
	Amount uint64 `json:"amount"`
	Time   int64  `json:"time"` // the block's time
}

// Event is a thing worth telling a watcher about: a committed block, a
// slashed validator, a validator whose stake was restored, or a node that
// halted.
type Event struct {
	Kind      string `json:"kind"` // "block", "slash", "heal", or "halt"
	Height    int64  `json:"height"`
	Validator string `json:"validator"` // proposer for "block", offender/halted/healed node otherwise
}

// ValidatorInfo is one validator's state in a Snapshot.
type ValidatorInfo struct {
	Moniker     string  `json:"moniker"`
	Address     string  `json:"address"`
	Stake       uint64  `json:"stake"`
	VotingPower float64 `json:"votingPower"` // fraction of current total stake
	Slashed     bool    `json:"slashed"`
	Proposed    int     `json:"proposed"`
}

// Snapshot is a consistent, lock-guarded view of the cluster for an API
// response.
type Snapshot struct {
	Genesis    int64           `json:"genesis"` // genesis block time, unix seconds
	Height     int64           `json:"height"`
	Supply     uint64          `json:"supply"`
	Blocks     []BlockInfo     `json:"blocks"` // recent, oldest first
	Txs        []TxInfo        `json:"txs"`    // recent, oldest first
	Validators []ValidatorInfo `json:"validators"`
	Halts      []string        `json:"halts"` // nodes that stopped on an impossible state
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

	c := &Cluster{
		set:      set,
		mempool:  mempool,
		genesis:  genesis.CreatedAt,
		proposed: make(map[string]int),
		slashed:  make(map[string]bool),
	}
	for i, v := range set.members {
		chain, err := newChainFromGenesis(genesis)
		if err != nil {
			panic(fmt.Sprintf("cluster genesis: %v", err))
		}
		c.nodes = append(c.nodes, &node{
			self:         v,
			set:          set,
			chain:        &chain,
			mempool:      mempool,
			inbox:        b.inboxes[i],
			bus:          b,
			cluster:      c,
			ctx:          context.Background(),
			maxBlockTxs:  maxBlockTxs,
			seenVotes:    make(map[int64]map[string]vote),
			slashed:      make(map[string]bool),
			pendingSlash: make(map[string]int64),
			pendingHeal:  make(map[string]int64),
			election:     newElection(set),
		})
	}

	// Seed the live view from genesis so Account works before the first block.
	gs := c.nodes[0].chain.state
	c.balances = gs.Balances
	c.nonces = gs.Nonces
	c.supply = gs.Supply()

	return c
}

// Submit validates a transaction and adds it to the shared mempool. It
// rejects anything malformed, badly addressed, or badly signed, so junk
// never reaches a proposer or the seen-address list. Nonce and balance are
// checked later, when a proposer tries to apply it.
func (c *Cluster) Submit(tx Transaction) error {
	if tx.From == tx.To || tx.Amount == 0 {
		return fmt.Errorf("malformed transaction")
	}
	pub, err := pubKeyFromAddress(tx.From)
	if err != nil {
		return fmt.Errorf("bad sender address: %w", err)
	}
	if _, err := pubKeyFromAddress(tx.To); err != nil {
		return fmt.Errorf("bad recipient address: %w", err)
	}
	if !ed25519.Verify(pub, tx.signableBytes(), tx.Signature) {
		return fmt.Errorf("invalid signature")
	}
	return c.mempool.Add(tx)
}

// Run drives every validator through the given number of heights, one
// goroutine per node, and returns once all of them have committed that
// many blocks.
func (c *Cluster) Run(heights int64) {
	c.drive(heights)
}

// RunContext drives every validator until ctx is cancelled, then returns
// once every node goroutine has unwound.
func (c *Cluster) RunContext(ctx context.Context) {
	for _, n := range c.nodes {
		n.ctx = ctx
	}
	c.drive(0)
}

// PaceBlocks makes every node pause d between committing one block and
// starting the next, so a demo runs at a watchable rate. Zero (the
// default) runs as fast as consensus allows. Call it before RunContext.
func (c *Cluster) PaceBlocks(d time.Duration) {
	for _, n := range c.nodes {
		n.minInterval = d
	}
}

func (c *Cluster) drive(maxHeight int64) {
	var wg sync.WaitGroup
	for _, n := range c.nodes {
		wg.Add(1)
		go func(n *node) {
			defer wg.Done()
			n.run(maxHeight)
		}(n)
	}
	wg.Wait()
}

// recordBlock is called by the first node to commit each height. It
// advances the live view and emits a block event.
func (c *Cluster) recordBlock(b Block, st State) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if b.Height <= c.height {
		return // another node already recorded this height
	}
	c.height = b.Height
	c.supply = st.Supply()
	c.balances = st.Balances
	c.nonces = st.Nonces
	c.proposed[b.Proposer]++
	c.recent = append(c.recent, BlockInfo{
		Height:   b.Height,
		Hash:     hex.EncodeToString(b.Hash()),
		Proposer: b.Proposer,
		Txs:      len(b.Transactions),
		Time:     b.CreatedAt,
	})
	if len(c.recent) > recentBlocks {
		c.recent = c.recent[len(c.recent)-recentBlocks:]
	}
	for _, tx := range b.Transactions {
		c.txs = append(c.txs, TxInfo{
			Hash:   hex.EncodeToString(tx.Hash()),
			Height: b.Height,
			From:   tx.From,
			To:     tx.To,
			Amount: tx.Amount,
			Time:   b.CreatedAt,
		})
	}
	if len(c.txs) > recentTxs {
		c.txs = append([]TxInfo(nil), c.txs[len(c.txs)-recentTxs:]...)
	}
	c.emit(Event{Kind: "block", Height: b.Height, Validator: b.Proposer})
}

// recordSlash is called the first time any node slashes a given offender.
func (c *Cluster) recordSlash(addr string, height int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.slashed[addr] {
		return
	}
	c.slashed[addr] = true
	c.emit(Event{Kind: "slash", Height: height, Validator: addr})
}

// recordHeal is called by every node when a scheduled heal takes effect.
// The first call clears the slashed flag and emits one heal event; the
// rest are no-ops.
func (c *Cluster) recordHeal(addr string, height int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.slashed[addr] {
		return
	}
	delete(c.slashed, addr)
	c.emit(Event{Kind: "heal", Height: height, Validator: addr})
}

// recordHalt is called by a node's run loop when it stops on an impossible
// consensus state instead of panicking the shared process.
func (c *Cluster) recordHalt(addr, reason string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.halts = append(c.halts, addr+": "+reason)
	c.emit(Event{Kind: "halt", Height: c.height, Validator: addr})
}

// emit delivers e to every subscriber, dropping it for any whose buffer is
// full rather than stalling consensus. The caller holds c.mu.
func (c *Cluster) emit(e Event) {
	for _, ch := range c.subs {
		select {
		case ch <- e:
		default:
		}
	}
}

// Subscribe returns a channel of cluster events. Call Unsubscribe when
// done, or the channel leaks.
func (c *Cluster) Subscribe() <-chan Event {
	ch := make(chan Event, 64)
	c.mu.Lock()
	c.subs = append(c.subs, ch)
	c.mu.Unlock()
	return ch
}

// Unsubscribe removes a channel returned by Subscribe.
func (c *Cluster) Unsubscribe(ch <-chan Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, s := range c.subs {
		if s == ch {
			c.subs = append(c.subs[:i], c.subs[i+1:]...)
			return
		}
	}
}

// Snapshot returns a consistent view of the cluster: height, supply, the
// recent blocks, and every validator's stake and status.
func (c *Cluster) Snapshot() Snapshot {
	c.mu.Lock()
	s := Snapshot{
		Genesis: c.genesis,
		Height:  c.height,
		Supply:  c.supply,
		Blocks:  append([]BlockInfo{}, c.recent...), // [] not null when empty, for the UI
		Txs:     append([]TxInfo{}, c.txs...),
		Halts:   append([]string{}, c.halts...),
	}
	proposed := make(map[string]int, len(c.proposed))
	for k, v := range c.proposed {
		proposed[k] = v
	}
	slashed := make(map[string]bool, len(c.slashed))
	for k, v := range c.slashed {
		slashed[k] = v
	}
	c.mu.Unlock()

	c.set.mu.RLock()
	total := c.set.totalStake()
	s.Validators = make([]ValidatorInfo, len(c.set.members))
	for i, m := range c.set.members {
		vp := 0.0
		if total > 0 {
			vp = float64(m.stake) / float64(total)
		}
		s.Validators[i] = ValidatorInfo{
			Moniker:     m.moniker,
			Address:     m.address,
			Stake:       m.stake,
			VotingPower: vp,
			Slashed:     slashed[m.address],
			Proposed:    proposed[m.address],
		}
	}
	c.set.mu.RUnlock()
	return s
}

// Account returns an address's current balance and next expected nonce.
func (c *Cluster) Account(addr string) (balance uint64, nonce int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.balances[addr], c.nonces[addr]
}

// Size is the number of validators in the cluster.
func (c *Cluster) Size() int {
	return len(c.nodes)
}

// Chain returns the chain held by validator i, for inspection after a run.
func (c *Cluster) Chain(i int) *Chain {
	return c.nodes[i].chain
}

// MakeFaulty makes validator i double-vote, so the cluster can be seen
// catching and slashing it. The node goes quiet again once its own slash
// takes effect; a heal delay then restores its stake and lets it be
// faulted afresh.
func (c *Cluster) MakeFaulty(i int) {
	c.nodes[i].byzantine.Store(true)
}

// Faulty reports whether validator i is equivocating right now: MakeFaulty
// has been called and the resulting slash has not yet taken effect.
func (c *Cluster) Faulty(i int) bool {
	return c.nodes[i].byzantine.Load()
}

// SetHealDelay sets how many heights after a slash takes effect a
// validator's stake is restored, its double-voting stops, and it can be
// caught again. Zero, the default, leaves a slash permanent. Call it before
// the run starts.
func (c *Cluster) SetHealDelay(heights int64) {
	for _, n := range c.nodes {
		n.healDelay = heights
	}
}

// Evidence returns the equivocation evidence the cluster has gathered, one
// entry per (offender, height), each re-verified.
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
