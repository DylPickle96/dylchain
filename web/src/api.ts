import { useEffect, useRef, useState } from 'react'

export type Validator = {
  moniker: string
  address: string
  stake: number
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
// zeros. It mirrors coin.go's FormatAmount.
export function formatDYL(base: number, maxFrac = 6): string {
  const whole = Math.floor(base / UDYL)
  const frac = base % UDYL
  const w = whole.toLocaleString('en-US')
  if (frac === 0) return w
  const f = String(frac).padStart(6, '0').slice(0, maxFrac).replace(/0+$/, '')
  return f ? `${w}.${f}` : w
}

// compactDYL renders large amounts as 1.2M / 340K, small ones in full.
export function compactDYL(base: number): string {
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

// age renders how long ago a unix-seconds timestamp was.
export function age(unixSeconds: number): string {
  const s = Math.max(0, Math.round(Date.now() / 1000 - unixSeconds))
  if (s < 5) return 'just now'
  if (s < 60) return `${s}s ago`
  if (s < 3600) return `${Math.floor(s / 60)}m ago`
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`
  return `${Math.floor(s / 86400)}d ago`
}

// duration renders a span of seconds as 3d 4h, 12m, 45s.
export function duration(seconds: number): string {
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

export async function getState(): Promise<State> {
  const r = await fetch('/state')
  if (!r.ok) throw new Error(`/state ${r.status}`)
  return r.json()
}

async function post(path: string, body: unknown): Promise<string | null> {
  const r = await fetch(path, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (r.ok) return null
  return (await r.text()).trim() || `HTTP ${r.status}`
}

export const postFault = (index: number) => post('/fault', { index })
export const postFaucet = (address: string) => post('/faucet', { address })
export const postTx = (tx: unknown) => post('/tx', tx)

export async function getAccount(address: string): Promise<{ balance: number; nonce: number }> {
  const r = await fetch(`/account?address=${encodeURIComponent(address)}`)
  if (!r.ok) throw new Error(`/account ${r.status}`)
  return r.json()
}

// useCluster polls /state, refetching immediately whenever an /events frame
// arrives, and keeps a rolling log of those frames.
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
        const s = await getState()
        if (!stopped) {
          setState(s)
          setError(null)
        }
      } catch (e) {
        if (!stopped) setError(String(e))
      } finally {
        inFlight.current = false
      }
    }

    refresh()
    const poll = setInterval(refresh, 4000)

    const es = new EventSource('/events')
    es.onopen = () => !stopped && setLive(true)
    es.onerror = () => !stopped && setLive(false)
    es.onmessage = (m) => {
      let ev: ClusterEvent
      try {
        ev = { ...JSON.parse(m.data), received: Date.now() }
      } catch {
        return
      }
      if (stopped) return
      setEvents((prev) => [ev, ...prev].slice(0, 80))
      refresh()
    }

    return () => {
      stopped = true
      clearInterval(poll)
      es.close()
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
