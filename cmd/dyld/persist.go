package main

import (
	"bufio"
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"

	"dyl/chain"
)

// storedKeys is the on-disk key material for a persistent deployment: the
// ed25519 seed for the faucet, each demo account, and every validator, so a
// restarted process keeps the same identities as the block log it replays.
type storedKeys struct {
	Faucet     string            `json:"faucet"`
	Accounts   map[string]string `json:"accounts"`
	Validators []string          `json:"validators"`
}

// loadOrCreateKeys reads <dir>/keys.json, or generates fresh seeds for the
// faucet, every demo account and n validators and writes them. An existing
// file's validator count wins over n.
func loadOrCreateKeys(dir string, n int) storedKeys {
	path := filepath.Join(dir, "keys.json")
	if data, err := os.ReadFile(path); err == nil {
		var k storedKeys
		if err := json.Unmarshal(data, &k); err != nil {
			log.Fatalf("keys file %s: %v", path, err)
		}
		if len(k.Validators) == 0 {
			log.Fatalf("keys file %s has no validators", path)
		}
		return k
	}

	k := storedKeys{Faucet: newSeedHex(), Accounts: map[string]string{}, Validators: make([]string, n)}
	for name := range demoAccounts {
		k.Accounts[name] = newSeedHex()
	}
	for i := range k.Validators {
		k.Validators[i] = newSeedHex()
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Fatalf("data dir %s: %v", dir, err)
	}
	data, _ := json.MarshalIndent(k, "", "  ")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		log.Fatalf("write %s: %v", path, err)
	}
	log.Printf("generated %s (%d validators)", path, n)
	return k
}

func newSeedHex() string {
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		log.Fatal(err)
	}
	return hex.EncodeToString(priv.Seed())
}

// walletFromSeedHex rebuilds a server wallet from a stored seed.
func walletFromSeedHex(h string) wallet {
	seed, err := hex.DecodeString(h)
	if err != nil || len(seed) != ed25519.SeedSize {
		log.Fatalf("bad key seed %q", h)
	}
	priv := ed25519.NewKeyFromSeed(seed)
	return wallet{priv: priv, addr: chain.AddressFromKey(priv.Public().(ed25519.PublicKey))}
}

func seedBytes(hexes []string) [][]byte {
	out := make([][]byte, len(hexes))
	for i, h := range hexes {
		b, err := hex.DecodeString(h)
		if err != nil || len(b) != ed25519.SeedSize {
			log.Fatalf("bad validator seed %q", h)
		}
		out[i] = b
	}
	return out
}

// blockLog is an append-only file of committed blocks, one JSON object per
// line. It is the whole persistence story: on restart the server replays it
// to rebuild the chain, and appends every new block as it commits.
type blockLog struct {
	f *os.File
	w *bufio.Writer
}

func openBlockLog(dir string) *blockLog {
	f, err := os.OpenFile(filepath.Join(dir, "blocks.jsonl"), os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o644)
	if err != nil {
		log.Fatalf("open block log: %v", err)
	}
	return &blockLog{f: f, w: bufio.NewWriter(f)}
}

// read returns every block in the log, in order. A parse failure is fatal:
// a corrupt log must not be silently resumed from.
func (l *blockLog) read() []chain.Block {
	if _, err := l.f.Seek(0, io.SeekStart); err != nil {
		log.Fatal(err)
	}
	var blocks []chain.Block
	sc := bufio.NewScanner(l.f)
	sc.Buffer(make([]byte, 0, 1<<20), 32<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var b chain.Block
		if err := json.Unmarshal(line, &b); err != nil {
			log.Fatalf("block log corrupt near block %d: %v", len(blocks), err)
		}
		blocks = append(blocks, b)
	}
	if err := sc.Err(); err != nil {
		log.Fatalf("read block log: %v", err)
	}
	if _, err := l.f.Seek(0, io.SeekEnd); err != nil {
		log.Fatal(err)
	}
	return blocks
}

// append writes one block and flushes to the OS. It runs on the committing
// node's goroutine, so it stays off fsync; a process crash keeps the line,
// only a machine crash could lose the last few.
func (l *blockLog) append(b chain.Block) {
	line, err := json.Marshal(b)
	if err != nil {
		log.Printf("marshal block %d: %v", b.Height, err)
		return
	}
	l.w.Write(line)
	l.w.WriteByte('\n')
	if err := l.w.Flush(); err != nil {
		log.Printf("append block %d to log: %v", b.Height, err)
	}
}
