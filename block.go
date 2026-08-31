package blockchain

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"
)

type Block struct {
	Transaction  []byte `json:"Transaction"`
	PreviousHash []byte `json:"PreviousHash"`
	CreatedAt    int64  `json:"CreatedAt"`
	Height       int64  `json:"Height"`
}

func (b Block) Hash() []byte {
	hasher := sha256.New()
	data, err := json.Marshal(b)
	if err != nil {
		panic(err)
	}
	hasher.Write(data)
	return hasher.Sum(nil)
}

type Chain struct {
	Blocks []Block
}

func NewChain() Chain {
	genesisBlock := Block{
		Transaction:  []byte{},
		PreviousHash: []byte{},
		CreatedAt:    time.Now().Unix(),
		Height:       0,
	}
	return Chain{Blocks: []Block{genesisBlock}}
}

func (c *Chain) AddBlock(tx []byte) {
	if len(c.Blocks) == 0 {
		panic("No Genesis block, use NewChain()")
	}
	previousBlock := c.Blocks[len(c.Blocks)-1]
	newBlock := Block{
		Transaction:  tx,
		PreviousHash: previousBlock.Hash(),
		CreatedAt:    time.Now().Unix(),
		Height:       previousBlock.Height + 1,
	}
	c.Blocks = append(c.Blocks, newBlock)
}

func (c *Chain) Validate() error {
	for i := 0; i < len(c.Blocks)-1; i++ {
		if !bytes.Equal(c.Blocks[i].Hash(), c.Blocks[i+1].PreviousHash) {
			return fmt.Errorf("block %d: PreviousHash does not match hash of block %d", i+1, i)
		}
		if c.Blocks[i].Height+1 != c.Blocks[i+1].Height {
			return fmt.Errorf("block %d: height is %d, expected %d", i+1, c.Blocks[i+1].Height, c.Blocks[i].Height+1)
		}
	}
	return nil
}
