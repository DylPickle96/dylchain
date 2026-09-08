import type { State } from '../api'
import { shortAddr } from '../api'

export function BlockFeed({ state }: { state: State }) {
  const byAddr = new Map(state.validators.map((v) => [v.address, v.moniker]))
  const blocks = [...state.blocks].reverse() // newest first

  return (
    <section className="panel">
      <header>
        <span>Recent blocks</span>
      </header>
      {blocks.length === 0 ? (
        <div className="empty">waiting for the first block</div>
      ) : (
        <table>
          <thead>
            <tr>
              <th className="num">Height</th>
              <th>Proposer</th>
              <th className="num">Txs</th>
              <th className="num">Age</th>
            </tr>
          </thead>
          <tbody>
            {blocks.map((b) => (
              <tr key={b.height}>
                <td className="num">{b.height.toLocaleString('en-US')}</td>
                <td>
                  {byAddr.get(b.proposer) ?? (
                    <span className="mono faint">{shortAddr(b.proposer)}</span>
                  )}
                </td>
                <td className="num">{b.txs}</td>
                <td className="num dim">{age(b.time)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  )
}

function age(unixSeconds: number): string {
  const s = Math.max(0, Math.round(Date.now() / 1000 - unixSeconds))
  if (s < 60) return `${s}s`
  if (s < 3600) return `${Math.floor(s / 60)}m`
  return `${Math.floor(s / 3600)}h`
}
