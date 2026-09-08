import { useEffect, useRef, useState } from 'react'
import type { ClusterEvent, Name } from '../api'
import { Icon } from './bits'

type Toast = { id: string; kind: 'slash' | 'heal'; text: string }

// Toasts pops a brief notice for slash and heal events, so the payoff of
// the fault button is seen even when the event log is scrolled away.
export function Toasts({ events, names }: { events: ClusterEvent[]; names: (a: string) => Name }) {
  const seen = useRef<Set<string> | null>(null)
  const timers = useRef<Set<ReturnType<typeof setTimeout>>>(new Set())
  const [toasts, setToasts] = useState<Toast[]>([])

  useEffect(() => {
    const key = (e: ClusterEvent) => `${e.kind}-${e.height}-${e.validator}`

    // First render: treat the whole backlog as already seen.
    if (seen.current === null) {
      seen.current = new Set(events.map(key))
      return
    }

    const fresh: Toast[] = []
    for (const e of events) {
      if (e.kind !== 'slash' && e.kind !== 'heal') continue
      const k = key(e)
      if (seen.current.has(k)) continue
      seen.current.add(k)
      const who = names(e.validator).label
      const at = e.height.toLocaleString('en-US')
      fresh.push({
        id: k,
        kind: e.kind,
        text: e.kind === 'slash' ? `${who} slashed at block ${at}` : `${who} restored at block ${at}`,
      })
    }
    if (fresh.length === 0) return

    setToasts((prev) => [...fresh, ...prev].slice(0, 4))
    // Dismiss timers live in a ref so the next state poll re-running this
    // effect does not cancel them; they are only cleared on unmount.
    for (const t of fresh) {
      const id = setTimeout(() => {
        timers.current.delete(id)
        setToasts((prev) => prev.filter((x) => x.id !== t.id))
      }, 6500)
      timers.current.add(id)
    }
  }, [events, names])

  useEffect(() => {
    const pending = timers.current
    return () => pending.forEach(clearTimeout)
  }, [])

  if (toasts.length === 0) return null
  return (
    <div className="toasts" aria-live="polite">
      {toasts.map((t) => (
        <div key={t.id} className={`toast ${t.kind}`}>
          <Icon name={t.kind === 'slash' ? 'bolt' : 'heal'} size={15} />
          <span>{t.text}</span>
        </div>
      ))}
    </div>
  )
}
