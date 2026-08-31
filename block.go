package blockchain

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"
)

type Block struct {
	Transactions []Transaction `json:"Transaction"`
	PreviousHash []byte        `json:"PreviousHash"`
	CreatedAt    int64         `json:"CreatedAt"`
	Height       int64         `json:"Height"`
}

type Chain struct {
	Blocks []Block
}

type State struct {
	Balances map[string]uint64
	Nonces   map[string]int64
}
type Transaction struct {
	From   string
	To     string
	Amount uint64
	Nonce  int64
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

func NewChain() Chain {
	genesisBlock := Block{
		Transactions: []Transaction{},
		PreviousHash: []byte{},
		CreatedAt:    time.Now().Unix(),
		Height:       0,
	}
	return Chain{Blocks: []Block{genesisBlock}}
}

func NewState(alloc map[string]uint64) State {
	s := State{
		Balances: make(map[string]uint64, len(alloc)),
		Nonces:   make(map[string]int64),
	}
	for addr, bal := range alloc {
		s.Balances[addr] = bal
	}
	return s
}

func (c *Chain) AddBlock(tx []Transaction) {
	if len(c.Blocks) == 0 {
		panic("No Genesis block, use NewChain()")
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

func Apply(state State, block Block) (State, error) {
	next := State{
		Balances: make(map[string]uint64, len(state.Balances)),
		Nonces:   make(map[string]int64, len(state.Nonces)),
	}
	for addr, bal := range state.Balances {
		next.Balances[addr] = bal
	}
	for addr, n := range state.Nonces {
		next.Nonces[addr] = n
	}
	for i, tx := range block.Transactions {
		if tx.From == "" {
			return State{}, fmt.Errorf("tx %d: empty sender", i)
		}
		if tx.From == tx.To {
			return State{}, fmt.Errorf("tx %d: self-send from %s", i, tx.From)
		}
		if tx.Amount == 0 {
			return State{}, fmt.Errorf("tx %d: zero amount", i)
		}
		if tx.Nonce != next.Nonces[tx.From] {
			return State{}, fmt.Errorf("tx %d: nonce is %d, expected %d for %s", i, tx.Nonce, next.Nonces[tx.From], tx.From)
		}
		if next.Balances[tx.From] < tx.Amount {
			return State{}, fmt.Errorf("tx %d: insufficient balance for %s: have %d, need %d", i, tx.From, next.Balances[tx.From], tx.Amount)
		}
		next.Balances[tx.From] -= tx.Amount
		next.Balances[tx.To] += tx.Amount
		next.Nonces[tx.From]++
	}
	return next, nil
}
