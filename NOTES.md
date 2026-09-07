# Working notes

Context for resuming work. Not user-facing (see `README.md` for that).

## Where we are

Stages 1 to 6 done (BFT consensus, happy path only). 7a: blocks mint a
reward to their proposer. 7b: nodes catch a validator that double-votes.
7c: a caught double-voter is slashed to zero stake. 7.5: measured, the
cluster already scales to ~200 validators, only the inbox buffer needed a
fix. Stage 8a done (live-cluster primitives: RunContext, Snapshot, events). 8b done: cmd/dyld serves the live cluster over HTTP + SSE. Stage 9 (the UI) is next.

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
| 7.5 | Scaling pass: run hundreds of validators smoothly | done |
| 8a | Live-cluster primitives (RunContext, Snapshot, events) | done |
| 8b | Demo backend: HTTP + SSE server, tx driver | done |
| 9 | Explorer UI (React/Vite) | next |

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
  every priority, highest proposes, winner drops by total stake). The
  exported one recomputes from height 1 to stay a pure function, for tests.
  The live cluster uses `election` (`proposer.go`), a per-node copy of the
  accumulator advanced one step per height, so cost does not grow with
  chain length. Ties break by member index, not address. No
  centering/scaling step: priorities stay bounded, so it is not needed.
- No proposer timeout. If a height's proposer never proposes, every node
  blocks in `waitFor`. Not fixed (6f dropped); nothing in one process
  stalls a proposer.
- `Mempool` is FIFO, no validation, dedup, or fee ordering.
- `ValidatorSet` holds every validator's private key, because one process
  simulates all of them. A real node would hold only its own.
- `bus` inboxes are buffered at `max(1024, 16*N)` (stage 7.5). A run that
  falls more than ~16 heights behind on some node would still block, but
  nothing observed does.

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

`observe`, after appending `Evidence`, records `pendingSlash[offender] =
evidence.Height + slashDelay` (2). The node's `run` loop applies pending
slashes whose effective height has arrived, before that height's proposer
and threshold are computed, by calling `election.slash` (its own view) and
`ValidatorSet.Slash` (the shared set, for display). Every node has the
evidence by the effective height, so they all drop the stake at the same
height. `ValidatorSet` has a `sync.RWMutex`: `Slash` takes the write lock,
the readers take the read lock, `totalStake()` is the unlocked sum shared
by the locked methods (no recursive read-locking). `Cluster.ValidatorSet()`
exposes the set for reading stakes after a run.

The proposer accumulator and the commit threshold both come from the
node's `election`, not the shared set, and the slash is scheduled rather
than applied immediately. Before that (see git history) an immediate
`Slash` could let one node compute a height's proposer post-slash while
another computed it pre-slash: two proposals for one height, split votes,
permanent stall. Still not the real design, though: evidence lives in node
memory, not in a block, so a chain replayed from its blocks alone would
not know a validator was slashed.

## Stage 7.5: scaling pass - done, mostly by measuring

Target was ~100 to 200 validators, a block every 1 to 3 seconds. Added
`GenerateValidators(n)` (fresh keys, stake halving every 8 ranks, `val-NNN`
monikers) and `BenchmarkClusterRun` / `TestClusterLargeWithFault`, then
measured on an M4 Pro:

```
n=16    ~1.2 ms/height
n=64    ~10  ms/height   (flat over 20, 100, 300 heights)
n=128   ~35  ms/height
```

That already clears the target with room to spare, so the only real change
was **inbox buffers sized to the node count** (`max(1024, 16*N)`), which is
a correctness fix: a fixed 1024 overflows and deadlocks `broadcast` above
~60 validators.

The profile at n=128 is ~48% ed25519 vote verification (`O(N^2)` per
height, but height-independent and already spread across the node
goroutines) and ~33% allocation/GC. Deferred at the time; the audit
(stage 8, see below) then made the per-node accumulator load-bearing for a
weeks-long run and it was done. Still deferred, with the trigger:

- **`pending` as `map[int64][]message`** so `waitFor` scans one height's
  slice, not the whole stash. Past ~300 validators.
- **Parallel / batched vote verification.** Past ~300 validators, or if the
  demo host has few cores.
- **Indexed `StakeOf` / `Contains`.** Cheap, but the profile never showed
  them, so not now.
- **In-memory chain pruning** (keep last K blocks per node). If a demo
  runs unbounded; `seenVotes` is already pruned, `Chain.Blocks` is not.

## Stage 8a: live-cluster primitives - done

`chain/` grew what a long-running server needs:

- `Cluster.RunContext(ctx)` runs until cancelled. Nodes select on
  `ctx.Done()` inside `waitFor` and unwind via an `errStopped` panic that
  `run` recovers, so `wg.Wait()` returns promptly after cancel. `Run(n)` is
  now `drive(context.Background(), n)`.
- `Submit(tx) error` verifies the signature and rejects a malformed
  transaction at the door. Nonce and balance are still left to apply time.
- `propose` runs `applicable`, which keeps only the transactions that apply
  in order against current state and drops the rest, so a bad browser
  transaction can no longer panic a proposer in `checkCandidate`. Dropped
  transactions are gone (already drained from the mempool).
- The first node to commit each height calls `Cluster.recordBlock`, which
  advances a mutex-guarded live view (height, supply, recent blocks,
  per-proposer count, balances, nonces) and emits a `block` event.
  `observe` calls `recordSlash` on the first slash of an offender, emitting
  a `slash` event.
- `Snapshot()` copies that view plus per-validator stake and voting-power
  share under the set's read lock. `Account(addr)` returns balance and
  nonce. `Subscribe()` / `Unsubscribe()` hand out buffered `Event`
  channels; a full channel drops the event rather than stalling consensus.

## Stage 8b: the server - done

`cmd/dyld/main.go`, stdlib only. Boots N validators from
`GenerateValidators`, genesis-funds a faucet wallet (effectively
unlimited) and four named demo accounts (treasury, alice, bob, carol),
runs `RunContext` and a driver goroutine that sends a small random
transfer between demo accounts every `-tx-every`. `PaceBlocks(-block-time)`
keeps the rate watchable; default one block a second.

Endpoints, all CORS-open:

- `GET /state` - Snapshot (height, supply, recent blocks, validators with
  stake / voting-power share / slashed / proposed count) plus the demo
  accounts and the seen-address list.
- `GET /events` - SSE, one `data:` frame per `block` and `slash` Event,
  with a comment ping every 15s.
- `GET /account?address=` - balance and next nonce.
- `POST /tx` - a signed `chain.Transaction` as JSON. `Submit` checks the
  signature; a bad one is a 400.
- `POST /faucet {address}` - server signs a transfer from the faucet,
  rate-limited to once per 30s per address.
- `POST /fault {index}` - `MakeFaulty` on that validator.

The server holds the faucet and demo-account keys and tracks a per-wallet
next-nonce so back-to-back sends do not collide. "Seen addresses" fill in
from `/tx` and `/faucet` traffic. `/` serves `web/dist` if built, else a
one-line status. `cmd/dyld/main_test.go` drives every endpoint against a
live cluster.

Config: `-validators` (20), `-addr` (:8080), `-block-time` (1s),
`-tx-every` (2s).

### Abuse limits

The endpoint is meant to be pointed at the public internet on a cheap
host, so a stranger must not be able to spend the compute budget.

Most of this list is the second pass, after an Opus subagent audit (see
git log around "audit follow-up").

- `-validators` is a flag, not a request parameter, and must stay that
  way: cluster cost is `O(n^2)` verifications per height.
- `maxBlockTxs` is 64, so proposer work per block is bounded no matter how
  big the mempool grows. The mempool itself caps at 10000
  (`chain.ErrMempoolFull`, a 429).
- `Submit` validates `tx.To` as a real address, and `applyTx` does too, so
  junk recipients cannot enter the ledger or the seen list.
- `/tx`, `/faucet`, `/fault` go through a per-IP token bucket (5/s, burst
  20) in a bounded FIFO of 4096 IPs plus a global ceiling, so one client
  cannot 429 everyone. `/state` and `/account` go through a looser per-IP
  bucket (30/s, burst 60). `/faucet` also keeps its per-address 30s limit.
- `/faucet` rejects its own address and non-addresses before signing, and
  `signFrom` returns a rollback for the reserved nonce, so a rejected send
  cannot leave the faucet's local nonce ahead of the chain forever.
- `/fault` refuses (409) once faulty-plus-slashed stake would reach a
  third of the original total, and returns early (202, no work) when the
  index is already faulty.
- The seen-address list is a bounded FIFO (500), evicting the oldest,
  instead of an unbounded map that `/state` re-serialised in full.
- `seenVotes` on each node is pruned to a two-height window; without that
  it grew ~70 KB/height forever.
- `/events` caps at 512 streams in total and 3 per IP (503 past either),
  closes a connection after 30 min, and writes with a deadline so a client
  that stops reading errors out instead of parking a goroutine.
- `http.Server` has `ReadHeaderTimeout` and `IdleTimeout` (no
  `WriteTimeout`, it would cut the SSE stream); POST bodies are capped at
  16 KiB with `http.MaxBytesReader`.

Left as gold-plating for a toy: directory-listing / dotfile exposure if
`web/dist` is ever served by the bare `http.FileServer`, `uint64` amount
overflow in demo math, and a consensus-disagreement `panic` that kills the
process instead of halting one node.

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
