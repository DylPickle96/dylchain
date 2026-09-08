package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"dyl/chain"
)

func TestServerEndpoints(t *testing.T) {
	s := newServer(4)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go s.cluster.RunContext(ctx)

	ts := httptest.NewServer(s.routes())
	t.Cleanup(ts.Close)

	waitFor(t, 5*time.Second, func() bool {
		var st struct {
			Height int64 `json:"height"`
		}
		getJSON(t, ts.URL+"/state", &st)
		return st.Height >= 3
	}, "no blocks produced")

	// /account: a demo account is funded from genesis.
	alice := s.accounts["alice"].addr
	var acc struct {
		Balance uint64 `json:"balance"`
		Nonce   int64  `json:"nonce"`
	}
	getJSON(t, ts.URL+"/account?address="+alice, &acc)
	if acc.Balance == 0 {
		t.Error("alice has no balance")
	}

	// /events streams block events.
	select {
	case line := <-readFirstEvent(t, ts.URL+"/events"):
		if !strings.Contains(line, `"kind":"block"`) {
			t.Errorf("first event was %q, want a block event", line)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no event on the stream")
	}

	// POST /tx: a well-signed transfer is accepted.
	good, _ := s.signFrom(s.accounts["alice"], s.accounts["bob"].addr, chain.BlockReward)
	if code := postJSON(t, ts.URL+"/tx", good); code != http.StatusAccepted {
		t.Errorf("POST /tx (good): got %d, want 202", code)
	}

	// POST /tx: a broken signature is rejected.
	good.Signature[0] ^= 0xff
	if code := postJSON(t, ts.URL+"/tx", good); code != http.StatusBadRequest {
		t.Errorf("POST /tx (bad sig): got %d, want 400", code)
	}

	// POST /faucet funds an arbitrary address.
	newAddr := newWallet().addr
	if code := postJSON(t, ts.URL+"/faucet", map[string]string{"address": newAddr}); code != http.StatusOK {
		t.Errorf("POST /faucet: got %d, want 200", code)
	}
	waitFor(t, 5*time.Second, func() bool {
		var a struct {
			Balance uint64 `json:"balance"`
		}
		getJSON(t, ts.URL+"/account?address="+newAddr, &a)
		return a.Balance > 0
	}, "faucet transfer never landed")

	// POST /fault: the named validator gets slashed.
	if code := postJSON(t, ts.URL+"/fault", map[string]int{"index": 3}); code != http.StatusAccepted {
		t.Errorf("POST /fault: got %d, want 202", code)
	}
	waitFor(t, 5*time.Second, func() bool {
		var st struct {
			Validators []struct {
				Slashed bool `json:"slashed"`
			} `json:"validators"`
		}
		getJSON(t, ts.URL+"/state", &st)
		return len(st.Validators) > 3 && st.Validators[3].Slashed
	}, "validator 3 never slashed")
}

// markSeen keeps a bounded FIFO: full means the oldest address is evicted.
func TestMarkSeenBounded(t *testing.T) {
	s := newServer(4)
	for i := 0; i < maxSeen+50; i++ {
		s.markSeen(fmt.Sprintf("addr-%d", i))
	}
	if len(s.seenList) != maxSeen || len(s.seenSet) != maxSeen {
		t.Fatalf("seen sizes: list %d set %d, want %d each", len(s.seenList), len(s.seenSet), maxSeen)
	}
	if s.seenSet["addr-0"] {
		t.Error("oldest address was not evicted")
	}
	if !s.seenSet[fmt.Sprintf("addr-%d", maxSeen+49)] {
		t.Error("newest address is missing")
	}
	// A duplicate does not grow the list.
	s.markSeen(fmt.Sprintf("addr-%d", maxSeen+49))
	if len(s.seenList) != maxSeen {
		t.Errorf("duplicate grew the list to %d", len(s.seenList))
	}
}

// Per-IP buckets are independent, and evicting an IP from the bounded map
// resets its bucket.
func TestIPLimiters(t *testing.T) {
	l := newIPLimiters(1000, 2, 2) // burst 2, track 2 IPs

	grants := func(ip string, n int) int {
		got := 0
		for i := 0; i < n; i++ {
			if l.allow(ip) {
				got++
			}
		}
		return got
	}

	if got := grants("a", 3); got != 2 {
		t.Errorf("IP a: %d of 3 granted, want 2 (its burst)", got)
	}
	if got := grants("b", 2); got != 2 {
		t.Errorf("IP b: %d of 2 granted, want 2 (its own burst)", got)
	}
	l.allow("c") // evicts a, the oldest
	if got := grants("a", 1); got != 1 {
		t.Error("evicted IP a did not get a fresh bucket")
	}
}

// The token bucket allows a burst, then denies, then recovers as tokens
// refill.
func TestLimiter(t *testing.T) {
	l := newLimiter(5, 3)
	for i := 0; i < 3; i++ {
		if !l.allow() {
			t.Fatalf("burst token %d denied", i)
		}
	}
	if l.allow() {
		t.Error("call past the burst was allowed")
	}
	time.Sleep(300 * time.Millisecond) // ~1.5 tokens back
	if !l.allow() {
		t.Error("no token after refill")
	}
}

// A burst of writes from one address eventually gets a 429.
func TestWriteRateLimit(t *testing.T) {
	s := newServer(4)
	ts := httptest.NewServer(s.routes())
	t.Cleanup(ts.Close)

	got429 := false
	for i := 0; i < 100 && !got429; i++ {
		if postJSON(t, ts.URL+"/faucet", map[string]string{"address": newWallet().addr}) == http.StatusTooManyRequests {
			got429 = true
		}
	}
	if !got429 {
		t.Error("write rate limiter never returned 429 under a burst")
	}
}

// Faulting distinct validators past the budget gets a 409, so consensus can
// always still reach two thirds. Few enough requests to stay under the
// write burst.
func TestFaultBudget(t *testing.T) {
	s := newServer(6)
	ts := httptest.NewServer(s.routes())
	t.Cleanup(ts.Close)

	got409 := false
	for i := 0; i < s.cluster.Size() && !got409; i++ {
		switch postJSON(t, ts.URL+"/fault", map[string]int{"index": i}) {
		case http.StatusConflict:
			got409 = true
		case http.StatusAccepted:
		default:
			t.Fatalf("fault %d: unexpected status", i)
		}
	}
	if !got409 {
		t.Error("fault budget never refused")
	}
}

// The faucet rejects its own address and malformed addresses before
// touching its nonce, so a bad request cannot wedge it: a good request
// right after still funds.
func TestFaucetRejectsBadAddressWithoutWedging(t *testing.T) {
	s := newServer(4)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go s.cluster.RunContext(ctx)

	ts := httptest.NewServer(s.routes())
	t.Cleanup(ts.Close)

	if code := postJSON(t, ts.URL+"/faucet", map[string]string{"address": s.faucet.addr}); code != http.StatusBadRequest {
		t.Errorf("faucet self-send: got %d, want 400", code)
	}
	if code := postJSON(t, ts.URL+"/faucet", map[string]string{"address": "not-an-address"}); code != http.StatusBadRequest {
		t.Errorf("faucet junk address: got %d, want 400", code)
	}

	good := newWallet().addr
	if code := postJSON(t, ts.URL+"/faucet", map[string]string{"address": good}); code != http.StatusOK {
		t.Fatalf("faucet good address after bad ones: got %d, want 200", code)
	}
	waitFor(t, 5*time.Second, func() bool {
		var a struct {
			Balance uint64 `json:"balance"`
		}
		getJSON(t, ts.URL+"/account?address="+good, &a)
		return a.Balance > 0
	}, "faucet did not fund after a rejected request")
}

// /proof returns a Merkle inclusion proof for a mined transaction that
// folds back to the block's transaction root, and 404s for an unknown one.
func TestProofEndpoint(t *testing.T) {
	s := newServer(4)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go s.cluster.RunContext(ctx)

	ts := httptest.NewServer(s.routes())
	t.Cleanup(ts.Close)

	tx, _ := s.signFrom(s.accounts["alice"], s.accounts["bob"].addr, chain.BlockReward)
	if code := postJSON(t, ts.URL+"/tx", tx); code != http.StatusAccepted {
		t.Fatalf("submit tx: got %d", code)
	}
	wantHash := hex.EncodeToString(tx.Hash())

	var found struct {
		Height int64 `json:"height"`
		Hash   string
	}
	waitFor(t, 5*time.Second, func() bool {
		var st struct {
			Txs []struct {
				Hash   string `json:"hash"`
				Height int64  `json:"height"`
			} `json:"txs"`
		}
		getJSON(t, ts.URL+"/state", &st)
		for _, x := range st.Txs {
			if x.Hash == wantHash {
				found.Height, found.Hash = x.Height, x.Hash
				return true
			}
		}
		return false
	}, "submitted transaction never landed in a block")

	var p struct {
		Leaf     string   `json:"leaf"`
		Index    int      `json:"index"`
		Siblings []string `json:"siblings"`
		Root     string   `json:"root"`
	}
	getJSON(t, ts.URL+"/proof?height="+strconv.FormatInt(found.Height, 10)+"&hash="+wantHash, &p)

	leaf, _ := hex.DecodeString(p.Leaf)
	root, _ := hex.DecodeString(p.Root)
	sibs := make([][]byte, len(p.Siblings))
	for i, s := range p.Siblings {
		sibs[i], _ = hex.DecodeString(s)
	}
	if p.Leaf != wantHash {
		t.Errorf("proof leaf %s, want the tx hash %s", p.Leaf, wantHash)
	}
	if !chain.VerifyInclusionProof(leaf, p.Index, sibs, root) {
		t.Error("proof does not verify against its own root")
	}

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/proof?height=1&hash="+hex.EncodeToString(make([]byte, 32)), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("proof for an unknown tx: got %d, want 404", resp.StatusCode)
	}
}

func TestGuardedDir(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("index.html", "<!doctype html>root")
	write("assets/app.js", "console.log(1)")
	write(".env", "SECRET=1")

	fsys := guardedDir{http.Dir(root)}

	if f, err := fsys.Open("/assets/app.js"); err != nil {
		t.Errorf("open asset: %v", err)
	} else {
		f.Close()
	}
	if _, err := fsys.Open("/assets"); err == nil {
		t.Error("opened a directory without an index.html; listing is exposed")
	}
	if _, err := fsys.Open("/.env"); err == nil {
		t.Error("served a dotfile")
	}
	if _, err := fsys.Open("/assets/../.env"); err == nil {
		t.Error("served a dotfile through a traversal path")
	}
}

func waitFor(t *testing.T, d time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal(msg)
}

func getJSON(t *testing.T, url string, v any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
}

func postJSON(t *testing.T, url string, body any) int {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func readFirstEvent(t *testing.T, url string) <-chan string {
	t.Helper()
	out := make(chan string, 1)
	go func() {
		resp, err := http.Get(url)
		if err != nil {
			return
		}
		defer resp.Body.Close()
		br := bufio.NewReader(resp.Body)
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				return
			}
			if data, ok := strings.CutPrefix(strings.TrimRight(line, "\n"), "data: "); ok {
				out <- data
				return
			}
		}
	}()
	return out
}

// A server built with -data writes a block log and, restarted against the
// same directory, resumes the chain from it rather than starting fresh.
func TestPersistResume(t *testing.T) {
	dir := t.TempDir()

	s1 := buildServer(4, dir)
	ctx1, cancel1 := context.WithCancel(context.Background())
	go s1.cluster.RunContext(ctx1)
	go s1.drive(ctx1, 30*time.Millisecond)

	ts1 := httptest.NewServer(s1.routes())
	waitFor(t, 5*time.Second, func() bool {
		var st struct {
			Height int64 `json:"height"`
		}
		getJSON(t, ts1.URL+"/state", &st)
		return st.Height >= 8
	}, "first run produced no blocks")

	var before struct {
		Height   int64 `json:"height"`
		Accounts []struct {
			Name    string `json:"name"`
			Balance uint64 `json:"balance"`
		} `json:"accounts"`
	}
	getJSON(t, ts1.URL+"/state", &before)
	cancel1()
	ts1.Close()
	// give the run loop a moment to unwind and flush the last block
	time.Sleep(100 * time.Millisecond)

	s2 := buildServer(4, dir)
	got := s2.cluster.Snapshot().Height
	if got < before.Height {
		t.Fatalf("resumed at height %d, first run reached %d", got, before.Height)
	}
	if s2.faucet.addr != s1.faucet.addr {
		t.Error("faucet address changed across restart")
	}

	ctx2, cancel2 := context.WithCancel(context.Background())
	t.Cleanup(cancel2)
	go s2.cluster.RunContext(ctx2)
	ts2 := httptest.NewServer(s2.routes())
	t.Cleanup(ts2.Close)
	waitFor(t, 5*time.Second, func() bool {
		var st struct {
			Height int64    `json:"height"`
			Halts  []string `json:"halts"`
		}
		getJSON(t, ts2.URL+"/state", &st)
		if len(st.Halts) != 0 {
			t.Fatalf("halts after resume: %v", st.Halts)
		}
		return st.Height > got
	}, "resumed chain did not advance")
}
