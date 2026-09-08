import type { ClusterEvent, Name } from '../api'
import { Icon } from './bits'

export function EventLog({
  events,
  names,
}: {
  events: ClusterEvent[]
  names: (addr: string) => Name
}) {
  const notable = events.filter((e) => e.kind !== 'block').length

  return (
    <section className="panel eventlog">
      <header>
        <span className="title">Live events</span>
        <span className="faint">{notable > 0 ? `${notable} notable` : 'streamed over SSE'}</span>
      </header>
      {events.length === 0 ? (
        <div className="empty">no events yet</div>
      ) : (
        <div className="events scroll tall">
          {events.map((e, i) => (
            <div className={`row ${e.kind}`} key={`${e.height}-${e.kind}-${i}`}>
              <span className="at mono">{clock(e.received)}</span>
              <span className="ico">
                <Icon name={e.kind === 'block' ? 'block' : e.kind === 'slash' ? 'bolt' : 'alert'} size={13} />
              </span>
              <span className="what">{describe(e, names)}</span>
            </div>
          ))}
        </div>
      )}
    </section>
  )
}

function describe(e: ClusterEvent, names: (a: string) => Name): string {
  const who = names(e.validator).label
  const h = e.height.toLocaleString('en-US')
  switch (e.kind) {
    case 'block':
      return `Block ${h} committed, proposed by ${who}`
    case 'slash':
      return `${who} slashed: it signed two conflicting votes at block ${h}`
    case 'halt':
      return `${who} halted at block ${h}`
  }
}

function clock(ms: number): string {
  return new Date(ms).toLocaleTimeString('en-US', { hour12: false })
}
