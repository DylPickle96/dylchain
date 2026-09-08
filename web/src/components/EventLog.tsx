import type { ClusterEvent, State } from '../api'
import { shortAddr } from '../api'

export function EventLog({
  events,
  state,
}: {
  events: ClusterEvent[]
  state: State | null
}) {
  const name = (addr: string) =>
    state?.validators.find((v) => v.address === addr)?.moniker ?? shortAddr(addr)

  return (
    <section className="panel span">
      <header>
        <span>Events</span>
        <span className="faint">{events.length}</span>
      </header>
      {events.length === 0 ? (
        <div className="empty">no events yet</div>
      ) : (
        <div className="events">
          {events.map((e, i) => (
            <div className={`row ${e.kind}`} key={`${e.height}-${e.kind}-${i}`}>
              <span className="at">{clock(e.received)}</span>
              <span className="what">{describe(e, name)}</span>
            </div>
          ))}
        </div>
      )}
    </section>
  )
}

function describe(e: ClusterEvent, name: (a: string) => string): string {
  switch (e.kind) {
    case 'block':
      return `block ${e.height.toLocaleString('en-US')} proposed by ${name(e.validator)}`
    case 'slash':
      return `${name(e.validator)} slashed for a double-vote at height ${e.height.toLocaleString('en-US')}`
    case 'halt':
      return `${name(e.validator)} halted at height ${e.height.toLocaleString('en-US')}`
  }
}

function clock(ms: number): string {
  return new Date(ms).toLocaleTimeString('en-US', { hour12: false })
}
