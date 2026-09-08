import { useEffect, useRef, useState } from 'react'

export type Validator = {
  moniker: string
  address: string
  stake: number // effective weight: self-stake + delegations, 0 while slashed
  delegated: number // the delegated portion, shown even while slashed
  votingPower: number
  slashed: boolean
  proposed: number
}

export type BlockInfo = {
  height: number
  hash: string
  proposer: string
  txs: number
  time: number
}

export type TxInfo = {
  hash: string
  height: number
  kind?: 'delegate' | 'undelegate' | ''
  from: string
  to: string
  amount: number
  time: number
}

export type Account = {
  name?: string
  address: string
  balance: number
}

export type State = {
  genesis: number
  height: number
  supply: number
  pending: number
  blockReward: number
  faucetGrant: number
  blocks: BlockInfo[]
  txs: TxInfo[]
  validators: Validator[]
  halts: string[]
  accounts: Account[]
  seen: Account[]
}

export type ClusterEvent = {
  kind: 'block' | 'slash' | 'heal' | 'halt'
  height: number
  validator: string
  received: number
}

export const UDYL = 1_000_000

// formatDYL renders a base-unit count as a DYL amount, trimming trailing
// zeros. It mirrors coin.go's FormatAmount. A non-finite input renders as
// "0" rather than leaking "NaN" into the page.
export function formatDYL(base: number, maxFrac = 6): string {
  if (!Number.isFinite(base)) return '0'
  const n = Math.max(0, Math.floor(base))
  const whole = Math.floor(n / UDYL)
  const frac = n % UDYL
  const w = whole.toLocaleString('en-US')
  if (frac === 0) return w
  const f = String(frac).padStart(6, '0').slice(0, maxFrac).replace(/0+$/, '')
  return f ? `${w}.${f}` : w
}

// compactDYL renders large amounts as 1.2M / 340K, small ones in full.
export function compactDYL(base: number): string {
  if (!Number.isFinite(base)) return '0'
  const dyl = base / UDYL
  if (dyl >= 1_000_000) return `${(dyl / 1_000_000).toFixed(2)}M`
  if (dyl >= 10_000) return `${(dyl / 1_000).toFixed(1)}K`
  return formatDYL(base, 2)
}

export function shortAddr(addr: string): string {
  return addr.length > 14 ? `${addr.slice(0, 7)}…${addr.slice(-4)}` : addr
}

export function shortHash(hash: string): string {
  return hash.length > 12 ? `${hash.slice(0, 6)}…${hash.slice(-4)}` : hash
}

export function isAddress(s: string): boolean {
  return /^dyl[0-9a-f]{64}$/.test(s)
}

export function isTxHash(s: string): boolean {
  return /^[0-9a-f]{64}$/.test(s)
}

// age renders how long ago a unix-seconds timestamp was.
export function age(unixSeconds: number): string {
  if (!Number.isFinite(unixSeconds)) return ''
  const s = Math.max(0, Math.round(Date.now() / 1000 - unixSeconds))
  if (s < 5) return 'just now'
  if (s < 60) return `${s}s ago`
  if (s < 3600) return `${Math.floor(s / 60)}m ago`
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`
  return `${Math.floor(s / 86400)}d ago`
}

// duration renders a span of seconds as 3d 4h, 12m, 45s.
export function duration(seconds: number): string {
  if (!Number.isFinite(seconds)) return ''
  const s = Math.max(0, Math.floor(seconds))
  const d = Math.floor(s / 86400)
  const h = Math.floor((s % 86400) / 3600)
  const m = Math.floor((s % 3600) / 60)
  if (d > 0) return `${d}d ${h}h`
  if (h > 0) return `${h}h ${m}m`
  if (m > 0) return `${m}m ${s % 60}s`
  return `${s}s`
}

// dylToUdyl parses a DYL amount string into base units, or null if it is
// not a positive number with at most 6 decimal places.
export function dylToUdyl(input: string): bigint | null {
  const s = input.trim()
  if (!/^\d+(\.\d{1,6})?$/.test(s)) return null
  const [whole, frac = ''] = s.split('.')
  const base = BigInt(whole) * BigInt(UDYL) + BigInt(frac.padEnd(6, '0'))
  return base > 0n ? base : null
}

// Names resolves an address to something a person can read: a validator
// moniker, a demo account name, "you", or a shortened address.
export type Name = {
  label: string
  kind: 'validator' | 'account' | 'faucet' | 'you' | 'unknown'
}

export function makeNames(state: State, you?: string): (addr: string) => Name {
  const m = new Map<string, Name>()
  for (const v of state.validators) m.set(v.address, { label: v.moniker, kind: 'validator' })
  for (const a of state.accounts) {
    if (!a.name) continue
    const label = a.name === 'faucet' ? 'Faucet' : a.name[0].toUpperCase() + a.name.slice(1)
    m.set(a.address, { label, kind: a.name === 'faucet' ? 'faucet' : 'account' })
  }
  if (you) m.set(you, { label: 'You', kind: 'you' })
  return (addr) => m.get(addr) ?? { label: shortAddr(addr), kind: 'unknown' }
}

// hue derives a stable colour from an address, for avatars.
export function hue(addr: string): number {
  let h = 0
  for (let i = 3; i < addr.length; i++) h = (h * 31 + addr.charCodeAt(i)) >>> 0
  return h % 360
}

// ---------------------------------------------------------------------------
// Backend: "http" talks to a dyld server on the same origin; "wasm" runs the
// chain in the page (cmd/dylwasm). `npm run dev` picks http via
// .env.development; a plain build defaults to wasm, so it deploys anywhere.
// ---------------------------------------------------------------------------

const BACKEND: 'wasm' | 'http' = import.meta.env.VITE_BACKEND === 'http' ? 'http' : 'wasm'

export type AccountInfo = {
  balance: number
  nonce: number
  delegations: Record<string, number> | null // validator address -> bonded
}

export type InclusionProof = {
  height: number
  leaf: string
  index: number
  siblings: string[]
  root: string
}

function normaliseState(s: State): State {
  if (!Number.isFinite(s.blockReward)) s.blockReward = UDYL
  if (!Number.isFinite(s.faucetGrant)) s.faucetGrant = 100 * UDYL
  return s
}

// --- http backend ----------------------------------------------------------

async function httpGetState(signal?: AbortSignal): Promise<State> {
  const r = await fetch('/state', { signal })
  if (!r.ok) throw new Error(`/state ${r.status}`)
  return normaliseState((await r.json()) as State)
}

async function httpPost(path: string, body: unknown): Promise<string | null> {
  const r = await fetch(path, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (r.ok) return null
  return (await r.text()).trim() || `HTTP ${r.status}`
}

async function httpGetAccount(address: string): Promise<AccountInfo> {
  const r = await fetch(`/account?address=${encodeURIComponent(address)}`)
  if (!r.ok) throw new Error(`/account ${r.status}`)
  const a = (await r.json()) as AccountInfo
  if (!a.delegations) a.delegations = null
  return a
}

async function httpGetProof(height: number, hash: string): Promise<InclusionProof> {
  const r = await fetch(`/proof?height=${height}&hash=${encodeURIComponent(hash)}`)
  if (!r.ok) throw new Error((await r.text()).trim() || `/proof ${r.status}`)
  return r.json()
}

// --- wasm backend --------------------------------------------------------

function wasmErr(r: { ok?: boolean; error?: string }): string | null {
  return r && r.error ? r.error : null
}

async function wasmGetState(): Promise<State> {
  return normaliseState(JSON.parse(window.dylState()) as State)
}

async function wasmGetAccount(address: string): Promise<AccountInfo> {
  const a = JSON.parse(window.dylAccount(address)) as AccountInfo
  if (!a.delegations) a.delegations = null
  return a
}

async function wasmGetProof(height: number, hash: string): Promise<InclusionProof> {
  const out = window.dylProof(height, hash)
  if (typeof out !== 'string') throw new Error(out.error || 'proof failed')
  return JSON.parse(out) as InclusionProof
}

// --- public data access (dispatches on BACKEND) -------------------------

export function getState(signal?: AbortSignal): Promise<State> {
  return BACKEND === 'http' ? httpGetState(signal) : wasmGetState()
}

export function getAccount(address: string): Promise<AccountInfo> {
  return BACKEND === 'http' ? httpGetAccount(address) : wasmGetAccount(address)
}

export function getProof(height: number, hash: string): Promise<InclusionProof> {
  return BACKEND === 'http' ? httpGetProof(height, hash) : wasmGetProof(height, hash)
}

export async function postTx(tx: unknown): Promise<string | null> {
  return BACKEND === 'http' ? httpPost('/tx', tx) : wasmErr(window.dylSubmitTx(JSON.stringify(tx)))
}

export async function postFaucet(address: string): Promise<string | null> {
  return BACKEND === 'http' ? httpPost('/faucet', { address }) : wasmErr(window.dylFaucet(address))
}

export async function postFault(index: number): Promise<string | null> {
  return BACKEND === 'http' ? httpPost('/fault', { index }) : wasmErr(window.dylFault(index))
}

// --- useCluster ---------------------------------------------------------

// useCluster keeps a State snapshot, a rolling event log, and a "live"
// flag. Over http it polls /state and listens on the /events SSE stream;
// over wasm it boots the in-page chain and polls dylState() on a timer,
// refetching immediately whenever the chain pushes an event.
export function useCluster() {
  const [state, setState] = useState<State | null>(null)
  const [events, setEvents] = useState<ClusterEvent[]>([])
  const [live, setLive] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const inFlight = useRef(false)

  useEffect(() => {
    let stopped = false

    const refresh = async () => {
      if (inFlight.current) return
      inFlight.current = true
      try {
        const s = await getState(BACKEND === 'http' ? AbortSignal.timeout(8000) : undefined)
        if (stopped) return
        setState(s)
        setError(null)
        setLive(true)
      } catch (e) {
        if (stopped) return
        setError(String(e))
        setLive(false)
      } finally {
        inFlight.current = false
      }
    }

    const onEvent = (raw: unknown) => {
      if (stopped) return
      const ev = { ...(raw as object), received: Date.now() } as ClusterEvent
      setEvents((prev) => [ev, ...prev].slice(0, 80))
      refresh()
    }

    if (BACKEND === 'wasm') {
      let poll: ReturnType<typeof setInterval>
      import('./wasm').then(({ loadChain }) =>
        loadChain((_kind, data) => onEvent(data))
          .then(() => {
            if (stopped) return
            refresh()
            poll = setInterval(refresh, 1500)
          })
          .catch((e) => !stopped && setError(String(e))),
      )
      return () => {
        stopped = true
        clearInterval(poll)
      }
    }

    // http backend
    let es: EventSource | null = null
    let retry: ReturnType<typeof setTimeout> | undefined
    const connect = () => {
      if (stopped) return
      es = new EventSource('/events')
      es.onopen = () => !stopped && refresh()
      es.onmessage = (m) => {
        if (stopped) return
        try {
          onEvent(JSON.parse(m.data))
        } catch {
          /* ignore */
        }
      }
      es.onerror = () => {
        if (stopped || !es || es.readyState !== EventSource.CLOSED) return
        es.close()
        es = null
        clearTimeout(retry)
        retry = setTimeout(connect, 3000)
      }
    }
    refresh()
    const poll = setInterval(refresh, 4000)
    connect()
    return () => {
      stopped = true
      clearInterval(poll)
      clearTimeout(retry)
      es?.close()
    }
  }, [])

  return { state, events, live, error }
}

// useTick re-renders on an interval and returns the current time in unix
// seconds, for relative timestamps.
export function useTick(ms: number): number {
  const [now, set] = useState(() => Date.now() / 1000)
  useEffect(() => {
    const t = setInterval(() => set(Date.now() / 1000), ms)
    return () => clearInterval(t)
  }, [ms])
  return now
}
