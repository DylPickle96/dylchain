import { useState } from 'react'
import type { ClusterEvent, Name, State, Validator } from '../api'
import { compactDYL, formatDYL, postFault } from '../api'
import { Avatar, Copy, Icon } from './bits'
import { PowerBar } from './PowerBar'

export function ValidatorTable({
  state,
  events,
  names,
  onLookup,
}: {
  state: State
  events: ClusterEvent[]
  names: (addr: string) => Name
  onLookup: (addr: string) => void
}) {
  const [open, setOpen] = useState<string | null>(null)
  // /fault takes the validator's position in the set, which is the order
  // /state returns them in, before we sort for display.
  const indexOf = new Map(state.validators.map((v, i) => [v.address, i]))
  const ranked = [...state.validators].sort((a, b) => b.stake - a.stake)
  const slashHeight = new Map<string, number>()
  for (const e of events) if (e.kind === 'slash') slashHeight.set(e.validator, e.height)

  return (
    <section className="panel validators">
      <header>
        <span className="title">Validators</span>
        <span className="faint">{state.validators.length} in the set</span>
      </header>
      <PowerBar state={state} highlight={open} />
      <div className="scroll">
        <table>
          <thead>
            <tr>
              <th className="num">#</th>
              <th>Validator</th>
              <th>Voting power</th>
              <th className="num">Stake</th>
              <th className="num">Blocks</th>
            </tr>
          </thead>
          <tbody>
            {ranked.map((v, i) => (
              <Row
                key={v.address}
                v={v}
                rank={i + 1}
                index={indexOf.get(v.address) ?? 0}
                state={state}
                slashedAt={slashHeight.get(v.address)}
                names={names}
                open={open === v.address}
                onToggle={() => setOpen(open === v.address ? null : v.address)}
                onLookup={onLookup}
              />
            ))}
          </tbody>
        </table>
      </div>
    </section>
  )
}

function Row({
  v,
  rank,
  index,
  state,
  slashedAt,
  names,
  open,
  onToggle,
  onLookup,
}: {
  v: Validator
  rank: number
  index: number
  state: State
  slashedAt?: number
  names: (addr: string) => Name
  open: boolean
  onToggle: () => void
  onLookup: (addr: string) => void
}) {
  return (
    <>
      <tr
        className={`clickable${v.slashed ? ' slashed' : ''}${open ? ' open' : ''}`}
        onClick={onToggle}
      >
        <td className="num faint">{rank}</td>
        <td>
          <span className="who validator">
            <Avatar addr={v.address} kind="validator" />
            <span className="label">{v.moniker || names(v.address).label}</span>
            {v.slashed && <span className="badge slashed">slashed</span>}
          </span>
        </td>
        <td>
          <div className="power">
            <div className="track">
              <div className="fill" style={{ width: `${Math.max(1.5, v.votingPower * 100).toFixed(1)}%` }} />
            </div>
            <span className="pct">{(v.votingPower * 100).toFixed(1)}%</span>
          </div>
        </td>
        <td className="num dim" title={`${formatDYL(v.stake)} DYL`}>
          {compactDYL(v.stake)}
        </td>
        <td className="num">{v.proposed.toLocaleString('en-US')}</td>
      </tr>
      {open && (
        <tr className="detail-row">
          <td colSpan={5}>
            <Detail v={v} index={index} state={state} slashedAt={slashedAt} onLookup={onLookup} />
          </td>
        </tr>
      )}
    </>
  )
}

function Detail({
  v,
  index,
  state,
  slashedAt,
  onLookup,
}: {
  v: Validator
  index: number
  state: State
  slashedAt?: number
  onLookup: (addr: string) => void
}) {
  const [pending, setPending] = useState(false)
  const [note, setNote] = useState<string | null>(null)
  const mine = state.blocks.filter((b) => b.proposer === v.address).length

  const trigger = async () => {
    setPending(true)
    setNote(null)
    const err = await postFault(index)
    setPending(false)
    setNote(err ?? 'Fault injected. This validator now signs two conflicting blocks per round until it is slashed, then recovers on its own a few minutes later. Watch the event log.')
  }

  return (
    <div className="detail">
      <div className="detail-grid">
        <div>
          <div className="detail-label">Recent blocks proposed</div>
          <div className="grid">
            {state.blocks.map((b) => (
              <span
                key={b.height}
                className={`cell${b.proposer === v.address ? ' proposed' : ''}`}
                title={`block ${b.height.toLocaleString('en-US')}${b.proposer === v.address ? ', proposed by this validator' : ''}`}
              />
            ))}
          </div>
          <div className="detail-sub">
            {mine} of the last {state.blocks.length} · {v.proposed.toLocaleString('en-US')} in total
          </div>
        </div>
        <div>
          <div className="detail-label">Address</div>
          <div className="addr-line">
            <button className="mono linkish" onClick={() => onLookup(v.address)} title="look up this address">
              {v.address}
            </button>
            <Copy text={v.address} />
          </div>
          <div className="detail-sub">
            stake {formatDYL(v.stake)} DYL · {(v.votingPower * 100).toFixed(2)}% of voting power
          </div>
        </div>
      </div>

      <div className="fault">
        {v.slashed ? (
          <div className="fault-done">
            <Icon name="alert" />
            <span>
              Slashed{slashedAt ? ` at block ${slashedAt.toLocaleString('en-US')}` : ''}. Its stake is gone and it no longer proposes or votes. The demo restores it after a couple of minutes.
            </span>
          </div>
        ) : (
          <>
            <button className="btn danger" onClick={trigger} disabled={pending}>
              <Icon name="bolt" />
              Make this validator double-vote
            </button>
            <span className="fault-hint">
              Every other node catches the conflicting signatures and slashes it two blocks later. It rejoins the set on its own a few minutes after that.
            </span>
          </>
        )}
        {note && !v.slashed && <div className="note">{note}</div>}
      </div>
    </div>
  )
}
