import { useState } from 'react'
import type { Name, State } from '../api'
import { age, formatDYL, shortHash, useTick } from '../api'
import { Copy, Who } from './bits'

export function BlockFeed({
  state,
  names,
  onLookup,
}: {
  state: State
  names: (addr: string) => Name
  onLookup: (q: string) => void
}) {
  const now = useTick(1000)
  const [open, setOpen] = useState<number | null>(null)
  const blocks = [...state.blocks].reverse() // newest first

  return (
    <section className="panel blocks">
      <header>
        <span className="title">Blocks</span>
        <span className="faint">latest {blocks.length}</span>
      </header>
      {blocks.length === 0 ? (
        <div className="empty">waiting for the first block</div>
      ) : (
        <div className="scroll tall">
          <table>
            <thead>
              <tr>
                <th>Height</th>
                <th>Proposer</th>
                <th className="num">Txs</th>
                <th className="num">Age</th>
              </tr>
            </thead>
            <tbody>
              {blocks.map((b) => {
                const isOpen = open === b.height
                const txs = isOpen ? state.txs.filter((t) => t.height === b.height) : []
                return (
                  <BlockRows
                    key={b.hash}
                    b={b}
                    fresh={now - b.time < 6}
                    open={isOpen}
                    txs={txs}
                    names={names}
                    onToggle={() => setOpen(isOpen ? null : b.height)}
                    onLookup={onLookup}
                  />
                )
              })}
            </tbody>
          </table>
        </div>
      )}
    </section>
  )
}

function BlockRows({
  b,
  fresh,
  open,
  txs,
  names,
  onToggle,
  onLookup,
}: {
  b: State['blocks'][number]
  fresh: boolean
  open: boolean
  txs: State['txs']
  names: (addr: string) => Name
  onToggle: () => void
  onLookup: (q: string) => void
}) {
  return (
    <>
      <tr className={`clickable${fresh ? ' fresh' : ''}${open ? ' open' : ''}`} onClick={onToggle}>
        <td>
          <span className="mono accent">{b.height.toLocaleString('en-US')}</span>
        </td>
        <td>
          <Who addr={b.proposer} name={names(b.proposer)} />
        </td>
        <td className="num">
          <span className={`pill${b.txs > 0 ? ' on' : ''}`}>{b.txs}</span>
        </td>
        <td className="num dim">{age(b.time)}</td>
      </tr>
      {open && (
        <tr className="detail-row">
          <td colSpan={4}>
            <div className="detail">
              <div className="kv">
                <span className="k">hash</span>
                <span className="v mono">
                  {b.hash} <Copy text={b.hash} />
                </span>
                <span className="k">time</span>
                <span className="v">{new Date(b.time * 1000).toLocaleString()}</span>
                <span className="k">proposer</span>
                <span className="v">
                  <Who addr={b.proposer} name={names(b.proposer)} onClick={onLookup} />
                </span>
              </div>
              {b.txs === 0 ? (
                <div className="detail-sub">No transactions in this block.</div>
              ) : txs.length === 0 ? (
                <div className="detail-sub">Its transactions have left the recent feed.</div>
              ) : (
                <ul className="txlist">
                  {txs.map((t) => (
                    <li key={t.hash}>
                      <span className="mono faint">{shortHash(t.hash)}</span>
                      <Who addr={t.from} name={names(t.from)} size={16} onClick={onLookup} />
                      <span className="faint">→</span>
                      <Who addr={t.to} name={names(t.to)} size={16} onClick={onLookup} />
                      <span className="amt mono">{formatDYL(t.amount)} DYL</span>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          </td>
        </tr>
      )}
    </>
  )
}
