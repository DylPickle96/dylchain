// Command dyld runs a dyl validator cluster in-process and serves a small
// HTTP + SSE API for the explorer UI to watch and poke it.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"math"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"dyl/chain"
)

func main() {
	var (
		// validators is operator-only. Cluster cost is O(n^2) signature
		// verifications per height, so this must never become
		// request-controlled.
		validators = flag.Int("validators", 20, "number of validators")
		addr       = flag.String("addr", ":8080", "HTTP listen address")
		blockTime  = flag.Duration("block-time", time.Second, "minimum time between blocks")
		txEvery    = flag.Duration("tx-every", 2*time.Second, "how often the driver submits a transaction")
		healAfter  = flag.Int64("heal-after", healDelay, "heights after a slash to restore a validator; 0 keeps it permanent")
	)
	flag.Parse()

	srv := newServer(*validators)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	srv.cluster.PaceBlocks(*blockTime)
	srv.cluster.SetHealDelay(*healAfter)
	go srv.cluster.RunContext(ctx)
	go srv.drive(ctx, *txEvery)

	httpSrv := &http.Server{
		Addr:              *addr,
		Handler:           srv.routes(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       2 * time.Minute,
		// no WriteTimeout: it would cut off the /events stream
	}
	go func() {
		log.Printf("dyld listening on %s with %d validators", *addr, *validators)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()

	<-ctx.Done()
	log.Print("shutting down")
	shutCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutCtx)
}

// wallet is a key pair the server holds: the faucet and the named demo
// accounts.
type wallet struct {
	priv ed25519.PrivateKey
	addr string
}

func newWallet() wallet {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		log.Fatal(err)
	}
	return wallet{priv: priv, addr: chain.AddressFromKey(pub)}
}

const (
	maxBlockTxs       = 64               // cap proposer work per block regardless of mempool size
	maxViewers        = 512              // /events streams in total
	maxViewersPerIP   = 16               // /events streams from one address (NAT, a proxy, or dev churn share one)
	streamMaxLifetime = 30 * time.Minute // then the /events connection is closed
	maxSeen           = 500              // addresses kept in the seen list

	// faucetGrant is what POST /faucet sends a visitor, per 30s per address.
	// Enough to make a delegation a visible slice of a validator.
	faucetGrant = 5_000 * chain.BaseUnitsPerCoin
)

type server struct {
	cluster  *chain.Cluster
	faucet   wallet
	accounts map[string]wallet // name -> wallet, the demo recipients
	writes   *ipLimiters       // per-IP rate limit for /tx, /faucet, /fault
	reads    *ipLimiters       // looser per-IP rate limit for /state, /account
	viewers  atomic.Int64      // live /events connections in total

	origStake []uint64 // per-validator stake at genesis, for the fault budget
	origTotal uint64

	faultMu sync.Mutex // serialises the /fault budget check with MakeFaulty

	mu          sync.Mutex
	nonces      map[string]int64 // server-held wallets: next nonce to use
	seenList    []string         // addresses observed via /tx and /faucet, oldest first
	seenSet     map[string]bool
	lastFaucet  map[string]time.Time
	viewersByIP map[string]int // live /events connections per address
}

// healDelay is how many heights after a slash the demo restores a
// validator: stake back, double-voting stopped, catchable again. At the
// default one-second block time that is about two minutes, long enough to
// watch the slash land, short enough that an unattended demo heals itself.
const healDelay = 120

func newServer(n int) *server {
	if n < 4 {
		n = 4
	}
	s := &server{
		faucet:      newWallet(),
		accounts:    map[string]wallet{},
		writes:      newIPLimiters(5, 20, 4096),  // 5/s, burst 20, per IP; 4096 IPs tracked
		reads:       newIPLimiters(30, 60, 4096), // looser, for the polled read endpoints
		nonces:      map[string]int64{},
		seenSet:     map[string]bool{},
		lastFaucet:  map[string]time.Time{},
		viewersByIP: map[string]int{},
	}

	alloc := map[string]uint64{
		s.faucet.addr: 1_000_000_000 * chain.BaseUnitsPerCoin, // effectively unlimited
	}
	for name, coins := range demoAccounts {
		w := newWallet()
		s.accounts[name] = w
		alloc[w.addr] = coins * chain.BaseUnitsPerCoin
	}

	set := chain.NewValidatorSet(chain.GenerateValidators(n)...)
	s.cluster = chain.NewCluster(alloc, set, maxBlockTxs)

	genesis := s.cluster.Snapshot() // stakes here are pre-slash originals
	s.origStake = make([]uint64, len(genesis.Validators))
	for i, v := range genesis.Validators {
		s.origStake[i] = v.Stake
		s.origTotal += v.Stake
	}
	return s
}

// limiter is a token bucket: rate tokens accrue per second up to burst, and
// allow spends one.
type limiter struct {
	mu     sync.Mutex
	tokens float64
	burst  float64
	rate   float64
	last   time.Time
}

func newLimiter(rate, burst float64) *limiter {
	return &limiter{tokens: burst, burst: burst, rate: rate, last: time.Now()}
}

func (l *limiter) allow() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	l.tokens += now.Sub(l.last).Seconds() * l.rate
	if l.tokens > l.burst {
		l.tokens = l.burst
	}
	l.last = now
	if l.tokens < 1 {
		return false
	}
	l.tokens--
	return true
}

// ipLimiters is a bucket per client address in a bounded FIFO, plus a
// global bucket so a spread of IPs still cannot exceed a total rate. When
// the map is full the oldest IP is dropped.
type ipLimiters struct {
	mu          sync.Mutex
	m           map[string]*limiter
	order       []string
	rate, burst float64
	max         int
	global      *limiter
}

func newIPLimiters(rate, burst float64, max int) *ipLimiters {
	return &ipLimiters{
		m:      map[string]*limiter{},
		rate:   rate,
		burst:  burst,
		max:    max,
		global: newLimiter(rate*20, burst*10),
	}
}

func (l *ipLimiters) allow(ip string) bool {
	if !l.global.allow() {
		return false
	}
	l.mu.Lock()
	lim := l.m[ip]
	if lim == nil {
		if len(l.order) >= l.max {
			delete(l.m, l.order[0])
			l.order = l.order[1:]
		}
		lim = newLimiter(l.rate, l.burst)
		l.m[ip] = lim
		l.order = append(l.order, ip)
	}
	l.mu.Unlock()
	return lim.allow()
}

// clientIP is the request's source address without the port. It trusts
// RemoteAddr only: no X-Forwarded-For, so a proxy would need its own limit.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// demoAccounts are the server-held wallets that generate background
// traffic, with their genesis balance in DYL. The treasury and the exchange
// are the whales; the rest are people.
var demoAccounts = map[string]uint64{
	"treasury": 1_000_000,
	"exchange": 250_000,
	"alice":    4_200,
	"bob":      1_850,
	"carol":    3_100,
	"dave":     960,
}

// people are the demo accounts that send everyday transfers.
var people = []string{"alice", "bob", "carol", "dave"}

// drive submits background traffic so the chain is never idle. Each tick
// it picks a pattern: mostly small transfers between people, sometimes an
// exchange withdrawal, a treasury grant to a validator, or a burst of
// several transfers at once so blocks vary in size.
func (s *server) drive(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			switch r := rand.Intn(100); {
			case r < 48:
				s.submitDemo(s.randomPerson(""), s.randomPerson, personalAmount())
			case r < 62:
				s.submitDemo("exchange", s.randomPerson, dyl(10+rand.Float64()*490))
			case r < 72:
				s.submitDemo("treasury", s.randomValidator, dyl(25+rand.Float64()*75))
			case r < 80:
				s.submitDemo(s.randomPerson(""), func(string) string { return s.accounts["exchange"].addr }, personalAmount())
			case r < 90:
				s.submitStake() // a person delegates to or undelegates from a validator
			default:
				for i, n := 0, 3+rand.Intn(4); i < n; i++ {
					s.submitDemo(s.randomPerson(""), s.randomPerson, personalAmount())
				}
			}
		}
	}
}

// submitDemo signs a transfer from a named account to whatever pick
// returns (given the sender's address, so it can avoid a self-send) and
// submits it, rolling the nonce back if the mempool refuses it.
func (s *server) submitDemo(from string, pick func(exclude string) string, amount uint64) {
	w := s.accounts[from]
	to := pick(w.addr)
	if to == "" || to == w.addr {
		return
	}
	tx, rollback := s.signFrom(w, to, amount)
	if s.cluster.Submit(tx) != nil {
		rollback()
	}
}

// submitStake has a random person delegate to a validator, or, half the
// time and only if they already have a bond, undelegate part of one. It
// keeps the validator table showing live delegated stake.
func (s *server) submitStake() {
	w := s.accounts[people[rand.Intn(len(people))]]
	bonds := s.cluster.Delegations(w.addr)

	kind, to, amount := chain.KindDelegate, s.randomValidator(""), dyl(25+rand.Float64()*225)
	if len(bonds) > 0 && rand.Intn(2) == 0 {
		for v, have := range bonds { // range over a map: an arbitrary existing bond
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
	tx, rollback := s.signFromKind(kind, w, to, amount)
	if s.cluster.Submit(tx) != nil {
		rollback()
	}
}

// randomPerson returns a person's name (when exclude is empty) or address
// (otherwise, never the excluded one).
func (s *server) randomPerson(exclude string) string {
	if exclude == "" {
		return people[rand.Intn(len(people))]
	}
	for {
		if a := s.accounts[people[rand.Intn(len(people))]].addr; a != exclude {
			return a
		}
	}
}

// randomValidator returns a random unslashed validator's address.
func (s *server) randomValidator(string) string {
	vs := s.cluster.Snapshot().Validators
	for range 8 {
		if v := vs[rand.Intn(len(vs))]; !v.Slashed {
			return v.Address
		}
	}
	return ""
}

// personalAmount is a log-normal-ish everyday transfer, mostly well under
// a DYL, occasionally a few, rarely tens, capped at 200 DYL.
func personalAmount() uint64 {
	v := 0.35 * math.Exp(rand.NormFloat64()*1.2)
	return dyl(math.Min(math.Max(v, 0.01), 200))
}

// dyl converts a DYL amount to base units, rounded to a hundredth of a DYL
// so the feed reads like money rather than noise.
func dyl(amount float64) uint64 {
	return uint64(math.Round(amount*100)) * (chain.BaseUnitsPerCoin / 100)
}

// signFrom builds and signs a transfer from a server-held wallet, reserving
// the next nonce. It returns a rollback to call if the transaction is not
// accepted, so a rejected send does not leave the local nonce ahead of the
// chain forever.
func (s *server) signFrom(from wallet, to string, amount uint64) (tx chain.Transaction, rollback func()) {
	return s.signFromKind(chain.KindTransfer, from, to, amount)
}

// signFromKind is signFrom for any transaction kind.
func (s *server) signFromKind(kind string, from wallet, to string, amount uint64) (tx chain.Transaction, rollback func()) {
	s.mu.Lock()
	_, chainNonce := s.cluster.Account(from.addr)
	nonce := chainNonce
	if n := s.nonces[from.addr]; n > nonce {
		nonce = n
	}
	s.nonces[from.addr] = nonce + 1
	s.mu.Unlock()

	tx = chain.Transaction{Kind: kind, From: from.addr, To: to, Amount: amount, Nonce: nonce}
	tx.Signature = tx.Sign(from.priv)
	return tx, func() {
		s.mu.Lock()
		if s.nonces[from.addr] == nonce+1 {
			s.nonces[from.addr] = nonce
		}
		s.mu.Unlock()
	}
}

// markSeen records addresses in a bounded FIFO. Submit has already checked
// that anything passed here is a well-formed address.
func (s *server) markSeen(addrs ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range addrs {
		if a == "" || s.seenSet[a] {
			continue
		}
		if len(s.seenList) >= maxSeen {
			delete(s.seenSet, s.seenList[0])
			s.seenList = s.seenList[1:]
		}
		s.seenList = append(s.seenList, a)
		s.seenSet[a] = true
	}
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/state", s.readLimited(s.handleState))
	mux.HandleFunc("/account", s.readLimited(s.handleAccount))
	mux.HandleFunc("/proof", s.readLimited(s.handleProof))
	mux.HandleFunc("/events", s.handleEvents)
	mux.HandleFunc("/tx", s.writeLimited(s.handleTx))
	mux.HandleFunc("/faucet", s.writeLimited(s.handleFaucet))
	mux.HandleFunc("/fault", s.writeLimited(s.handleFault))
	mux.Handle("/", staticOrStatus())
	return withCORS(maxBody(mux))
}

// writeLimited fronts a POST-only handler with the per-IP write rate
// limiter. The method check comes first so a wrong-method request cannot
// spend a token.
func (s *server) writeLimited(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		if !s.writes.allow(clientIP(r)) {
			http.Error(w, "rate limited, slow down", http.StatusTooManyRequests)
			return
		}
		h(w, r)
	}
}

// readLimited fronts a GET read endpoint with the looser per-IP read
// limiter, so a scraper cannot pin CPU polling /state.
func (s *server) readLimited(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "GET only", http.StatusMethodNotAllowed)
			return
		}
		if !s.reads.allow(clientIP(r)) {
			http.Error(w, "rate limited, slow down", http.StatusTooManyRequests)
			return
		}
		h(w, r)
	}
}

// maxBody caps request bodies so a giant POST cannot exhaust memory.
func maxBody(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
		h.ServeHTTP(w, r)
	})
}

type accountView struct {
	Name    string `json:"name,omitempty"`
	Address string `json:"address"`
	Balance uint64 `json:"balance"`
}

func (s *server) handleState(w http.ResponseWriter, _ *http.Request) {
	snap := s.cluster.Snapshot()

	accounts := make([]accountView, 0, len(s.accounts)+1)
	faucetBal, _ := s.cluster.Account(s.faucet.addr)
	accounts = append(accounts, accountView{Name: "faucet", Address: s.faucet.addr, Balance: faucetBal})
	names := make([]string, 0, len(s.accounts))
	for name := range s.accounts {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		bal, _ := s.cluster.Account(s.accounts[name].addr)
		accounts = append(accounts, accountView{Name: name, Address: s.accounts[name].addr, Balance: bal})
	}

	s.mu.Lock()
	seenAddrs := append([]string(nil), s.seenList...)
	s.mu.Unlock()
	seen := make([]accountView, 0, len(seenAddrs))
	for _, a := range seenAddrs {
		bal, _ := s.cluster.Account(a)
		seen = append(seen, accountView{Address: a, Balance: bal})
	}
	sort.Slice(seen, func(i, j int) bool { return seen[i].Address < seen[j].Address })

	writeJSON(w, map[string]any{
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
}

func (s *server) handleAccount(w http.ResponseWriter, r *http.Request) {
	addr := r.URL.Query().Get("address")
	if addr == "" {
		http.Error(w, "address required", http.StatusBadRequest)
		return
	}
	bal, nonce := s.cluster.Account(addr)
	writeJSON(w, map[string]any{
		"address":     addr,
		"balance":     bal,
		"nonce":       nonce,
		"delegations": s.cluster.Delegations(addr), // validator address -> bonded, or null
	})
}

// handleProof returns a Merkle inclusion proof for one transaction in one
// recent block: GET /proof?height=N&hash=HEX. The client folds the leaf up
// through the siblings and checks it against the root, which is the block's
// TxRoot. 404 if the block has scrolled out of the recent window or holds
// no such transaction.
func (s *server) handleProof(w http.ResponseWriter, r *http.Request) {
	height, err := strconv.ParseInt(r.URL.Query().Get("height"), 10, 64)
	if err != nil || height < 1 {
		http.Error(w, "height required", http.StatusBadRequest)
		return
	}
	hash := r.URL.Query().Get("hash")
	if len(hash) != 64 {
		http.Error(w, "hash required (64 hex characters)", http.StatusBadRequest)
		return
	}

	leaf, index, siblings, root, ok := s.cluster.InclusionProof(height, hash)
	if !ok {
		http.Error(w, "no such transaction in a recent block", http.StatusNotFound)
		return
	}
	sibs := make([]string, len(siblings))
	for i, s := range siblings {
		sibs[i] = hex.EncodeToString(s)
	}
	writeJSON(w, map[string]any{
		"height":   height,
		"leaf":     hex.EncodeToString(leaf),
		"index":    index,
		"siblings": sibs,
		"root":     hex.EncodeToString(root),
	})
}

func (s *server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	if s.viewers.Add(1) > maxViewers {
		s.viewers.Add(-1)
		http.Error(w, "too many live viewers, try again shortly", http.StatusServiceUnavailable)
		return
	}
	defer s.viewers.Add(-1)

	ip := clientIP(r)
	s.mu.Lock()
	if s.viewersByIP[ip] >= maxViewersPerIP {
		s.mu.Unlock()
		http.Error(w, "too many streams from your address", http.StatusServiceUnavailable)
		return
	}
	s.viewersByIP[ip]++
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if s.viewersByIP[ip]--; s.viewersByIP[ip] <= 0 {
			delete(s.viewersByIP, ip)
		}
		s.mu.Unlock()
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := s.cluster.Subscribe()
	defer s.cluster.Unsubscribe(ch)

	rc := http.NewResponseController(w)
	life := time.NewTimer(streamMaxLifetime)
	defer life.Stop()
	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()

	// write pushes a frame with a short deadline, so a client that stops
	// reading errors out instead of parking this goroutine forever.
	write := func(frame string) bool {
		_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if _, err := fmt.Fprint(w, frame); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	// Tell the browser to retry quickly if the stream drops.
	if !write("retry: 3000\n\n") {
		return
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case <-life.C:
			return
		case <-ping.C:
			if !write(": ping\n\n") {
				return
			}
		case e := <-ch:
			b, _ := json.Marshal(e)
			if !write("data: " + string(b) + "\n\n") {
				return
			}
		}
	}
}

func (s *server) handleTx(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var tx chain.Transaction
	if err := json.NewDecoder(r.Body).Decode(&tx); err != nil {
		http.Error(w, "bad JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	switch err := s.cluster.Submit(tx); {
	case err == nil:
	case errors.Is(err, chain.ErrMempoolFull):
		http.Error(w, err.Error(), http.StatusTooManyRequests)
		return
	default:
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.markSeen(tx.From, tx.To)
	w.WriteHeader(http.StatusAccepted)
}

func (s *server) handleFaucet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Address string `json:"address"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad JSON", http.StatusBadRequest)
		return
	}
	if !chain.ValidAddress(body.Address) || body.Address == s.faucet.addr {
		http.Error(w, "a valid, non-faucet address is required", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	last := s.lastFaucet[body.Address]
	if time.Since(last) < 30*time.Second {
		s.mu.Unlock()
		http.Error(w, "faucet rate limit, try again in a moment", http.StatusTooManyRequests)
		return
	}
	s.lastFaucet[body.Address] = time.Now()
	s.mu.Unlock()

	tx, rollback := s.signFrom(s.faucet, body.Address, faucetGrant)
	if err := s.cluster.Submit(tx); err != nil {
		rollback()
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.markSeen(body.Address)
	writeJSON(w, map[string]any{"funded": body.Address, "amount": faucetGrant})
}

func (s *server) handleFault(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Index int `json:"index"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad JSON", http.StatusBadRequest)
		return
	}
	if body.Index < 0 || body.Index >= s.cluster.Size() {
		http.Error(w, "index out of range", http.StatusBadRequest)
		return
	}

	// faultMu serialises the whole check-then-act so two concurrent
	// requests cannot both pass a budget that only one of them fits.
	s.faultMu.Lock()
	defer s.faultMu.Unlock()

	if s.cluster.Faulty(body.Index) {
		w.WriteHeader(http.StatusAccepted) // already equivocating, nothing to do
		return
	}

	// Refuse if faulty-plus-slashed stake would reach a third of the
	// original total, so the honest set can always still make two thirds.
	// Both inputs come from live cluster state, so once a validator heals
	// its budget is handed back and /fault opens up again on its own.
	snap := s.cluster.Snapshot()
	lost := s.origStake[body.Index]
	for i, v := range snap.Validators {
		if i == body.Index {
			continue
		}
		if v.Slashed || s.cluster.Faulty(i) {
			lost += s.origStake[i]
		}
	}
	if 3*lost >= s.origTotal {
		http.Error(w, "fault budget reached: any more would stall consensus", http.StatusConflict)
		return
	}

	s.cluster.MakeFaulty(body.Index)
	w.WriteHeader(http.StatusAccepted)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func withCORS(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// staticOrStatus serves the built UI from web/dist if it is there, and a
// plain status line otherwise.
func staticOrStatus() http.Handler {
	if info, err := os.Stat("web/dist"); err == nil && info.IsDir() {
		return http.FileServer(guardedDir{http.Dir("web/dist")})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprintln(w, "dyld is running. web/dist not built yet; try /state, /events.")
	})
}

// guardedDir is an http.FileSystem that hides dotfiles and refuses to list
// directories: a request for a directory without an index.html reads as not
// found rather than a browsable listing.
type guardedDir struct{ inner http.FileSystem }

func (d guardedDir) Open(name string) (http.File, error) {
	for part := range strings.SplitSeq(name, "/") {
		if strings.HasPrefix(part, ".") && part != "." && part != ".." {
			return nil, fs.ErrNotExist
		}
	}
	f, err := d.inner.Open(name)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if info.IsDir() {
		index := strings.TrimSuffix(name, "/") + "/index.html"
		if idx, err := d.inner.Open(index); err != nil {
			f.Close()
			return nil, fs.ErrNotExist
		} else {
			idx.Close()
		}
	}
	return f, nil
}
