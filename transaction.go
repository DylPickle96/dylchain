package dyl

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
)

type Transaction struct {
	From      string `json:"From"`
	To        string `json:"To"`
	Amount    uint64 `json:"Amount"`
	Nonce     int64  `json:"Nonce"`
	Signature []byte `json:"Signature"`
}

// Sign returns an ed25519 signature over the transaction's authorised
// fields, produced with the sender's private key.
func (t Transaction) Sign(privateKey ed25519.PrivateKey) []byte {
	return ed25519.Sign(privateKey, t.signableBytes())
}

// signableBytes is the deterministic serialisation the signature covers:
// the authorised fields only, never the Signature itself.
func (t Transaction) signableBytes() []byte {
	toSign := Transaction{
		From:   t.From,
		To:     t.To,
		Amount: t.Amount,
		Nonce:  t.Nonce,
	}
	data, err := json.Marshal(toSign)
	if err != nil {
		panic(err) // these field types cannot produce a marshal error
	}
	return data
}

// Merkle domain-separation prefixes: leaf hashes and internal-node hashes
// are computed over inputs that start with different bytes, so an internal
// node can never be reinterpreted as a leaf (the RFC 6962 construction).
const (
	merkleLeafPrefix byte = 0x00
	merkleNodePrefix byte = 0x01
)

// merkleRoot is the Merkle root committing to a block's transactions. An
// empty block has an all-zero root.
func merkleRoot(txs []Transaction) []byte {
	if len(txs) == 0 {
		return make([]byte, sha256.Size)
	}

	layer := make([][]byte, len(txs))
	for i, tx := range txs {
		data, err := json.Marshal(tx)
		if err != nil {
			panic(err) // these field types cannot produce a marshal error
		}
		h := sha256.New()
		h.Write([]byte{merkleLeafPrefix})
		h.Write(data)
		layer[i] = h.Sum(nil)
	}
	return walkTree(layer)[0]
}

// walkTree collapses one layer of hashes into the next by hashing adjacent
// pairs, recursing until a single hash remains. An odd layer duplicates
// its last element so every node has a partner.
func walkTree(layer [][]byte) [][]byte {
	if len(layer) <= 1 {
		return layer
	}
	if len(layer)%2 != 0 {
		layer = append(layer, layer[len(layer)-1])
	}

	next := make([][]byte, 0, len(layer)/2)
	for i := 0; i < len(layer); i += 2 {
		h := sha256.New()
		h.Write([]byte{merkleNodePrefix})
		h.Write(layer[i])
		h.Write(layer[i+1])
		next = append(next, h.Sum(nil))
	}
	return walkTree(next)
}
