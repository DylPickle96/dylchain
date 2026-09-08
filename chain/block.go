package chain

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// Block is one entry in the chain. TxRoot commits to Transactions, Hash
// covers the header (everything but Signature). Alloc and Validators are
// set on the genesis block only; Proposer and Signature only on blocks
// produced by consensus.
type Block struct {
	Transactions []Transaction     `json:"Transactions"`
	TxRoot       []byte            `json:"TxRoot"`
	PreviousHash []byte            `json:"PreviousHash"`
	CreatedAt    int64             `json:"CreatedAt"`
	Height       int64             `json:"Height"`
	Alloc        map[string]uint64 `json:"Alloc,omitempty"`
	Validators   map[string]uint64 `json:"Validators,omitempty"` // validator address -> genesis self-stake
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
		Validators:   b.Validators,
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

// genesisBlock builds the height-zero block. It carries no proposer, so
// applying it mints nothing. Alloc seeds balances and valBase (validator
// address -> genesis self-stake) seeds the reward split; both come out of
// ReplayBlocks. valBase may be nil for a chain with no validators.
func genesisBlock(alloc, valBase map[string]uint64) Block {
	return Block{
		Transactions: []Transaction{},
		TxRoot:       merkleRoot(nil),
		PreviousHash: []byte{},
		Alloc:        alloc,
		Validators:   valBase,
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

// InclusionProof returns a Merkle proof that the transaction whose hex hash
// is txHash sits in b: the leaf hash, its index, the sibling hashes from
// leaf up to the root, and b.TxRoot. found is false if b holds no such
// transaction. VerifyInclusionProof, or an equivalent light client, folds
// the leaf back up to the root with it.
func (b Block) InclusionProof(txHash string) (leaf []byte, index int, siblings [][]byte, root []byte, found bool) {
	want, err := hex.DecodeString(txHash)
	if err != nil || len(want) != sha256.Size {
		return nil, 0, nil, nil, false
	}
	for i, tx := range b.Transactions {
		if bytes.Equal(tx.Hash(), want) {
			sibs, err := merkleProof(b.Transactions, i)
			if err != nil {
				return nil, 0, nil, nil, false
			}
			return tx.Hash(), i, sibs, b.TxRoot, true
		}
	}
	return nil, 0, nil, nil, false
}

// VerifyInclusionProof reports whether folding leaf up through siblings by
// index parity reproduces root. It is the check a light client runs on the
// output of InclusionProof.
func VerifyInclusionProof(leaf []byte, index int, siblings [][]byte, root []byte) bool {
	return verifyMerkleProof(leaf, index, siblings, root)
}
