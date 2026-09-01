# Working notes

Context for resuming work. Not user-facing (see `README.md` for that).

## Where we are

Stages 1 to 5 are done, including Merkle inclusion proofs (the leftover
from the original stage 5 commit). Last committed tip before this work:

```
a790da6  Name the chain dyl and add the DYL coin
d9effdc  Add working notes for resuming development
d4daad7  Add README
2a1c1a5  Commit transactions with a Merkle root      (stage 5 root)
```

`gofmt`, `go vet`, `go test ./...` clean after the proof work.

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
| 6 | Multiple validators (BFT) | next |
| 7 | Slashing and minting | after 6 |

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
- **Export `MerkleProof` / `VerifyMerkleProof`** when another package is
  actually a light client. Until then they stay unexported like
  `merkleRoot`.

## How we have been working

- One concept per stage. Write the code, then tests, then commit.
- Dylan writes the implementation and asks for hints and review. Agent
  writes tests. Flag confidence levels explicitly.
- `gofmt` + `go vet` + `go test ./...` before every commit.
- Commit messages: plain, no co-author line, no em dashes or semicolons.
- Account model, integer amounts, standard library only.
- Unit tests stay in `*_test.go` beside the code, `package dyl`.
