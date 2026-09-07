package chain

import (
	"crypto/ed25519"
	"testing"
	"time"
)

// block wraps transactions into a Block for tests that exercise Apply
// directly, without going through a Chain.
func block(txs ...Transaction) Block {
	return Block{Transactions: txs}
}

// txs / tx build unsigned transactions for chain-level tests that never
// reach signature verification.
func txs(t ...Transaction) []Transaction { return t }

func tx(from, to string, amount uint64, nonce int64) Transaction {
	return Transaction{From: from, To: to, Amount: amount, Nonce: nonce}
}

// wallet is a test fixture: a key pair plus its derived address. The
// private key never leaves the test, exactly as in a real system.
type wallet struct {
	pub  ed25519.PublicKey
	priv ed25519.PrivateKey
	addr string
}

func newWallet(t *testing.T) wallet {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return wallet{pub: pub, priv: priv, addr: deriveAddress(pub)}
}

// send builds a transaction from this wallet and signs it.
func (w wallet) send(t *testing.T, to string, amount uint64, nonce int64) Transaction {
	t.Helper()
	txn := Transaction{From: w.addr, To: to, Amount: amount, Nonce: nonce}
	txn.Signature = txn.Sign(w.priv)
	return txn
}

// mustAdd appends a block and fails the test if it is rejected.
func mustAdd(t *testing.T, c *Chain, txns ...Transaction) {
	t.Helper()
	if err := c.AddBlock(txns); err != nil {
		t.Fatalf("AddBlock at height %d: %v", len(c.Blocks), err)
	}
}

// proposeBlock builds a well-formed candidate block for c's next height,
// carrying txns, proposed and signed by w. Tests mutate the result to
// exercise the checks in checkCandidate and Validate.
func (w wallet) proposeBlock(t *testing.T, c *Chain, txns ...Transaction) Block {
	t.Helper()
	tip := c.Blocks[len(c.Blocks)-1]
	b := Block{
		Transactions: txns,
		TxRoot:       merkleRoot(txns),
		PreviousHash: tip.Hash(),
		CreatedAt:    time.Now().Unix(),
		Height:       tip.Height + 1,
		Proposer:     w.addr,
	}
	b.Signature = b.sign(w.priv)
	return b
}
