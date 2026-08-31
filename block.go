package blockchain

import (
	"crypto/sha256"
	"encoding/json"
)

type Block struct {
	Transactions []Transaction `json:"Transactions"`
	PreviousHash []byte        `json:"PreviousHash"`
	CreatedAt    int64         `json:"CreatedAt"`
	Height       int64         `json:"Height"`
}

// Hash is the SHA-256 digest of the block's JSON serialisation. It is
// derived on demand rather than stored, so any change to a block's
// contents changes its hash.
func (b Block) Hash() []byte {
	hasher := sha256.New()
	data, err := json.Marshal(b)
	if err != nil {
		panic(err) // these field types cannot produce a marshal error
	}
	hasher.Write(data)
	return hasher.Sum(nil)
}
