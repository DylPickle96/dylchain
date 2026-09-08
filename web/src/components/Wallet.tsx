import { useEffect, useMemo, useState } from 'react'
import type { Name, State } from '../api'
import { dylToUdyl, formatDYL, getAccount, postFaucet, postTx, shortAddr } from '../api'
import { signTransfer, type Wallet as W } from '../wallet'
import { Avatar, Copy, Icon } from './bits'

const CUSTOM = '__custom__'

export function Wallet({
  state,
  wallet,
  names,
  onReset,
}: {
  state: State
  wallet: W
  names: (addr: string) => Name
  onReset: () => void
}) {
  const [acct, setAcct] = useState<{ balance: number; nonce: number } | null>(null)
  const [to, setTo] = useState('')
  const [customTo, setCustomTo] = useState('')
  const [amount, setAmount] = useState('')
  const [busy, setBusy] = useState(false)
  const [msg, setMsg] = useState<{ text: string; kind: 'ok' | 'err' } | null>(null)

  const refresh = useMemo(
    () => () => getAccount(wallet.address).then(setAcct).catch(() => setAcct(null)),
    [wallet.address],
  )

  useEffect(() => {
    refresh()
    const t = setInterval(refresh, 4000)
    return () => clearInterval(t)
  }, [refresh])

  // Refresh on every new block too, so a landed transfer shows promptly.
  useEffect(() => {
    refresh()
  }, [state.height, refresh])

  const recipients = useMemo(() => {
    const seen = new Set<string>([wallet.address])
    const groups: { label: string; items: { label: string; address: string }[] }[] = [
      { label: 'Accounts', items: [] },
      { label: 'Validators', items: [] },
      { label: 'Other wallets', items: [] },
    ]
    for (const a of state.accounts) {
      if (a.name === 'faucet' || seen.has(a.address)) continue
      seen.add(a.address)
      groups[0].items.push({ label: names(a.address).label, address: a.address })
    }
    for (const v of state.validators) {
      if (seen.has(v.address)) continue
      seen.add(v.address)
      groups[1].items.push({ label: v.moniker, address: v.address })
    }
    for (const a of state.seen) {
      if (seen.has(a.address)) continue
      seen.add(a.address)
      groups[2].items.push({ label: shortAddr(a.address), address: a.address })
    }
    return groups.filter((g) => g.items.length > 0)
  }, [state, wallet.address, names])

  const activity = useMemo(
    () => [...state.txs].reverse().filter((t) => t.from === wallet.address || t.to === wallet.address).slice(0, 4),
    [state.txs, wallet.address],
  )

  const faucet = async () => {
    setBusy(true)
    setMsg(null)
    const err = await postFaucet(wallet.address)
    setBusy(false)
    setMsg(err ? { text: err, kind: 'err' } : { text: '100 DYL on the way. It lands in the next block or two.', kind: 'ok' })
    if (!err) setTimeout(refresh, 1500)
  }

  const send = async () => {
    const dest = to === CUSTOM ? customTo.trim() : to
    const amt = dylToUdyl(amount)
    if (!dest) return setMsg({ text: 'Pick a recipient.', kind: 'err' })
    if (dest === wallet.address) return setMsg({ text: 'That is your own address.', kind: 'err' })
    if (!amt) return setMsg({ text: 'Enter an amount, up to 6 decimals.', kind: 'err' })
    if (acct && Number(amt) > acct.balance) return setMsg({ text: 'Not enough DYL. Try the faucet first.', kind: 'err' })

    setBusy(true)
    setMsg(null)
    try {
      const { nonce } = await getAccount(wallet.address)
      const tx = signTransfer(wallet, dest, amt, nonce)
      const err = await postTx(tx)
      setMsg(
        err
          ? { text: err, kind: 'err' }
          : { text: `Sent ${formatDYL(Number(amt))} DYL to ${names(dest).label}. Signed in your browser, verified by every validator.`, kind: 'ok' },
      )
      if (!err) {
        setAmount('')
        setTimeout(refresh, 1500)
      }
    } catch (e) {
      setMsg({ text: String(e), kind: 'err' })
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className="panel wallet">
      <header>
        <span className="title">Your wallet</span>
        <button
          className="linkish"
          onClick={() => {
            if (confirm('Throw this key away and start a new wallet? Any balance stays with the old key.')) {
              onReset()
              setMsg({ text: 'New wallet created.', kind: 'ok' })
            }
          }}
        >
          new wallet
        </button>
      </header>

      <div className="wallet-body">
        <div className="wallet-id">
          <Avatar addr={wallet.address} kind="you" size={40} />
          <div>
            <div className="balance">
              <span className="mono">{acct ? formatDYL(acct.balance) : '·'}</span>
              <span className="unit">DYL</span>
            </div>
            <div className="addr-row">
              <span className="mono dim" title={wallet.address}>
                {shortAddr(wallet.address)}
              </span>
              <Copy text={wallet.address} label="copy" />
            </div>
          </div>
        </div>

        <p className="wallet-blurb">
          A throwaway ed25519 key made in your browser and kept in localStorage. Nothing here has value, so play freely.
        </p>

        <button className="btn" onClick={faucet} disabled={busy}>
          <Icon name="bolt" />
          Get 100 DYL from the faucet
        </button>

        <div className="send">
          <select value={to} onChange={(e) => setTo(e.target.value)} aria-label="recipient">
            <option value="">Send to…</option>
            {recipients.map((g) => (
              <optgroup key={g.label} label={g.label}>
                {g.items.map((r) => (
                  <option key={r.address} value={r.address}>
                    {r.label}
                  </option>
                ))}
              </optgroup>
            ))}
            <option value={CUSTOM}>Paste an address…</option>
          </select>
          {to === CUSTOM && (
            <input
              className="mono"
              placeholder="dyl…"
              value={customTo}
              onChange={(e) => setCustomTo(e.target.value)}
              spellCheck={false}
            />
          )}
          <div className="amt-row">
            <input
              placeholder="Amount in DYL"
              inputMode="decimal"
              value={amount}
              onChange={(e) => setAmount(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && send()}
            />
            <button className="btn primary" onClick={send} disabled={busy}>
              Send
            </button>
          </div>
        </div>

        {msg && <div className={`wallet-msg ${msg.kind}`}>{msg.text}</div>}

        {activity.length > 0 && (
          <div className="activity">
            <div className="detail-label">Your recent activity</div>
            <ul className="txlist">
              {activity.map((t) => {
                const out = t.from === wallet.address
                const other = out ? t.to : t.from
                return (
                  <li key={t.hash}>
                    <span className={out ? 'dir out' : 'dir in'}>{out ? 'sent' : 'got'}</span>
                    <span className="amt mono">{formatDYL(t.amount)} DYL</span>
                    <span className="faint">{out ? 'to' : 'from'}</span>
                    <span className="who">
                      <Avatar addr={other} kind={names(other).kind} size={14} />
                      <span className="label">{names(other).label}</span>
                    </span>
                    <span className="faint mono">#{t.height.toLocaleString('en-US')}</span>
                  </li>
                )
              })}
            </ul>
          </div>
        )}
      </div>
    </section>
  )
}
