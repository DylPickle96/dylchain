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
  proposer: string
  txs: number
  time: number
}

export type Account = {
  name?: string
  address: string
  balance: number
}

export type State = {
  height: number
  supply: number
  blocks: BlockInfo[]
  validators: Validator[]
  halts: string[]
  accounts: Account[]
  seen: Account[]
}

export type ClusterEvent = {
  kind: 'block' | 'slash' | 'halt'
  height: number
  validator: string
  received: number
}

const UDYL = 1_000_000

// formatDYL renders a base-unit count as a DYL amount, trimming trailing
// zeros. It mirrors coin.go's FormatAmount.
export function formatDYL(base: number): string {
  const whole = Math.floor(base / UDYL)
  const frac = base % UDYL
  const w = whole.toLocaleString('en-US')
  if (frac === 0) return w
  const f = String(frac).padStart(6, '0').replace(/0+$/, '')
  return `${w}.${f}`
}

export function shortAddr(addr: string): string {
  return addr.length > 14 ? `${addr.slice(0, 8)}…${addr.slice(-4)}` : addr
}

export async function getState(): Promise<State> {
  const r = await fetch('/state')
  if (!r.ok) throw new Error(`/state ${r.status}`)
  return r.json()
}

export async function postFault(index: number): Promise<string | null> {
  const r = await fetch('/fault', {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ index }),
  })
  if (r.ok) return null
  return (await r.text()).trim() || `HTTP ${r.status}`
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
      setEvents((prev) => [ev, ...prev].slice(0, 60))
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
