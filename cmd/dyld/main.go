// Command dyld runs a dyl validator cluster in-process and serves a small
// HTTP + SSE API for the explorer UI to watch and poke it.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sort"
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
	)
	flag.Parse()

	srv := newServer(*validators)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	srv.cluster.PaceBlocks(*blockTime)
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
	maxViewersPerIP   = 3                // /events streams from one address
	streamMaxLifetime = 30 * time.Minute // then the /events connection is closed
	maxSeen           = 500              // addresses kept in the seen list
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

	mu          sync.Mutex
	nonces      map[string]int64 // server-held wallets: next nonce to use
	seenList    []string         // addresses observed via /tx and /faucet, oldest first
	seenSet     map[string]bool
	lastFaucet  map[string]time.Time
	faulty      map[int]bool   // validators MakeFaulty has been called on
	viewersByIP map[string]int // live /events connections per address
}

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
		faulty:      map[int]bool{},
		viewersByIP: map[string]int{},
	}

	alloc := map[string]uint64{
		s.faucet.addr: 1_000_000_000 * chain.BlockReward, // effectively unlimited
	}
	for _, name := range []string{"treasury", "alice", "bob", "carol"} {
		w := newWallet()
		s.accounts[name] = w
		alloc[w.addr] = 1_000 * chain.BlockReward
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

// drive submits a small transfer between two random demo accounts on a
// timer, so blocks are not always empty.
func (s *server) drive(ctx context.Context, every time.Duration) {
	names := make([]string, 0, len(s.accounts))
	for name := range s.accounts {
		names = append(names, name)
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			from := s.accounts[names[rand.Intn(len(names))]]
			to := s.accounts[names[rand.Intn(len(names))]]
			if from.addr == to.addr {
				continue
			}
			tx, rollback := s.signFrom(from, to.addr, uint64(1+rand.Intn(50))*chain.BlockReward/100)
			if s.cluster.Submit(tx) != nil {
				rollback()
			}
		}
	}
}

// signFrom builds and signs a transfer from a server-held wallet, reserving
// the next nonce. It returns a rollback to call if the transaction is not
// accepted, so a rejected send does not leave the local nonce ahead of the
// chain forever.
func (s *server) signFrom(from wallet, to string, amount uint64) (tx chain.Transaction, rollback func()) {
	s.mu.Lock()
	_, chainNonce := s.cluster.Account(from.addr)
	nonce := chainNonce
	if n := s.nonces[from.addr]; n > nonce {
		nonce = n
	}
	s.nonces[from.addr] = nonce + 1
	s.mu.Unlock()

	tx = chain.Transaction{From: from.addr, To: to, Amount: amount, Nonce: nonce}
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
		"height":     snap.Height,
		"supply":     snap.Supply,
		"blocks":     snap.Blocks,
		"validators": snap.Validators,
		"halts":      snap.Halts,
		"accounts":   accounts,
		"seen":       seen,
	})
}

func (s *server) handleAccount(w http.ResponseWriter, r *http.Request) {
	addr := r.URL.Query().Get("address")
	if addr == "" {
		http.Error(w, "address required", http.StatusBadRequest)
		return
	}
	bal, nonce := s.cluster.Account(addr)
	writeJSON(w, map[string]any{"address": addr, "balance": bal, "nonce": nonce})
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

	tx, rollback := s.signFrom(s.faucet, body.Address, 100*chain.BlockReward)
	if err := s.cluster.Submit(tx); err != nil {
		rollback()
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.markSeen(body.Address)
	writeJSON(w, map[string]any{"funded": body.Address, "amount": 100 * chain.BlockReward})
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

	s.mu.Lock()
	if s.faulty[body.Index] {
		s.mu.Unlock()
		w.WriteHeader(http.StatusAccepted) // already faulty, nothing to do
		return
	}
	s.mu.Unlock()

	// Refuse if faulty-plus-slashed stake would reach a third of the
	// original total, so the honest set can always still make two thirds
	// and the demo stays recoverable. The Snapshot is only reached for a
	// genuinely new fault.
	snap := s.cluster.Snapshot()
	s.mu.Lock()
	lost := s.origStake[body.Index]
	for i, v := range snap.Validators {
		if v.Slashed || s.faulty[i] {
			lost += s.origStake[i]
		}
	}
	if 3*lost >= s.origTotal {
		s.mu.Unlock()
		http.Error(w, "fault budget reached: any more would stall consensus", http.StatusConflict)
		return
	}
	s.faulty[body.Index] = true
	s.mu.Unlock()

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
		return http.FileServer(http.Dir("web/dist"))
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprintln(w, "dyld is running. web/dist not built yet; try /state, /events.")
	})
}
