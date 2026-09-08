import { ed25519 } from '@noble/curves/ed25519.js'
import { bytesToHex, hexToBytes } from '@noble/hashes/utils.js'

// A burner wallet: an ed25519 key pair kept in localStorage. The chain has
// no real value, so this is fine; a real wallet would never store a key in
// plaintext where any script on the page can read it.

const STORAGE_KEY = 'dyl.wallet.sk'

export type Wallet = {
  priv: Uint8Array
  address: string
}

export type SignedTx = {
  From: string
  To: string
  Amount: number
  Nonce: number
  Signature: string // standard base64, what Go's json.Unmarshal wants for []byte
}

function randomPriv(): Uint8Array {
  const u = ed25519.utils as { randomSecretKey?: () => Uint8Array; randomPrivateKey?: () => Uint8Array }
  return (u.randomSecretKey ?? u.randomPrivateKey!)()
}

function derive(priv: Uint8Array): Wallet {
  return { priv, address: 'dyl' + bytesToHex(ed25519.getPublicKey(priv)) }
}

// loadWallet returns the stored wallet, or generates and stores a new one.
// A storage failure (private mode) yields an ephemeral wallet.
export function loadWallet(): Wallet {
  try {
    const hex = localStorage.getItem(STORAGE_KEY)
    if (hex && /^[0-9a-f]{64}$/.test(hex)) return derive(hexToBytes(hex))
  } catch {
    return derive(randomPriv())
  }
  return resetWallet()
}

// resetWallet throws the current key away and starts a fresh one.
export function resetWallet(): Wallet {
  const priv = randomPriv()
  try {
    localStorage.setItem(STORAGE_KEY, bytesToHex(priv))
  } catch {
    // ephemeral for this session
  }
  return derive(priv)
}

// signableBytes mirrors chain/transaction.go exactly:
//   From ‖ 0x00 ‖ To ‖ 0x00 ‖ Amount (8 bytes big-endian) ‖ Nonce (8 bytes big-endian)
function signableBytes(from: string, to: string, amount: bigint, nonce: bigint): Uint8Array {
  const enc = new TextEncoder()
  const f = enc.encode(from)
  const t = enc.encode(to)
  const out = new Uint8Array(f.length + 1 + t.length + 1 + 16)
  let o = 0
  out.set(f, o)
  o += f.length
  out[o++] = 0
  out.set(t, o)
  o += t.length
  out[o++] = 0
  const dv = new DataView(out.buffer)
  dv.setBigUint64(o, amount, false)
  dv.setBigUint64(o + 8, nonce, false)
  return out
}

function toBase64(bytes: Uint8Array): string {
  let s = ''
  for (const b of bytes) s += String.fromCharCode(b)
  return btoa(s)
}

// signTransfer builds and signs a transfer. amount is in udyl (base units).
export function signTransfer(w: Wallet, to: string, amount: bigint, nonce: number): SignedTx {
  const sig = ed25519.sign(signableBytes(w.address, to, amount, BigInt(nonce)), w.priv)
  return {
    From: w.address,
    To: to,
    Amount: Number(amount),
    Nonce: nonce,
    Signature: toBase64(sig),
  }
}
