import { useEffect, useState } from 'react'
import type { AccountInfo, Name, State } from '../api'
import { formatDYL, getAccount, isAddress, isTxHash, shortHash } from '../api'
import { Avatar, Copy, Icon, Who } from './bits'

// Lookup shows what the search box found: an account with its balance and
// recent transfers, a block with its transactions, or one transaction.
export function Lookup({
  query,
  state,
  names,
  onLookup,
  onClose,
}: {
  query: string
  state: State
  names: (addr: string) => Name
  onLookup: (q: string) => void
  onClose: () => void
}) {
  const q = query.trim().toLowerCase()
  const height = /^#?\d+$/.test(q) ? Number(q.replace('#', '')) : null
  const addr = isAddress(q) ? q : null
  const txHash = isTxHash(q) ? q : null

  const kind = addr ? 'Account' : txHash ? 'Transaction' : height !== null ? 'Block' : 'Search'

  return (
    <section className="panel lookup">
      <header>
        <span className="title">
          <Icon name="search" /> {kind}
        </span>
        <button className="linkish" onClick={onClose}>
          <Icon name="x" /> close
        </button>
      </header>
      <div className="lookup-body">
        {addr ? (
          <AccountView key={addr} addr={addr} state={state} names={names} onLookup={onLookup} />
        ) : txHash ? (
          <TxView hash={txHash} state={state} names={names} onLookup={onLookup} />
        ) : height !== null ? (
          <BlockView height={height} state={state} names={names} onLookup={onLookup} />
        ) : (
          <div className="empty">
            Nothing matches <span className="mono">{q}</span>. Paste a full address (dyl and 64 hex characters), a 64-hex
            transaction hash, or a block height.
          </div>
        )}
      </div>
    </section>
  )
}

function TxView({
  hash,
  state,
  names,
  onLookup,
}: {
  hash: string
  state: State
  names: (addr: string) => Name
  onLookup: (q: string) => void
}) {
  const tx = state.txs.find((t) => t.hash === hash)
  if (!tx) {
    const oldest = state.txs[0]?.height
    return (
      <div className="empty">
        No transaction with that hash in the recent window
        {oldest !== undefined ? ` (blocks ${oldest.toLocaleString('en-US')} to ${state.height.toLocaleString('en-US')})` : ''}.
        It may still be in the mempool, or it has scrolled out of the feed.
      </div>
    )
  }
  const label = tx.kind === 'delegate' ? 'Delegate' : tx.kind === 'undelegate' ? 'Undelegate' : 'Transfer'
  return (
    <>
      <div className="lookup-name">
        {label}
        <span className="badge kind">block {tx.height.toLocaleString('en-US')}</span>
      </div>
      <div className="kv">
        <span className="k">hash</span>
        <span className="v mono">
          {tx.hash} <Copy text={tx.hash} />
        </span>
        <span className="k">{tx.kind === 'delegate' ? 'delegator' : tx.kind === 'undelegate' ? 'delegator' : 'from'}</span>
        <span className="v">
          <Who addr={tx.from} name={names(tx.from)} onClick={onLookup} />
        </span>
        <span className="k">{tx.kind ? 'validator' : 'to'}</span>
        <span className="v">
          <Who addr={tx.to} name={names(tx.to)} onClick={onLookup} />
        </span>
        <span className="k">amount</span>
        <span className="v mono">{formatDYL(tx.amount)} DYL</span>
        <span className="k">block</span>
        <span className="v">
          <button className="linkish mono" onClick={() => onLookup(String(tx.height))}>
            {tx.height.toLocaleString('en-US')}
          </button>
        </span>
        <span className="k">time</span>
        <span className="v">{new Date(tx.time * 1000).toLocaleString()}</span>
      </div>
    </>
  )
}

function AccountView({
  addr,
  state,
  names,
  onLookup,
}: {
  addr: string
  state: State
  names: (addr: string) => Name
  onLookup: (q: string) => void
}) {
  const [acct, setAcct] = useState<AccountInfo | null>(null)
  const [err, setErr] = useState<string | null>(null)
  // Re-fetch on every block so the balance tracks the chain. The component
  // is keyed on the address by its parent, so state resets on a new lookup.
  useEffect(() => {
    let stale = false
    getAccount(addr)
      .then((a) => !stale && setAcct(a))
      .catch((e) => !stale && setErr(String(e)))
    return () => {
      stale = true
    }
  }, [addr, state.height])

  const name = names(addr)
  const validator = state.validators.find((v) => v.address === addr)
  const txs = [...state.txs].reverse().filter((t) => t.from === addr || t.to === addr)

  return (
    <>
      <div className="wallet-id">
        <Avatar addr={addr} kind={name.kind} size={40} />
        <div>
          <div className="lookup-name">
            {name.label}
            {validator && <span className="badge kind">validator</span>}
            {validator?.slashed && <span className="badge slashed">slashed</span>}
            {name.kind === 'account' && <span className="badge kind">demo account</span>}
            {name.kind === 'you' && <span className="badge kind">this browser</span>}
          </div>
          <div className="addr-row">
            <span className="mono dim">{addr}</span>
            <Copy text={addr} />
          </div>
        </div>
      </div>
      <div className="kv">
        <span className="k">balance</span>
        <span className="v mono">{acct ? `${formatDYL(acct.balance)} DYL` : err ? 'unavailable' : '…'}</span>
        <span className="k">nonce</span>
        <span className="v mono">{acct ? acct.nonce : '…'}</span>
        {validator && (
          <>
            <span className="k">bonded</span>
            <span className="v mono">
              {formatDYL(validator.stake)} DYL
              {validator.delegated > 0 && (
                <span className="faint"> ({formatDYL(validator.delegated)} delegated)</span>
              )}
            </span>
            <span className="k">blocks proposed</span>
            <span className="v mono">{validator.proposed.toLocaleString('en-US')}</span>
          </>
        )}
      </div>

      {acct?.delegations && Object.keys(acct.delegations).length > 0 && (
        <>
          <div className="detail-label">Staked</div>
          <ul className="txlist">
            {Object.entries(acct.delegations)
              .sort((a, b) => b[1] - a[1])
              .map(([v, amount]) => (
                <li key={v}>
                  <Who addr={v} name={names(v)} size={16} onClick={onLookup} />
                  <span className="amt mono">{formatDYL(amount)} DYL</span>
                </li>
              ))}
          </ul>
        </>
      )}

      <div className="detail-label">Recent transfers</div>
      {txs.length === 0 ? (
        <div className="detail-sub">None in the recent feed.</div>
      ) : (
        <ul className="txlist">
          {txs.map((t) => {
            const out = t.from === addr
            const other = out ? t.to : t.from
            return (
              <li key={t.hash}>
                <button className="linkish mono faint" onClick={() => onLookup(t.hash)}>
                  {shortHash(t.hash)}
                </button>
                <span className={out ? 'dir out' : 'dir in'}>{out ? 'sent' : 'got'}</span>
                <span className="amt mono">{formatDYL(t.amount)} DYL</span>
                <span className="faint">{out ? 'to' : 'from'}</span>
                <Who addr={other} name={names(other)} size={16} onClick={onLookup} />
                <button className="linkish mono faint" onClick={() => onLookup(String(t.height))}>
                  #{t.height.toLocaleString('en-US')}
                </button>
              </li>
            )
          })}
        </ul>
      )}
    </>
  )
}

function BlockView({
  height,
  state,
  names,
  onLookup,
}: {
  height: number
  state: State
  names: (addr: string) => Name
  onLookup: (q: string) => void
}) {
  const b = state.blocks.find((x) => x.height === height)
  if (!b) {
    const oldest = state.blocks[0]?.height
    return (
      <div className="empty">
        Block {height.toLocaleString('en-US')} is not in the recent window
        {oldest !== undefined ? ` (blocks ${oldest.toLocaleString('en-US')} to ${state.height.toLocaleString('en-US')} are)` : ''}.
      </div>
    )
  }
  const txs = state.txs.filter((t) => t.height === height)
  return (
    <>
      <div className="lookup-name">
        Block {b.height.toLocaleString('en-US')}
        <span className="badge kind">{b.txs} txs</span>
      </div>
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
      <div className="detail-label">Transactions</div>
      {b.txs === 0 ? (
        <div className="detail-sub">Empty block.</div>
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
    </>
  )
}
