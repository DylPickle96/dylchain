package blockchain

import (
	"crypto/ed25519"
	"encoding/json"
)

type Transaction struct {
	From      string `json:"From"`
	To        string `json:"To"`
	Amount    uint64 `json:"Amount"`
	Nonce     int64  `json:"Nonce"`
	Signature []byte
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
