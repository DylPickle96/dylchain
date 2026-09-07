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
	"net/http"
	"os"
	"os/signal"
	"sort"
	"sync"
	"syscall"
	"time"

	"dyl/chain"
)

func main() {
	var (
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

	httpSrv := &http.Server{Addr: *addr, Handler: srv.routes()}
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

type server struct {
	cluster  *chain.Cluster
	faucet   wallet
	accounts map[string]wallet // name -> wallet, the demo recipients

	mu         sync.Mutex
	nonces     map[string]int64    // server-held wallets: next nonce to use
	seen       map[string]struct{} // addresses observed via /tx and /faucet
	lastFaucet map[string]time.Time
}

func newServer(n int) *server {
	if n < 4 {
		n = 4
	}
	s := &server{
		faucet:     newWallet(),
		accounts:   map[string]wallet{},
		nonces:     map[string]int64{},
		seen:       map[string]struct{}{},
		lastFaucet: map[string]time.Time{},
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
	s.cluster = chain.NewCluster(alloc, set, 0)
	return s
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
			tx := s.sign(from, to.addr, uint64(1+rand.Intn(50))*chain.BlockReward/100)
			_ = s.cluster.Submit(tx)
		}
	}
}

// sign builds and signs a transfer from a server-held wallet, using a
// locally tracked nonce so back-to-back sends do not collide.
func (s *server) sign(from wallet, to string, amount uint64) chain.Transaction {
	s.mu.Lock()
	_, chainNonce := s.cluster.Account(from.addr)
	nonce := chainNonce
	if n := s.nonces[from.addr]; n > nonce {
		nonce = n
	}
	s.nonces[from.addr] = nonce + 1
	s.mu.Unlock()

	tx := chain.Transaction{From: from.addr, To: to, Amount: amount, Nonce: nonce}
	tx.Signature = tx.Sign(from.priv)
	return tx
}

func (s *server) markSeen(addrs ...string) {
	s.mu.Lock()
	for _, a := range addrs {
		if a != "" {
			s.seen[a] = struct{}{}
		}
	}
	s.mu.Unlock()
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/state", s.handleState)
	mux.HandleFunc("/account", s.handleAccount)
	mux.HandleFunc("/events", s.handleEvents)
	mux.HandleFunc("/tx", s.handleTx)
	mux.HandleFunc("/faucet", s.handleFaucet)
	mux.HandleFunc("/fault", s.handleFault)
	mux.Handle("/", staticOrStatus())
	return withCORS(mux)
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
	seen := make([]accountView, 0, len(s.seen))
	for a := range s.seen {
		bal, _ := s.cluster.Account(a)
		seen = append(seen, accountView{Address: a, Balance: bal})
	}
	s.mu.Unlock()
	sort.Slice(seen, func(i, j int) bool { return seen[i].Address < seen[j].Address })

	writeJSON(w, map[string]any{
		"height":     snap.Height,
		"supply":     snap.Supply,
		"blocks":     snap.Blocks,
		"validators": snap.Validators,
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
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := s.cluster.Subscribe()
	defer s.cluster.Unsubscribe(ch)

	enc := json.NewEncoder(w)
	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case e := <-ch:
			fmt.Fprint(w, "data: ")
			_ = enc.Encode(e) // writes the JSON object plus a newline
			fmt.Fprint(w, "\n")
			flusher.Flush()
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
	if err := s.cluster.Submit(tx); err != nil {
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
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Address == "" {
		http.Error(w, "address required", http.StatusBadRequest)
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

	tx := s.sign(s.faucet, body.Address, 100*chain.BlockReward)
	if err := s.cluster.Submit(tx); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.markSeen(body.Address)
	writeJSON(w, map[string]any{"funded": body.Address, "amount": 100 * chain.BlockReward})
}

func (s *server) handleFault(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
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
