# Working notes

Context for resuming work. Not user-facing (see `README.md` for that).

## Where we are

Stages 1 to 6 are done. Stage 6 (one-step-vote BFT consensus) is the happy
path only. Stage 6 tip:

```
5c126db  Run one-step-vote BFT consensus across a validator cluster   6e
01c2aa8  Add the validator set                                        6d
f93f677  Sign blocks with the proposer's key                          6c
e0eaf55  Replay state from genesis in Validate                        6b
34125d3  Record the genesis allocation in the genesis block           6a
```

`gofmt`, `go vet`, `go test -race ./...` all clean.

The package and module are named `dyl`. The native coin is `DYL`, base
unit `udyl`, precision 6, with `FormatAmount` in `coin.go` for display.
Minting the coin is stage 7; supply is fixed at the genesis allocation
until then.

## The staged plan

| Stage | Concept | State |
|-------|---------|-------|
| 1 | Blocks, hashing, validated chain | done |
| 2 | Structured transactions | done |
| 3 | Account state, `Apply` | done |
| 4 | ed25519 signatures | done |
| 5 | Merkle root and inclusion proofs | done |
| 6 | Multiple validators (BFT), one-step vote, happy path | done |
| 7a | Minting: block reward to the proposer | next |
| 7b | Equivocation detection: `Evidence` from conflicting signed messages | after 7a |
| 7c | Slashing: verified evidence cuts the offender's stake | after 7b |
| 7.5 | Scaling pass: run hundreds of validators smoothly | after 7c |
| 8 | Demo backend: long-running cluster, HTTP + SSE, fault injection | after 7.5 |
| 9 | Explorer UI (React/Vite) | after 8 |

The goal is a portfolio proof of concept: the cluster running live behind
an explorer-style UI, with a button that makes a validator double-sign so
you can watch the set catch and slash it. Full unhappy-path BFT (proposer
timeouts, round changes, prevote + precommit) is out of scope; only the
double-sign detection that slashing needs is being built.

## Merkle proofs (done)

In `transaction.go`, unexported. The chain does not call them: it is a
full node and already has every transaction, so `AddBlock` / `Validate`
only recompute `merkleRoot`. Proofs are for a future light client.

```go
func merkleProof(txs []Transaction, index int) ([][]byte, error)
func verifyMerkleProof(leafHash []byte, index int, proof [][]byte, root []byte) bool
```

`merkleRoot` and `merkleProof` share `nextLayer` (odd layer duplicates
the last node, then pairs with `0x01`). Proof generation records the
sibling at the tracked index, including a copy of yourself when you are
last in an odd layer, then `index /= 2` until one node remains.

Verify starts at `leafHash` (`0x00` already applied). For each sibling:
even index `H(0x01 || current || sibling)`, odd `H(0x01 || sibling ||
current)`, then `index /= 2`. Empty proof (one tx): `leafHash` must equal
`root`.

**No `leafCount` on verify.** That parameter is for a proof that *omits*
self-hashes, so the verifier must recreate odd layers from the leaf
count. This prover *stores* the self-hash as a normal sibling, so
parity plus `index /= 2` is enough. Do not mix the two encodings.

Tests live in `merkle_test.go` next to the package (idiomatic Go: not a
separate `test/` directory, so they can call unexported helpers).

## Stage 6: multiple validators (BFT) - done, happy path

Built as `Cluster`: N `Validator` goroutines in one process over an
in-memory broadcast `bus`. Per height: the stake-weighted proposer drains
the shared `Mempool`, builds and header-signs a block, and broadcasts it;
every
validator runs `checkCandidate` against its own `Chain` and broadcasts one
signed `vote`; each validator tallies votes by stake and calls
`CommitBlock` once `3*accepted > 2*total`. Out-of-order messages (a vote
before its proposal, traffic for a later height) are stashed in
`node.pending` and rescanned by `waitFor`; `prunePending` drops decided
heights.

The three deferred items from the original plan are all done:

- **Genesis allocation in the genesis block.** `Block.Alloc`, set on the
  genesis block only, `omitempty` so non-genesis blocks and empty allocs do
  not change the hash. `ReplayBlocks([]Block) (State, error)` seeds from it
  and applies every later block. `NewChain` goes through
  `newChainFromGenesis`, which replays, so a fresh chain and a synced one
  derive genesis state the same way.
- **Propose vs commit split.** `AddBlock` stays the unsigned single-writer
  path. `CommitBlock` is the consensus path: it does not build a block,
  only re-checks a candidate (`checkCandidate`: link, height, `TxRoot`,
  proposer signature, `Apply`) and advances state.
- **Replay-based `Validate`.** It runs `ReplayBlocks` over the whole chain,
  checks the result matches `c.state`, and verifies each consensus block's
  proposer signature.

Simplifications, all deliberate:

- One process, channels not sockets. No message loss, no reordering beyond
  goroutine scheduling, no Byzantine nodes. One-step voting is not actually
  stress-tested for safety.
- One vote step, not prevote + precommit. A real partition could commit two
  blocks at one height. Two steps with locking is a later pass.
- `ProposerForHeight` runs the CometBFT priority accumulator (add stake to
  every priority, highest proposes, winner drops by total stake), but
  recomputes it from height 1 on every call to stay a pure function, at
  `O(height * n)`. Ties break by member index, not address. No
  centering/scaling step: the set is fixed and priorities stay bounded, so
  it is not needed. If runs ever get long, carry the accumulator on the
  node instead.
- No proposer timeout. If a height's proposer never proposes, every node
  blocks in `waitFor`. First thing 6f fixes.
- `Mempool` is FIFO, no validation, dedup, or fee ordering.
- `ValidatorSet` holds every validator's private key, because one process
  simulates all of them. A real node would hold only its own.
- `bus` inboxes are fixed 1024-buffer channels. A run producing more than
  that per node would block. Fine at current sizes.

## Stage 6f: dropped

Proposer timeouts, round changes, and prevote + precommit are not being
built. In a single process with no real network there is nothing to
recover from, and one-step voting stays as is. The one piece worth having,
double-sign detection, moved into 7b because slashing needs it.

## Stage 7a: minting

Block reward credited to `block.Proposer` in `Apply`, after the
transaction loop, guarded on `Proposer != ""` so genesis and `AddBlock`
blocks mint nothing. Deterministic from the header, so `ReplayBlocks` and
`Validate`'s replay check reproduce it. `BlockReward` constant in
`coin.go`. Add `State.Supply()` (sum of balances, no burning) for the UI.

The proposal 46 question becomes concrete here: `BlockReward` per block vs
the size of a PSE release. If minting outruns the release the pause is
cosmetic. For the toy, pick a round `BlockReward` and note the tension.

## Stage 7b: equivocation detection

Each node keeps a first-seen record per (height, signer). A second signed
message from that signer for the same height over different content is
`Evidence{a, b}`. `verifyEvidence`: both signatures valid, same signer,
conflicting content, same height. The node surfaces evidence on a channel
or a slice. Detection is deterministic, so every honest node produces the
same evidence independently. Scoped to height, not (height, round), since
there are no rounds.

## Stage 7c: slashing

On verified evidence against validator X, cut `X.stake` (a fraction, or to
zero). Each node applies it directly: detection is deterministic so the
sets stay in sync without an in-block evidence record. `ValidatorSet`
gains a `Slash(addr)`, stake is no longer immutable. `TotalStake`,
proposer weighting, and the 2/3 threshold all shift automatically from the
next height. Not replayable from the block list, which is the cost of the
per-node shortcut; documented, acceptable for the demo.

## Stage 7.5: scaling pass

Target ~100 to 200 validators running smoothly, block every 1 to 3
seconds. The protocol does not change, the data structures do. All the
current bottlenecks are `O(N^2)` or worse:

- **Priority accumulator on the node.** Advance one step per height
  instead of recomputing `ProposerForHeight` from height 1. Kills the
  `O(height)` factor. Needs per-node mutable state that stays in lockstep
  because every node applies the same step in the same order.
- **Index `StakeOf` / `Contains`** with a `map[string]...` built at
  construction, rebuilt on slash.
- **`pending` as `map[int64]map[string]...`** keyed by (height, signer),
  so "next uncounted vote" is a lookup, not a slice scan. The current
  linear scan is `O(N^2)` per node per height.
- **Inbox buffers `~4*N`**, not a flat 1024, or `broadcast` deadlocks.
- **Parallel vote verification** (small worker pool). ed25519 verify at
  `O(N^2)` per height is the CPU wall.
- **Validator-set generator:** N keypairs, stakes from a Zipf or
  exponential draw (a few whales, a long tail), normalized to a round
  total, a moniker each, maybe a fake uptime for the UI.

## Stage 8: demo backend

- `Cluster.RunContext(ctx)`: runs until cancelled, not a fixed height
  count.
- A driver goroutine submitting transactions on a timer.
- `net/http` (stdlib): `GET /state` JSON snapshot (height, validators with
  stake and voting-power share, balances, supply, recent blocks, slash
  events), `GET /events` SSE stream, `POST /tx`, `POST /fault` to make a
  named validator double-sign.
- Lives in `cmd/` or its own package so `package dyl` stays a pure library.

## Stage 9: explorer UI

React + Vite, a separate frontend project, talks to the stage 8 server.
Stripped explorer: block feed, validator table with voting-power bars,
supply counter, event log. The `POST /fault` button is the story: click
it, a validator's bar drops, a slash event lands in the feed.

## Parked design questions

- **`type Address string`** instead of bare `string` for `From`, `To`,
  state map keys, `Proposer`, and vote/validator addresses. Ergonomic only,
  do it anytime.
- **`Hash()` reuses `Block` with fields zeroed** to hash the header. A
  dedicated `blockHeader` struct would be cleaner, more so now the header
  also carries `Alloc` and `Proposer`. Cosmetic.
- **Block hash determinism relies on `encoding/json` field order.** Stable
  in practice, but a canonical encoder is the real answer. Low priority.
- **Block hash is derived, not stored.** On a consensus chain the proposer
  signature covers the header, so a consistent rewrite is caught
  (`TestValidateCatchesConsistentTipRewrite`,
  `TestValidateCatchesTamperedConsensusHeader`). On an unsigned `AddBlock`
  chain, a header-only field like `CreatedAt` on the tip can still be
  rewritten undetected. A stored, signed hash closes it fully.
- **`checkCandidate` runs `Apply`, then `CommitBlock` returns that state
  rather than reapplying.** Fine. If the flow grows, watch that the two do
  not drift.
- **Export `MerkleProof` / `VerifyMerkleProof`** when another package is
  actually a light client. Until then they stay unexported like
  `merkleRoot`.

## How we have been working

- One concept per stage. Write the code, then tests, then commit.
- Dylan writes the implementation and asks for hints and review. Agent
  writes tests. Flag confidence levels explicitly.
- `gofmt` + `go vet` + `go test -race ./...` before every commit.
- Commit messages: plain, no co-author line, no em dashes or semicolons.
- Account model, integer amounts, standard library only. This holds for
  `package dyl` and the stage 8 backend. The stage 9 UI is a separate
  React/Vite project and is exempt.
- Unit tests stay in `*_test.go` beside the code, `package dyl`.
