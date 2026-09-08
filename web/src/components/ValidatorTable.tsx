import { useState } from 'react'
import type { State, Validator } from '../api'
import { formatDYL, postFault, shortAddr } from '../api'

export function ValidatorTable({ state }: { state: State }) {
  const [open, setOpen] = useState<string | null>(null)
  // /fault takes the validator's position in the set, which is the order
  // /state returns them in, before we sort for display.
  const indexOf = new Map(state.validators.map((v, i) => [v.address, i]))
  const ranked = [...state.validators].sort((a, b) => b.stake - a.stake)

  return (
    <section className="panel">
      <header>
        <span>Validators</span>
        <span className="faint">{state.validators.length}</span>
      </header>
      <table>
        <thead>
          <tr>
            <th className="num">#</th>
            <th>Moniker</th>
            <th>Voting power</th>
            <th className="num">Proposed</th>
          </tr>
        </thead>
        <tbody>
          {ranked.map((v, i) => (
            <Row
              key={v.address}
              v={v}
              rank={i + 1}
              index={indexOf.get(v.address) ?? 0}
              blocks={state.blocks}
              open={open === v.address}
              onToggle={() => setOpen(open === v.address ? null : v.address)}
            />
          ))}
        </tbody>
      </table>
    </section>
  )
}

function Row({
  v,
  rank,
  index,
  blocks,
  open,
  onToggle,
}: {
  v: Validator
  rank: number
  index: number
  blocks: State['blocks']
  open: boolean
  onToggle: () => void
}) {
  return (
    <>
      <tr className={`clickable${v.slashed ? ' slashed' : ''}`} onClick={onToggle}>
        <td className="num faint">{rank}</td>
        <td>
          {v.moniker || <span className="mono">{shortAddr(v.address)}</span>}{' '}
          {v.slashed && <span className="badge slashed">slashed</span>}
        </td>
        <td>
          <div className="power">
            <div className="track">
              <div
                className="fill"
                style={{ width: `${Math.max(2, v.votingPower * 100).toFixed(1)}%` }}
              />
            </div>
            <span className="pct">{(v.votingPower * 100).toFixed(1)}%</span>
          </div>
        </td>
        <td className="num">{v.proposed.toLocaleString('en-US')}</td>
      </tr>
      {open && (
        <tr>
          <td colSpan={4} style={{ padding: 0 }}>
            <Detail v={v} index={index} blocks={blocks} />
          </td>
        </tr>
      )}
    </>
  )
}

function Detail({
  v,
  index,
  blocks,
}: {
  v: Validator
  index: number
  blocks: State['blocks']
}) {
  const [pending, setPending] = useState(false)
  const [note, setNote] = useState<string | null>(null)

  const trigger = async () => {
    setPending(true)
    setNote(null)
    const err = await postFault(index)
    setPending(false)
    setNote(err ?? 'fault injected: this validator will double-vote')
  }

  return (
    <div className="detail">
      <div className="grid">
        {blocks.map((b) => (
          <span
            key={b.height}
            className={`cell${b.proposer === v.address ? ' proposed' : ''}`}
            title={`height ${b.height}${b.proposer === v.address ? ', proposed by this validator' : ''}`}
          />
        ))}
      </div>
      <div className="mono faint" style={{ fontSize: 12, marginBottom: 10 }}>
        {v.address} · stake {formatDYL(v.stake)} DYL
      </div>
      <button className="fault-btn" onClick={trigger} disabled={pending || v.slashed}>
        {v.slashed ? 'already slashed' : 'make this validator double-vote'}
      </button>
      {note && <span className="note">{note}</span>}
    </div>
  )
}
