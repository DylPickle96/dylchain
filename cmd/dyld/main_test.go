package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	good := s.sign(s.accounts["alice"], s.accounts["bob"].addr, chain.BlockReward)
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
