import type { Name, State } from '../api'
import { age, formatDYL, shortHash, useTick } from '../api'
import { Icon, Who } from './bits'

export function TxFeed({
  state,
  names,
  you,
  onLookup,
}: {
  state: State
  names: (addr: string) => Name
  you?: string
  onLookup: (q: string) => void
}) {
  const now = useTick(1000)
  const txs = [...state.txs].reverse() // newest first

  return (
    <section className="panel txs">
      <header>
        <span className="title">Transactions</span>
        <span className="faint">
          latest {txs.length}
          {state.pending > 0 && ` · ${state.pending} in the mempool`}
        </span>
      </header>
      {txs.length === 0 ? (
        <div className="empty">no transactions yet</div>
      ) : (
        <div className="scroll tall">
          <table>
            <thead>
              <tr>
                <th>Tx</th>
                <th>From</th>
                <th></th>
                <th>To</th>
                <th className="num">Amount</th>
                <th className="num">Block</th>
                <th className="num">Age</th>
              </tr>
            </thead>
            <tbody>
              {txs.map((t) => {
                const yours = you !== undefined && (t.from === you || t.to === you)
                return (
                  <tr key={t.hash} className={`${now - t.time < 6 ? 'fresh' : ''}${yours ? ' yours' : ''}`}>
                    <td>
                      <button className="linkish mono faint" title={t.hash} onClick={() => onLookup(t.hash)}>
                        {shortHash(t.hash)}
                      </button>
                    </td>
                    <td>
                      <Who addr={t.from} name={names(t.from)} onClick={onLookup} />
                    </td>
                    <td className="arrow">
                      {t.kind === 'delegate' ? (
                        <span className="txkind stake" title="delegate">
                          stakes
                        </span>
                      ) : t.kind === 'undelegate' ? (
                        <span className="txkind unstake" title="undelegate">
                          unstakes
                        </span>
                      ) : (
                        <Icon name="arrow" size={13} />
                      )}
                    </td>
                    <td>
                      <Who addr={t.to} name={names(t.to)} onClick={onLookup} />
                    </td>
                    <td className="num">
                      <span className="mono">{formatDYL(t.amount)}</span> <span className="faint">DYL</span>
                    </td>
                    <td className="num">
                      <button className="linkish mono" onClick={() => onLookup(String(t.height))}>
                        {t.height.toLocaleString('en-US')}
                      </button>
                    </td>
                    <td className="num dim">{age(t.time)}</td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}
    </section>
  )
}
