# Working notes

Context for resuming work. Not user-facing (see `README.md` for that).

## Where we are

Stages 1 to 5 are done and committed. Eight commits, ending at "Commit
transactions with a Merkle root", plus the README and this file.

```
2a1c1a5  Commit transactions with a Merkle root      (stage 5)
fb05116  Apply transactions to state when a block is added
ac9c722  Restructure package into per-concern files
61c5829  Add ed25519 signatures to transactions       (stage 4)
532fffe  Use maps.Copy for the state map copies
af860a8  Add account state and block application       (stage 3)
d813c86  Replace opaque payload with structured transactions  (stage 2)
048f572  Add block hashing and a validated chain       (stage 1)
```

All tests pass. `gofmt`, `go vet`, `go test ./...` clean.

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
| 5 | Merkle root over transactions | done |
| 6 | Multiple validators (BFT) | next |
| 7 | Slashing and minting | after 6 |

There is also an unfinished piece of stage 5: **Merkle inclusion proofs**.
That is the smallest next task and a good downtime one. Details below.

## Immediate next task: Merkle proofs

The tree is built but you cannot yet prove a single transaction is in a
block without the whole block. That is the point of a Merkle tree and what
a light client uses.

Functions to add (in `transaction.go` or a new `merkle.go`):

```go
// sibling hashes along the path from leaf `index` up to the root
func merkleProof(txs []Transaction, index int) ([][]byte, error)

// recompute the root from a leaf + its path and check it matches
func verifyMerkleProof(leafHash []byte, index, leafCount int, proof [][]byte, root []byte) bool
```

### merkleProof

Rebuild the tree level by level like `walkTree`, tracking one position:

- if the tracked index is even, the sibling is at `index+1`; if odd, at
  `index-1`
- append that sibling hash to the proof
- move up: `index /= 2`
- stop when the level has one node

### verifyMerkleProof

Start from `leafHash`. For each sibling in the proof:

- current index even: `H(0x01 || current || sibling)` (you are on the left)
- current index odd: `H(0x01 || sibling || current)` (you are on the right)
- `index /= 2`

After all siblings are consumed, compare to `root`.

### Decision points

- Left/right ordering must match how the tree was built. Index parity is
  what tells you which side you are on. Getting it backwards gives a
  valid-looking but wrong root.
- The odd-layer duplicate-last rule has to be reproduced on both sides. If
  you are the last node in an odd layer, your sibling is yourself. This is
  why `verify` takes `leafCount`: it needs to know when a layer was odd.
- Domain separation matches the builder: `0x00` for the starting leaf,
  `0x01` for every combine step.
- Single-transaction tree: empty proof, root is the leaf hash.

### Tests

- proof verifies for every index of a 1, 2, 3, 4, 5 transaction block
- a proof with one sibling altered fails
- a proof verified against the wrong root fails
- a proof for the wrong index fails
- round trip against a real `Block.TxRoot`

## Stage 6: multiple validators (BFT)

The real one. Several validator goroutines in one process, communicating
over channels. Rough shape:

- a validator set with stake weights
- one proposer per round proposes a block
- others vote; a block commits when it has votes from more than two thirds
  of stake
- committed means final (BFT gives instant finality, unlike proof of work)

Things this forces that are currently deferred:

- **Genesis allocation into the genesis block.** Right now `NewChain`
  takes an `alloc` map and loads it straight into state; it is not in the
  block. Once nodes exchange chains, the block list has to be
  self-contained. Put the allocation in the genesis block (a field, or
  synthetic mint transactions) and add `ReplayBlocks([]Block) (State, error)`.
- `AddBlock` currently drives block production and state advance together.
  BFT will likely split "propose a candidate block" from "commit a block
  the set agreed on". Expect `AddBlock` to move behind a proposer or get a
  sibling method.
- A stronger `Validate` that replays state from genesis and confirms every
  block applies, not just that hashes link.

## Stage 7: slashing and minting

- a validator that double-signs (proposes two different blocks for one
  round) loses stake
- a mint function adds tokens per block
- this is where the proposal 46 question becomes concrete: minted per
  block vs the size of a PSE release. If minting exceeds the release, the
  pause is cosmetic.

## Parked design questions

- **Genesis allocation in the genesis block** (see stage 6). Needed once
  replay or sync exists.
- **`type Address string`** instead of bare `string` for `From`, `To`, and
  state map keys. Ergonomic only, do it anytime.
- **`Hash()` reuses `Block` with fields zeroed** to hash the header. A
  dedicated `blockHeader` struct would be cleaner. Cosmetic.
- **Block hash determinism relies on `encoding/json` field order.** Stable
  in practice, but a canonical encoder is the real answer. Low priority.
- **Consistent tip rewrite is undetectable.** Changing a tip transaction
  and recomputing its `TxRoot` passes `Validate`, because nothing links to
  the tip's hash. Closes when consensus signs blocks. Documented in a test
  (`TestValidateDoesNotCatchConsistentTipRewrite`).

## How we have been working

- One concept per stage. Write the code, then tests, then commit.
- Dylan writes the implementation and asks for hints and review. Agent
  writes tests. Flag confidence levels explicitly.
- `gofmt` + `go vet` + `go test ./...` before every commit.
- Commit messages: plain, no co-author line, no em dashes or semicolons.
- Account model, integer amounts, standard library only.
