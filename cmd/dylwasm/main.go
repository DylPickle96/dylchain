// Command dylwasm runs the whole dyl validator cluster inside a browser
// tab. It is the chain library plus the demo glue from cmd/dyld, minus the
// HTTP layer: instead of an SSE stream and JSON endpoints it exposes a
// handful of functions on the JS global object and pushes events through a
// callback. Build it with scripts/build-wasm.sh.
//
//go:build js && wasm

package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"math"
	"math/rand"
	"sort"
	"sync"
	"syscall/js"
	"time"

	"dyl/chain"
)

const (
	numValidators = 12 // small: every height is a burst of O(n^2) verifies on one wasm thread
	blockTime     = time.Second
	healDelay     = 120 // heights; ~2 minutes at one block a second
	maxBlockTxs   = 64
	maxSeen       = 300
	faucetGrant   = 5_000 * chain.BaseUnitsPerCoin
	faucetCooloff = 30 * time.Second
)

// demoAccounts are the wallets that generate background traffic, with their
// genesis balance in whole DYL. It mirrors cmd/dyld.
var demoAccounts = map[string]uint64{
	"treasury": 1_000_000,
	"exchange": 250_000,
	"alice":    4_200,
	"bob":      1_850,
	"carol":    3_100,
	"dave":     960,
}

var people = []string{"alice", "bob", "carol", "dave"}

type wallet struct {
	priv ed25519.PrivateKey
	addr string
}

func newWallet() wallet {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		panic(err)
	}
	return wallet{priv: priv, addr: chain.AddressFromKey(pub)}
}

// world is the in-page equivalent of cmd/dyld's server: the cluster plus
// the faucet, the demo accounts, the fault budget, and the small pieces of
// state (seen addresses, faucet cool-offs, local nonces) the demo needs.
type world struct {
	cluster   *chain.Cluster
	faucet    wallet
	accounts  map[string]wallet
	origStake []uint64
	origTotal uint64

	faultMu sync.Mutex

	mu         sync.Mutex
	nonces     map[string]int64
	seenList   []string
	seenSet    map[string]bool
	lastFaucet map[string]time.Time
}

var w *world // set once by dylStart

func newWorld() *world {
	x := &world{
		accounts:   map[string]wallet{},
		nonces:     map[string]int64{},
		seenSet:    map[string]bool{},
		lastFaucet: map[string]time.Time{},
	}
	x.faucet = newWallet()
	alloc := map[string]uint64{
		x.faucet.addr: 1_000_000_000 * chain.BaseUnitsPerCoin,
	}
	for name, coins := range demoAccounts {
		wal := newWallet()
		x.accounts[name] = wal
		alloc[wal.addr] = coins * chain.BaseUnitsPerCoin
	}

	set := chain.NewValidatorSet(chain.GenerateValidators(numValidators)...)
	x.cluster = chain.NewCluster(alloc, set, maxBlockTxs)
	x.cluster.PaceBlocks(blockTime)
	x.cluster.SetHealDelay(healDelay)

	snap := x.cluster.Snapshot()
	x.origStake = make([]uint64, len(snap.Validators))
	for i, v := range snap.Validators {
		self := v.Stake - v.Delegated
		x.origStake[i] = self
		x.origTotal += self
	}
	return x
}

// --- event fan-out -----------------------------------------------------------

func emit(e chain.Event) {
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	js.Global().Call("__dylEvent", e.Kind, string(b))
}

func pump(ctx context.Context) {
	ch := w.cluster.Subscribe()
	defer w.cluster.Unsubscribe(ch)
	for {
		select {
		case <-ctx.Done():
			return
		case e := <-ch:
			emit(e)
		}
	}
}

// --- traffic driver (a trimmed copy of cmd/dyld's) --------------------------

func drive(ctx context.Context) {
	t := time.NewTicker(blockTime)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			switch r := rand.Intn(100); {
			case r < 48:
				submitDemo(randomPersonName(), randomPersonAddr, personalAmount())
			case r < 62:
				submitDemo("exchange", randomPersonAddr, dyl(10+rand.Float64()*490))
			case r < 72:
				submitDemo("treasury", randomValidatorAddr, dyl(25+rand.Float64()*75))
			case r < 80:
				submitDemo(randomPersonName(), func(string) string { return w.accounts["exchange"].addr }, personalAmount())
			case r < 90:
				submitStake()
			default:
				for i, n := 0, 3+rand.Intn(4); i < n; i++ {
					submitDemo(randomPersonName(), randomPersonAddr, personalAmount())
				}
			}
		}
	}
}

func submitDemo(from string, pick func(exclude string) string, amount uint64) {
	wal := w.accounts[from]
	to := pick(wal.addr)
	if to == "" || to == wal.addr {
		return
	}
	tx, rollback := w.signFromKind(chain.KindTransfer, wal, to, amount)
	if w.cluster.Submit(tx) != nil {
		rollback()
	}
}

func submitStake() {
	wal := w.accounts[people[rand.Intn(len(people))]]
	bonds := w.cluster.Delegations(wal.addr)
	kind, to, amount := chain.KindDelegate, randomValidatorAddr(""), dyl(25+rand.Float64()*225)
	if len(bonds) > 0 && rand.Intn(2) == 0 {
		for v, have := range bonds {
			kind, to, amount = chain.KindUndelegate, v, have/2+dyl(1)
			if amount > have {
				amount = have
			}
			break
		}
	}
	if to == "" {
		return
	}
	tx, rollback := w.signFromKind(kind, wal, to, amount)
	if w.cluster.Submit(tx) != nil {
		rollback()
	}
}

func randomPersonName() string { return people[rand.Intn(len(people))] }

func randomPersonAddr(exclude string) string {
	for range 8 {
		if a := w.accounts[people[rand.Intn(len(people))]].addr; a != exclude {
			return a
		}
	}
	return ""
}

func randomValidatorAddr(string) string {
	vs := w.cluster.Snapshot().Validators
	for range 8 {
		if v := vs[rand.Intn(len(vs))]; !v.Slashed {
			return v.Address
		}
	}
	return ""
}

func personalAmount() uint64 {
	v := 0.35 * math.Exp(rand.NormFloat64()*1.2)
	return dyl(math.Min(math.Max(v, 0.01), 200))
}

func dyl(amount float64) uint64 {
	return uint64(math.Round(amount*100)) * (chain.BaseUnitsPerCoin / 100)
}

func (x *world) signFromKind(kind string, from wallet, to string, amount uint64) (chain.Transaction, func()) {
	x.mu.Lock()
	_, chainNonce := x.cluster.Account(from.addr)
	nonce := chainNonce
	if n := x.nonces[from.addr]; n > nonce {
		nonce = n
	}
	x.nonces[from.addr] = nonce + 1
	x.mu.Unlock()

	tx := chain.Transaction{Kind: kind, From: from.addr, To: to, Amount: amount, Nonce: nonce}
	tx.Signature = tx.Sign(from.priv)
	return tx, func() {
		x.mu.Lock()
		if x.nonces[from.addr] == nonce+1 {
			x.nonces[from.addr] = nonce
		}
		x.mu.Unlock()
	}
}

func (x *world) markSeen(addrs ...string) {
	x.mu.Lock()
	defer x.mu.Unlock()
	for _, a := range addrs {
		if a == "" || x.seenSet[a] {
			continue
		}
		if len(x.seenList) >= maxSeen {
			delete(x.seenSet, x.seenList[0])
			x.seenList = x.seenList[1:]
		}
		x.seenList = append(x.seenList, a)
		x.seenSet[a] = true
	}
}

// --- JSON views (match cmd/dyld's payloads and web/src/api.ts's types) -----

type accountView struct {
	Name    string `json:"name,omitempty"`
	Address string `json:"address"`
	Balance uint64 `json:"balance"`
}

func (x *world) stateJSON() string {
	snap := x.cluster.Snapshot()

	accounts := make([]accountView, 0, len(x.accounts)+1)
	fb, _ := x.cluster.Account(x.faucet.addr)
	accounts = append(accounts, accountView{Name: "faucet", Address: x.faucet.addr, Balance: fb})
	names := make([]string, 0, len(x.accounts))
	for name := range x.accounts {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		bal, _ := x.cluster.Account(x.accounts[name].addr)
		accounts = append(accounts, accountView{Name: name, Address: x.accounts[name].addr, Balance: bal})
	}

	x.mu.Lock()
	seenAddrs := append([]string(nil), x.seenList...)
	x.mu.Unlock()
	sort.Strings(seenAddrs)
	seen := make([]accountView, 0, len(seenAddrs))
	for _, a := range seenAddrs {
		bal, _ := x.cluster.Account(a)
		seen = append(seen, accountView{Address: a, Balance: bal})
	}

	b, _ := json.Marshal(map[string]any{
		"genesis":     snap.Genesis,
		"height":      snap.Height,
		"supply":      snap.Supply,
		"pending":     snap.Pending,
		"blockReward": uint64(chain.BlockReward),
		"faucetGrant": uint64(faucetGrant),
		"blocks":      snap.Blocks,
		"txs":         snap.Txs,
		"validators":  snap.Validators,
		"halts":       snap.Halts,
		"accounts":    accounts,
		"seen":        seen,
	})
	return string(b)
}

func (x *world) accountJSON(addr string) string {
	bal, nonce := x.cluster.Account(addr)
	b, _ := json.Marshal(map[string]any{
		"address":     addr,
		"balance":     bal,
		"nonce":       nonce,
		"delegations": x.cluster.Delegations(addr),
	})
	return string(b)
}

func (x *world) proofJSON(height int64, hash string) (string, bool) {
	leaf, index, siblings, root, ok := x.cluster.InclusionProof(height, hash)
	if !ok {
		return "", false
	}
	sibs := make([]string, len(siblings))
	for i, s := range siblings {
		sibs[i] = hex.EncodeToString(s)
	}
	b, _ := json.Marshal(map[string]any{
		"height":   height,
		"leaf":     hex.EncodeToString(leaf),
		"index":    index,
		"siblings": sibs,
		"root":     hex.EncodeToString(root),
	})
	return string(b), true
}

// --- request handlers ------------------------------------------------------

func (x *world) submitTx(raw string) error {
	var tx chain.Transaction
	if err := json.Unmarshal([]byte(raw), &tx); err != nil {
		return err
	}
	if err := x.cluster.Submit(tx); err != nil {
		return err
	}
	x.markSeen(tx.From, tx.To)
	return nil
}

func (x *world) faucetTo(addr string) error {
	if !chain.ValidAddress(addr) || addr == x.faucet.addr {
		return errString("a valid, non-faucet address is required")
	}
	x.mu.Lock()
	if time.Since(x.lastFaucet[addr]) < faucetCooloff {
		x.mu.Unlock()
		return errString("faucet cool-off, try again in a moment")
	}
	x.lastFaucet[addr] = time.Now()
	x.mu.Unlock()

	tx, rollback := x.signFromKind(chain.KindTransfer, x.faucet, addr, faucetGrant)
	if err := x.cluster.Submit(tx); err != nil {
		rollback()
		return err
	}
	x.markSeen(addr)
	return nil
}

func (x *world) fault(index int) error {
	if index < 0 || index >= x.cluster.Size() {
		return errString("index out of range")
	}
	x.faultMu.Lock()
	defer x.faultMu.Unlock()

	if x.cluster.Faulty(index) {
		return nil // already equivocating
	}
	snap := x.cluster.Snapshot()
	lost := x.origStake[index]
	for i, v := range snap.Validators {
		if i == index {
			continue
		}
		if v.Slashed || x.cluster.Faulty(i) {
			lost += x.origStake[i]
		}
	}
	if 3*lost >= x.origTotal {
		return errString("fault budget reached: any more would stall consensus")
	}
	x.cluster.MakeFaulty(index)
	return nil
}

type errString string

func (e errString) Error() string { return string(e) }

// --- js bindings ---------------------------------------------------------

func result(err error) any {
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	return map[string]any{"ok": true}
}

func start(this js.Value, args []js.Value) any {
	if w != nil {
		return nil
	}
	w = newWorld()
	ctx := context.Background()
	go w.cluster.RunContext(ctx)
	go pump(ctx)
	go drive(ctx)
	return nil
}

func state(js.Value, []js.Value) any {
	if w == nil {
		return "{}"
	}
	return w.stateJSON()
}

func account(this js.Value, args []js.Value) any {
	if w == nil || len(args) < 1 {
		return "{}"
	}
	return w.accountJSON(args[0].String())
}

func proof(this js.Value, args []js.Value) any {
	if w == nil || len(args) < 2 {
		return map[string]any{"error": "not ready"}
	}
	s, ok := w.proofJSON(int64(args[0].Int()), args[1].String())
	if !ok {
		return map[string]any{"error": "no such transaction in a recent block"}
	}
	return s
}

func submitTx(this js.Value, args []js.Value) any {
	if w == nil || len(args) < 1 {
		return result(errString("not ready"))
	}
	return result(w.submitTx(args[0].String()))
}

func faucet(this js.Value, args []js.Value) any {
	if w == nil || len(args) < 1 {
		return result(errString("not ready"))
	}
	return result(w.faucetTo(args[0].String()))
}

func fault(this js.Value, args []js.Value) any {
	if w == nil || len(args) < 1 {
		return result(errString("not ready"))
	}
	return result(w.fault(args[0].Int()))
}

func main() {
	g := js.Global()
	g.Set("dylStart", js.FuncOf(start))
	g.Set("dylState", js.FuncOf(state))
	g.Set("dylAccount", js.FuncOf(account))
	g.Set("dylProof", js.FuncOf(proof))
	g.Set("dylSubmitTx", js.FuncOf(submitTx))
	g.Set("dylFaucet", js.FuncOf(faucet))
	g.Set("dylFault", js.FuncOf(fault))
	g.Call("__dylReady")
	select {} // keep the runtime alive; a return here kills every binding
}
