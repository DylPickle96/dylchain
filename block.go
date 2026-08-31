package blockchain

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"strings"
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
	From      string `json:"From"`
	To        string `json:"To"`
	Amount    uint64 `json:"Amount"`
	Nonce     int64  `json:"Nonce"`
	Signature []byte
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
	maps.Copy(s.Balances, alloc)
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
	maps.Copy(next.Balances, state.Balances)
	maps.Copy(next.Nonces, state.Nonces)
	for i, tx := range block.Transactions {
		// 1. Structural checks: is this even a well-formed transfer?
		if tx.From == "" {
			return State{}, fmt.Errorf("tx %d: empty sender", i)
		}
		if tx.From == tx.To {
			return State{}, fmt.Errorf("tx %d: self-send from %s", i, tx.From)
		}
		if tx.Amount == 0 {
			return State{}, fmt.Errorf("tx %d: zero amount", i)
		}

		// 2. Authenticity: did the holder of From's private key actually
		// authorise these exact fields? Verify this before consulting any
		// account state, so an unauthenticated transaction never influences
		// which error we report or which balances we read.
		pubKey, err := pubKeyFromAddress(tx.From)
		if err != nil {
			return State{}, fmt.Errorf("tx %d: cannot derive public key for %s: %w", i, tx.From, err)
		}
		if !ed25519.Verify(pubKey, tx.signableBytes(), tx.Signature) {
			return State{}, fmt.Errorf("tx %d: invalid signature for %s", i, tx.From)
		}

		// 3. Account rules: does the authenticated sender have the right
		// nonce and enough balance?
		if tx.Nonce != next.Nonces[tx.From] {
			return State{}, fmt.Errorf("tx %d: nonce is %d, expected %d for %s", i, tx.Nonce, next.Nonces[tx.From], tx.From)
		}
		if next.Balances[tx.From] < tx.Amount {
			return State{}, fmt.Errorf("tx %d: insufficient balance for %s: have %d, need %d", i, tx.From, next.Balances[tx.From], tx.Amount)
		}

		// 4. Apply.
		next.Balances[tx.From] -= tx.Amount
		next.Balances[tx.To] += tx.Amount
		next.Nonces[tx.From]++
	}
	return next, nil
}

func deriveAddress(publicKey ed25519.PublicKey) string {
	return "dyl" + hex.EncodeToString(publicKey)
}

func pubKeyFromAddress(address string) (ed25519.PublicKey, error) {
	address, found := strings.CutPrefix(address, "dyl")
	if !found {
		return nil, fmt.Errorf("address does not contain prefix dyl")
	}
	b, err := hex.DecodeString(address)
	if err != nil {
		return nil, fmt.Errorf("cannot decode address: %w", err)
	}
	if len(b) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("address decodes to %d bytes, want %d", len(b), ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(b), nil
}

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
