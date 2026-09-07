package chain

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

// Transaction is a signed transfer of Amount base units from From to To.
// Nonce is the sender's next expected sequence number, starting at zero.
// Signature covers From, To, Amount, and Nonce, never itself.
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
	next := nextLayer(layer)
	return walkTree(next)
}

// nextLayer hashes each adjacent pair of a layer into one node of the layer
// above, prefixing every hash with merkleNodePrefix. An odd layer first
// duplicates its last element so every node has a partner.
func nextLayer(layer [][]byte) [][]byte {
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
	return next
}

// merkleProof returns the sibling hashes on the path from the transaction
// at index up to the root: the hashes a light client needs, alongside the
// leaf and the root, to prove that transaction is in the block. When index
// is the last node of an odd layer, its own hash is recorded as the
// sibling, so verifyMerkleProof needs only index parity, not the leaf
// count.
func merkleProof(txs []Transaction, index int) ([][]byte, error) {
	if len(txs) == 0 {
		return nil, fmt.Errorf("transactions length is zero")
	}
	if index < 0 || index >= len(txs) {
		return nil, fmt.Errorf("bad index for merkleProof")
	}
	layer := make([][]byte, 0)
	for _, tx := range txs {
		data, err := json.Marshal(tx)
		if err != nil {
			panic(err) // these field types cannot produce a marshal error
		}
		h := sha256.New()
		h.Write([]byte{merkleLeafPrefix})
		h.Write(data)
		layer = append(layer, h.Sum(nil))
	}
	proofs := make([][]byte, 0)
	for len(layer) > 1 {
		if len(layer)%2 != 0 && index == len(layer)-1 {
			proofs = append(proofs, layer[index])
		} else if index%2 == 0 {
			proofs = append(proofs, layer[index+1])
		} else {
			proofs = append(proofs, layer[index-1])
		}
		layer = nextLayer(layer)
		index = index / 2
	}
	return proofs, nil
}

// verifyMerkleProof folds leafHash up through the sibling path and reports
// whether the result equals root. At each step, index parity decides
// whether current is the left or right input, and index is halved. An
// empty proof means a single-transaction block, where leafHash must equal
// root.
func verifyMerkleProof(leafHash []byte, index int, proof [][]byte, root []byte) bool {
	current := leafHash
	for _, sibling := range proof {
		h := sha256.New()
		h.Write([]byte{merkleNodePrefix})
		if index%2 == 0 {
			h.Write(current)
			h.Write(sibling)
		} else {
			h.Write(sibling)
			h.Write(current)
		}
		current = h.Sum(nil)
		index /= 2
	}
	return bytes.Equal(current, root)
}
