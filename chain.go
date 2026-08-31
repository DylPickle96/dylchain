package blockchain

import (
	"bytes"
	"fmt"
	"time"
)

type Chain struct {
	Blocks []Block
}

// NewChain returns a chain containing only its genesis block, so a Chain
// never exists without one.
func NewChain() Chain {
	genesisBlock := Block{
		Transactions: []Transaction{},
		PreviousHash: []byte{},
		CreatedAt:    time.Now().Unix(),
		Height:       0,
	}
	return Chain{Blocks: []Block{genesisBlock}}
}

// AddBlock appends a block carrying tx, linking it to the current tip and
// incrementing the height. The caller supplies only the payload; the
// fields that must stay consistent with the rest of the chain are derived
// here.
func (c *Chain) AddBlock(tx []Transaction) {
	if len(c.Blocks) == 0 {
		panic("no genesis block, use NewChain()")
	}
	previousBlock := c.Blocks[len(c.Blocks)-1]
	newBlock := Block{
		Transactions: tx,
		PreviousHash: previousBlock.Hash(),
		CreatedAt:    time.Now().Unix(),
		Height:       previousBlock.Height + 1,
	}
	c.Blocks = append(c.Blocks, newBlock)
}

// Validate checks, for every adjacent pair of blocks, that the stored
// previous-hash matches a recompute of the earlier block and that the
// height increases by exactly one.
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
