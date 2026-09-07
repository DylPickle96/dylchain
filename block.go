package dyl

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"
)

// Block is one entry in the chain. TxRoot commits to Transactions, Hash
// covers the header (everything but Signature). Alloc is set on the genesis
// block only; Proposer and Signature only on blocks produced by consensus.
type Block struct {
	Transactions []Transaction     `json:"Transactions"`
	TxRoot       []byte            `json:"TxRoot"`
	PreviousHash []byte            `json:"PreviousHash"`
	CreatedAt    int64             `json:"CreatedAt"`
	Height       int64             `json:"Height"`
	Alloc        map[string]uint64 `json:"Alloc,omitempty"`
	Proposer     string            `json:"Proposer,omitempty"`
	Signature    []byte            `json:"Signature,omitempty"`
}

// Hash is the SHA-256 digest of the block header's JSON serialisation
// (every field except Signature, which signs this hash). It is derived on
// demand rather than stored, so any change to the header changes the hash.
func (b Block) Hash() []byte {
	block := Block{
		TxRoot:       b.TxRoot,
		PreviousHash: b.PreviousHash,
		CreatedAt:    b.CreatedAt,
		Height:       b.Height,
		Alloc:        b.Alloc,
		Proposer:     b.Proposer,
	}
	hasher := sha256.New()
	data, err := json.Marshal(block)
	if err != nil {
		panic(err) // these field types cannot produce a marshal error
	}
	hasher.Write(data)
	return hasher.Sum(nil)
}

// sign returns priv's ed25519 signature over the block's header hash. The
// proposer calls this and stores the result in Signature.
func (b Block) sign(priv ed25519.PrivateKey) []byte {
	return ed25519.Sign(priv, b.Hash())
}

// genesisBlock builds the height-zero block from a genesis allocation. It
// carries no proposer, so applying it mints nothing, and its Alloc is what
// ReplayBlocks seeds state from.
func genesisBlock(alloc map[string]uint64) Block {
	return Block{
		Transactions: []Transaction{},
		TxRoot:       merkleRoot(nil),
		PreviousHash: []byte{},
		Alloc:        alloc,
		CreatedAt:    time.Now().Unix(),
		Height:       0,
	}
}

// verifyBlockSignature checks that Signature was produced by the key behind
// the Proposer address, over the block's header hash. The caller is
// responsible for deciding whether a proposer is expected at all.
func verifyBlockSignature(b Block) error {
	pub, err := pubKeyFromAddress(b.Proposer)
	if err != nil {
		return fmt.Errorf("cannot get public key for proposer: %s. %w", b.Proposer, err)
	}
	if !ed25519.Verify(pub, b.Hash(), b.Signature) {
		return fmt.Errorf("proposer: %s, cannot verify block signature", b.Proposer)
	}
	return nil
}
