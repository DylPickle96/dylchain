# Working notes

Context for resuming work. Not user-facing (see `README.md` for that).

## Where we are

Stages 1 to 6 done (BFT consensus, happy path only). 7a: blocks mint a
reward to their proposer. 7b: nodes catch a validator that double-votes.
7c: a caught double-voter is slashed to zero stake. 7.5 (scaling pass) is
next.

Stage 6 tip:

```
5c126db  Run one-step-vote BFT consensus across a validator cluster   6e
01c2aa8  Add the validator set                                        6d
f93f677  Sign blocks with the proposer's key                          6c
e0eaf55  Replay state from genesis in Validate                        6b
34125d3  Record the genesis allocation in the genesis block           6a
```

`gofmt`, `go vet`, `go test -race ./...` all clean.

The module is `dyl`. The chain library is `package chain` under `chain/`,
imported as `dyl/chain`. `cmd/dyld/` (stage 8) and `web/` (stage 9) get
added with their stages. The native coin is `DYL`, base unit `udyl`,
precision 6, with `FormatAmount` in `coin.go` for display. Supply starts at
the genesis allocation and grows by `BlockReward` per committed block.
`State.Supply()` is the running total.

## The staged plan

| Stage | Concept | State |
|-------|---------|-------|
| 1 | Blocks, hashing, validated chain | done |
| 2 | Structured transactions | done |
| 3 | Account state, `Apply` | done |
| 4 | ed25519 signatures | done |
| 5 | Merkle root and inclusion proofs | done |
| 6 | Multiple validators (BFT), one-step vote, happy path | done |
| 7a | Minting: block reward to the proposer | done |
| 7b | Equivocation detection: `Evidence` from conflicting signed messages | done |
| 7c | Slashing: verified evidence cuts the offender's stake | done |
| 7.5 | Scaling pass: run hundreds of validators smoothly | next |
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
  blocks in `waitFor`. Not fixed (6f dropped); nothing in one process
  stalls a proposer.
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

## Stage 7a: minting - done

`BlockReward` (`coin.go`, one DYL) is credited to `block.Proposer` in
`Apply`, after the transaction loop, guarded on `Proposer != ""` so genesis
and `AddBlock` blocks mint nothing. Deterministic from the header, so
`ReplayBlocks` and `Validate`'s replay-plus-`maps.Equal` check reproduce
it, and the existing cluster tests (which assert only non-validator
balances) still pass. `State.Supply()` sums balances, no burning: it starts
at the genesis allocation and grows by `BlockReward` per committed block.

The proposal 46 question becomes concrete here: `BlockReward` per block vs
the size of a PSE release. If minting outruns the release the pause is
cosmetic. The toy just picks a round number and notes the tension.

## Stage 7b: equivocation detection - done

Scoped to double votes: two votes from one validator at one height for
different block hashes. `Evidence{Offender, Height, VoteA, VoteB}` in
`evidence.go`; `verifyEvidence` requires both votes validly signed by
Offender, at Height, for different hashes.

`node.observe(m)` runs on every message `waitFor` pulls off the inbox. It
keeps `seenVotes map[int64]map[string]vote` (height then voter) and appends
to `node.evidence` when a second, conflicting vote turns up.
`Cluster.MakeFaulty(i)` sets `node.byzantine`, which makes `runHeight`
broadcast a second vote for a junk hash every height; that vote never
matches anyone's tally predicate, so consensus still commits on the honest
majority. `Cluster.Evidence()` gathers every node's evidence, dedupes by
`offender@height`, re-verifies each.

Deliberately left for later:

- **Detection can miss the final height of a fixed `Run(N)`.** Nothing
  drains a node's inbox after its run loop ends, so the offender's
  last-height votes may go unobserved. Earlier heights are swept up by the
  next round's `waitFor`. A live cluster always has a next round.
- **`node.evidence` is read without a lock**, only safe after `Run`
  returns. Stage 8's live server needs a channel or a mutex.
- **`seenVotes` grows unbounded**, one entry per (height, voter) forever.
  Needs a height-window prune for a long run (7.5 or 8).
- Double *proposals* are also equivocation but need `Evidence` to carry
  block signatures (a different signing payload). Not built.

## Stage 7c: slashing - done

`observe`, after appending `Evidence`, calls `n.set.Slash(offender)`.
`ValidatorSet.Slash` sets that validator's stake to zero, idempotently,
under a write lock. `ValidatorSet` gained a `sync.RWMutex`: `Slash` takes
the write lock, `TotalStake` / `StakeOf` / `Contains` / `ProposerForHeight`
take the read lock. `totalStake()` is the unlocked sum, so the locked
methods can share it without recursive read-locking (an `RWMutex`
deadlock). `ProposerForHeight` skips zero-stake members when picking a
winner, so a slashed validator never proposes, but the priorities slice
stays indexed by member so the schedule shifts as little as possible.
`Cluster.ValidatorSet()` exposes the set for reading stakes after a run.

Not consensus-safe, on purpose. Nodes slash at slightly different times, so
for a window some have the slashed set and some do not, and because
`ProposerForHeight` recomputes from height 1 they can briefly disagree on
the upcoming schedule. In the demo shape (a few validators, one
misbehaving, roughly equal stake) 3 of 4 clears two thirds either way so it
does not stall, but the real fix is evidence in a block so every node
applies the slash at the same height. Not replayable from the block list
for the same reason. If `TestClusterSlashesDoubleVoter` ever flakes, that
is the escalation.

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
- Lives in `cmd/` or its own package so `package chain` stays a pure library.

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
  `package chain` and the stage 8 backend. The stage 9 UI is a separate
  React/Vite project and is exempt.
- Unit tests stay in `*_test.go` beside the code, `package chain`.
