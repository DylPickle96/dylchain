# dyl

A toy blockchain built from scratch in Go, as a learning exercise. It is
built in stages, each one adding a single concept and its tests before the
next begins. The goal is understanding the mechanics (hashing, account
state, signatures, Merkle commitments, stake-weighted BFT consensus), not
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
| 7 | Minting, equivocation detection, slashing | done |
| 7.5 | Scaling to a few hundred validators | done |
| 8 | Demo server (HTTP + SSE) | done |
| 9 | Explorer UI and browser burner wallet | done |

## Layout

```
chain/     the chain library, package chain, imported as dyl/chain
cmd/dyld/  the demo server: runs a live cluster, serves HTTP + SSE
web/       the explorer UI and burner wallet, React + Vite
```

`chain/` is one Go package split by concern:

| File | Contents |
|------|----------|
| `block.go` | `Block`, `Block.Hash`, `genesisBlock`, proposer signatures |
| `chain.go` | `Chain`, `NewChain`, `AddBlock`, `CommitBlock`, `Validate`, `ReplayBlocks` |
| `transaction.go` | `Transaction`, `Sign`, Merkle root and proofs |
| `state.go` | `State`, `NewState`, `Apply` |
| `address.go` | address derivation to and from ed25519 keys |
| `coin.go` | native coin denom, precision, amount formatting, block reward |
| `validator.go` | `Validator`, `ValidatorSet`, `ProposerForHeight` reference, `Slash` |
| `proposer.go` | `election`, a node's incremental copy of the priority accumulator |
| `mempool.go` | `Mempool`, the shared pending-transaction queue |
| `consensus.go` | votes, the in-process bus, the per-validator round loop, equivocation detection |
| `evidence.go` | `Evidence` for a double-vote, `verifyEvidence` |
| `cluster.go` | `Cluster`, the entry point for a consensus run |
| `generate.go` | `GenerateValidators`, named validators on a skewed stake curve for demos and load tests |
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

Applying a committed block mints `BlockReward` (100 DYL) to its proposer,
so total supply grows by one reward per height. The figure is deliberately
generous, not a claim about sensible issuance: the coin has no value and
the demo reads better when supply moves. It happens inside `Apply`, keyed off
the block's `Proposer`, so `ReplayBlocks` and `Validate` reproduce it.
`State.Supply()` is the running total (sum of balances, nothing is burned).

Votes and proposals that arrive out of order (a vote before its proposal, a
message for a later height) are stashed per node and rescanned, so timing
between goroutines does not wedge a round.

`ProposerForHeight` recomputes the priority accumulator from height 1 on
every call, so it stays a pure function of the height and the set. Tests
use it. The live cluster instead gives each node an `election` that
advances the same accumulator one step per height, so a run that lasts for
weeks does not pay a cost that grows with chain length. The vote path is
`O(n^2)` per height, dominated by ed25519 verification, which is
comfortable to a few hundred validators; past that it would be batched.

## Faults

`Cluster.MakeFaulty(i)` turns validator `i` Byzantine: it broadcasts a
second vote for a junk hash every height. That vote never matches a tally
predicate, so consensus still commits on the honest majority.

Every node runs `observe` over each message it receives, keeping the first
vote it saw from each (height, voter). A second, conflicting vote is
`Evidence`. Once `verifyEvidence` confirms it (both votes validly signed by
the offender, same height, different block), the node records it and
schedules a slash for `evidence.Height + slashDelay`. Every node has the
evidence by then, so they all zero that stake at the same height and keep
computing the same proposer and the same two-thirds threshold. From the
effective height on, the offender is skipped for proposer selection and
weighs nothing in the tally. `Cluster.Evidence()` gathers what the cluster
caught.

`Cluster.SetHealDelay(heights)` makes a slash temporary. `heights` after a
slash takes effect, every node restores that validator's stake, drops the
"already caught" guard so it can be slashed again, and (on the offender's
own node) stops the double-voting. The restore is scheduled the same way
the slash is, at a fixed height every node agrees on, so the accumulators
stay in step. The offender rejoins the rotation at the back. A zero delay,
the library default, leaves a slash permanent. The demo server sets it (see
`-heal-after`) so an unattended deployment recovers on its own after
someone plays with the fault button.

This is the only fault handled, and it is not the real design: evidence
lives only in node memory, not in a block, so a chain replayed from its
blocks alone would not know a validator was slashed. A production chain
records the evidence on-chain. The fixed `slashDelay` is what keeps the
in-memory version from letting two nodes disagree on a height.

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
- Consensus runs in one process over channels, with no real network and no
  message loss, so one-step voting is not actually stress-tested for
  safety. A partition could commit two blocks at one height; two vote steps
  (prevote plus precommit) are what prevent that.
- No proposer timeout. If a height's proposer never proposes, every node
  blocks waiting for it. Nothing in a single process stalls a proposer, so
  round changes are not built.
- Slash evidence lives in node memory, not in a block, so a chain replayed
  from its blocks alone would not apply the slash (see Faults).

## Running the demo

```
go run ./cmd/dyld
```

Boots a 20-validator cluster with named validators on a skewed stake curve
(the largest holds about 13%), produces a block a second, and keeps a set of
demo accounts (a treasury, an exchange, and four people) trading in the
background so the chain is never idle. It serves an API on `:8080`:

| Route | |
|-------|-|
| `GET /state` | genesis time, height, supply, recent blocks and transactions, validators, demo accounts, seen addresses |
| `GET /events` | Server-Sent Events: one frame per block, slash, heal, and node halt |
| `GET /account?address=` | balance and next nonce |
| `POST /tx` | a signed `Transaction` as JSON |
| `POST /faucet` | `{"address": "..."}`, funds it from the faucet |
| `POST /fault` | `{"index": N}`, makes validator N double-vote; 409 if that would slash a third of stake |

A faulted validator is slashed, then restored a couple of minutes later, so
the fault budget frees up again and an unattended demo does not degrade.

Flags: `-validators`, `-addr`, `-block-time`, `-tx-every`, `-heal-after`
(heights between a slash and its recovery, 0 to keep slashes permanent). It
serves the built UI from `web/dist` when that exists, otherwise just the
API. That static handler hides dotfiles and will not list a directory.

## The explorer and wallet

```
cd web
npm install
npm run dev     # dev server on :5173, proxies the API to :8080
npm run build   # writes web/dist for go run ./cmd/dyld to serve
```

The page is a small explorer. A stat strip shows height, block time, a
transactions-per-block sparkline, circulating supply with the minted total,
and bonded stake. The validator table sits under a stacked voting-power bar
with the ⅓ and ⅔ consensus thresholds marked. Open a validator to see which
recent blocks it proposed and a button that makes it double-vote, watch the
slash arrive in the event log two blocks later, and watch it rejoin the set
a few minutes after that. Block and transaction feeds update live, blocks
expand to list their transactions, and the search box looks up any address
or recent block height.

It also holds a burner wallet: an ed25519 key pair generated in the browser
and kept in `localStorage`. Visitors fund it from the faucet and send DYL to
validators, demo accounts, or any address they paste, and their own
transfers are highlighted in the feed. The chain has no value, so a
plaintext key in the browser is an acceptable trade for zero friction.
Transactions are signed client-side over the same byte layout
`chain/transaction.go` uses, so the server verifies them with no special
path.

## Running the tests

```
go test ./...
```

No dependencies beyond the standard library. Go 1.26 or newer.
