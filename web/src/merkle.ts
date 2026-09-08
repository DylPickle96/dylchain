import { sha256 } from '@noble/hashes/sha2.js'
import { bytesToHex, hexToBytes } from '@noble/hashes/utils.js'

// verifyInclusion folds a leaf hash up through its sibling path and returns
// the root it produces, so the caller can compare it to the block's
// transaction root. It mirrors chain/transaction.go's verifyMerkleProof:
// a 0x01 domain-separation prefix on every internal node, and index parity
// deciding whether the running hash is the left or right input.
export function foldToRoot(leafHex: string, index: number, siblingsHex: string[]): string {
  let current = hexToBytes(leafHex)
  let i = index
  for (const sibHex of siblingsHex) {
    const sib = hexToBytes(sibHex)
    const buf = new Uint8Array(1 + 32 + 32)
    buf[0] = 0x01
    if (i % 2 === 0) {
      buf.set(current, 1)
      buf.set(sib, 33)
    } else {
      buf.set(sib, 1)
      buf.set(current, 33)
    }
    current = sha256(buf)
    i = Math.floor(i / 2)
  }
  return bytesToHex(current)
}
