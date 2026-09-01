# blockchain

A toy blockchain built from scratch in Go, as a learning exercise. It is
built in stages, each one adding a single concept and its tests before the
next begins. The goal is understanding the mechanics (hashing, account
state, signatures, Merkle commitments, and eventually BFT consensus), not
production use.

## Status

| Stage | Concept | State |
|-------|---------|-------|
| 1 | Blocks, hashing, a validated chain | done |
| 2 | Structured transactions | done |
| 3 | Account state and block application | done |
| 4 | ed25519 transaction signatures | done |
| 5 | Merkle root over a block's transactions | done |
| 6 | Multiple validators (BFT consensus) | not started |
| 7 | Slashing and minting | not started |

## Layout

The package is one Go package split by concern:

| File | Contents |
|------|----------|
| `block.go` | `Block`, `Block.Hash` |
| `chain.go` | `Chain`, `NewChain`, `AddBlock`, `Validate` |
| `transaction.go` | `Transaction`, `Sign`, `merkleRoot` |
| `state.go` | `State`, `NewState`, `Apply` |
| `address.go` | address derivation to and from ed25519 keys |
| `*_test.go` | tests, with shared fixtures in `testutil_test.go` |

## Model

**Account based, not UTXO.** State is a balance and a nonce per address:

```go
type State struct {
	Balances map[string]uint64
	Nonces   map[string]int64
}
```

**Amounts are unsigned integers in the smallest unit.** No floating point
anywhere in the value path, so balances stay exact. Fractional display
would be a presentation concern handled at the edge.

**Addresses are derived from ed25519 public keys.** An address is
`"dyl" + hex(publicKey)`. The prefix is the toy equivalent of a chain's
bech32 prefix (`cosmos1...`, `core1...`). It is reversible, so `Apply` can
recover the public key from `tx.From` to verify a signature.

**Transactions are signed.** A `Transaction` carries a `Signature` over
its authorised fields (`From`, `To`, `Amount`, `Nonce`), never over the
signature itself. The private key never enters any on-chain type. It is
passed to `Sign` on the sender's side and nowhere else.

**Blocks commit to their transactions with a Merkle root.** `Block.TxRoot`
is the root of a binary hash tree over the transactions, using RFC 6962
style domain separation (`0x00` prefix on leaf hashes, `0x01` on internal
nodes) and duplicate-last padding for odd layers. `Block.Hash` covers the
block header, including `TxRoot`, not the raw transaction slice. An empty
block has an all-zero root.

## How a block is added

`AddBlock` is the single entry point. The caller supplies only the
transactions. Everything that has to stay consistent with the rest of the
chain is derived:

1. `PreviousHash` is set to the hash of the current tip.
2. `Height` is the tip's height plus one.
3. `TxRoot` is computed from the transactions.
4. `Apply` runs the transactions against the chain's current state.

`Apply` is all-or-nothing and works on a copy, so the chain's state is
never left half-updated. The first invalid transaction (bad signature,
wrong nonce, insufficient balance, malformed) rejects the whole block, and
`AddBlock` returns an error with nothing appended. Within a valid block,
transactions apply in order, so a later one sees the effect of an earlier
one.

## Validation

`Chain.Validate` checks two kinds of invariant:

- **Per block:** `TxRoot` equals a fresh recompute of the Merkle root, so
  the transaction body cannot be swapped under a valid header.
- **Per adjacent pair:** the stored `PreviousHash` matches a recompute of
  the earlier block, and the height increases by exactly one.

### Known gaps

- The block hash is derived, not stored. Tampering with a block is caught
  by the next block's link check, so the final block's header is only
  partially protected. Changing a tip transaction is caught by the
  `TxRoot` check, but a fully consistent tip rewrite (transaction changed
  and `TxRoot` recomputed) is not. This closes once consensus signs
  blocks.
- The genesis allocation is passed to `NewChain` and loaded into state
  directly. It is not recorded in the genesis block, so the chain cannot
  be replayed from the block list alone. This will matter at stage 6, when
  nodes exchange chains.

## Running the tests

```
go test ./...
```

No dependencies beyond the standard library. Go 1.26 or newer.
