# dyl

A toy blockchain built from scratch in Go, as a learning exercise. It is
built in stages, each one adding a single concept and its tests before the
next begins. The goal is understanding the mechanics (hashing, account
state, signatures, Merkle commitments, and eventually BFT consensus), not
production use.

The native coin is **DYL**. Balances and amounts are counts of its base
unit, `udyl`, where 1 DYL is 10^6 `udyl`.

## Status

| Stage | Concept | State |
|-------|---------|-------|
| 1 | Blocks, hashing, a validated chain | done |
| 2 | Structured transactions | done |
| 3 | Account state and block application | done |
| 4 | ed25519 transaction signatures | done |
| 5 | Merkle root and inclusion proofs | done |
| 6 | Multiple validators (BFT consensus) | done, happy path |
| 7a | Minting: block reward to the proposer | not started |
| 7b | Equivocation detection | not started |
| 7c | Slashing | not started |
| 7.5 | Scaling pass (hundreds of validators) | not started |
| 8 | Demo backend (HTTP + SSE, fault injection) | not started |
| 9 | Explorer UI | not started |

## Layout

The package is one Go package split by concern:

| File | Contents |
|------|----------|
| `block.go` | `Block`, `Block.Hash`, `genesisBlock`, proposer signatures |
| `chain.go` | `Chain`, `NewChain`, `AddBlock`, `CommitBlock`, `Validate`, `ReplayBlocks` |
| `transaction.go` | `Transaction`, `Sign`, Merkle root and proofs |
| `state.go` | `State`, `NewState`, `Apply` |
| `address.go` | address derivation to and from ed25519 keys |
| `coin.go` | native coin denom, precision, amount formatting |
| `validator.go` | `Validator`, `ValidatorSet`, stake-weighted proposer selection |
| `mempool.go` | `Mempool`, the shared pending-transaction queue |
| `consensus.go` | votes, the in-process bus, the per-validator round loop |
| `cluster.go` | `Cluster`, the entry point for a consensus run |
| `*_test.go` | tests, with shared fixtures in `testutil_test.go` |

## Model

**Account based, not UTXO.** State is a balance and a nonce per address:

```go
type State struct {
	Balances map[string]uint64
	Nonces   map[string]int64
}
```

**Amounts are unsigned integers in the smallest unit** (`udyl`). No
floating point anywhere in the value path, so balances stay exact.
`FormatAmount` in `coin.go` renders a base-unit count as a `DYL` string
for display, which is the only place the decimal point appears.

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

A full node already has every transaction, so `AddBlock` and `Validate`
only recompute the root. `merkleProof` / `verifyMerkleProof` let a light
client check that one transaction is in a block from the leaf hash, a
sibling path, and `TxRoot`, without the rest of the body. They are
unexported until something outside this package needs them. Odd layers
put a copy of the last node into the proof as its sibling, so verify
orders hashes by index parity and does not need the leaf count.

## How a block is added

There are two paths onto the chain.

`AddBlock` is the single-writer path, for a chain with one author and no
consensus. The caller supplies only the transactions, and everything that
has to stay consistent with the rest of the chain is derived:

1. `PreviousHash` is set to the hash of the current tip.
2. `Height` is the tip's height plus one.
3. `TxRoot` is computed from the transactions.
4. `Apply` runs the transactions against the chain's current state.

`CommitBlock` is the consensus path. It does not build a block: the
proposer already did, and the caller has gathered the votes. It re-runs
every check through `checkCandidate` (link, height, `TxRoot`, proposer
signature, `Apply`) and advances state. A node that only ever receives
committed blocks stays correct through it.

`Apply` is all-or-nothing and works on a copy, so the chain's state is
never left half-updated. The first invalid transaction (bad signature,
wrong nonce, insufficient balance, malformed) rejects the whole block, and
nothing is appended. Within a valid block, transactions apply in order, so
a later one sees the effect of an earlier one.

The genesis allocation lives in the genesis block's `Alloc` field, not just
in state, so `ReplayBlocks` can rebuild the ledger from a block list alone.
`NewChain` derives its own starting state through that same replay path.

## Consensus

`Cluster` runs several `Validator` goroutines in one process, joined by an
in-memory broadcast bus, doing one-step-vote BFT. Each validator keeps its
own `Chain`, seeded from one shared genesis block.

There is no coordinator. Every validator runs the same loop independently,
one round per height, and agreement falls out of all of them running the
same code over the same blocks and votes. Only the proposal and the votes
cross between nodes.

1. Compute the proposer for this height with `ProposerForHeight`
   (stake-weighted priority, CometBFT style; equal stakes give
   round-robin). Every node computes the same answer. If it is not you,
   skip to 3.
2. Proposer only: drain the shared `Mempool`, build a block, sign its
   header, broadcast it.
3. Receive the proposal, check it against your own chain, broadcast one
   signed vote for it.
4. Tally incoming votes by stake. Once votes covering **more than two
   thirds of total stake** are in (`3*accepted > 2*total`), commit. That
   commit is final: no fork choice, no reversion.

Votes and proposals that arrive out of order (a vote before its proposal, a
message for a later height) are stashed per node and rescanned, so timing
between goroutines does not wedge a round.

This is the happy path only: one process, no message loss, every validator
honest and online. `ProposerForHeight` recomputes the priority accumulator
from height 1 on every call, so it stays a pure function of the height and
the set, at `O(height * n)` per call. That, and the other `O(n^2)` costs in
the vote path, are what the stage 7.5 scaling pass addresses.

## Validation

`Chain.Validate` checks:

- **Per block:** `TxRoot` equals a fresh recompute of the Merkle root, and
  a consensus block's proposer signature verifies over its header.
- **Per adjacent pair:** the stored `PreviousHash` matches a recompute of
  the earlier block, and the height increases by exactly one.
- **Whole chain:** `ReplayBlocks` rebuilds the ledger from genesis and it
  matches the chain's own state, so a transaction cannot be altered after
  the fact without breaking its signature on replay, and in-memory state
  cannot drift from the blocks unnoticed.

### Known gaps

- The block hash is still derived, not stored. On a consensus chain the
  proposer signature covers the whole header, so a consistent rewrite of
  any block is caught. On a single-writer `AddBlock` chain there is no
  signature, so a rewrite of a header-only field such as `CreatedAt` on the
  tip, which nothing links to, still slips past. A stored, signed hash is
  the real fix.
- Consensus runs in one process over channels. There is no real network,
  no message loss, and no Byzantine behaviour, so one-step voting is not
  actually stress-tested for safety. A partition could commit two blocks at
  one height; two vote steps (prevote plus precommit) are what prevent
  that.
- No proposer timeout. If a height's proposer never proposes, every node
  blocks waiting for it. That, round changes, and double-sign detection are
  stage 6f.

## Running the tests

```
go test ./...
```

No dependencies beyond the standard library. Go 1.26 or newer.
