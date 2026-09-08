import { useEffect, useMemo, useState } from 'react'
import type { State } from '../api'
import { dylToUdyl, formatDYL, getAccount, postFaucet, postTx, shortAddr } from '../api'
import { loadWallet, resetWallet, signTransfer, type Wallet as W } from '../wallet'

const CUSTOM = '__custom__'

export function Wallet({ state }: { state: State }) {
  const [w, setW] = useState<W>(() => loadWallet())
  const [acct, setAcct] = useState<{ balance: number; nonce: number } | null>(null)
  const [to, setTo] = useState('')
  const [customTo, setCustomTo] = useState('')
  const [amount, setAmount] = useState('')
  const [busy, setBusy] = useState(false)
  const [msg, setMsg] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)

  const refresh = useMemo(
    () => () => getAccount(w.address).then(setAcct).catch(() => setAcct(null)),
    [w.address],
  )

  useEffect(() => {
    refresh()
    const t = setInterval(refresh, 4000)
    return () => clearInterval(t)
  }, [refresh])

  const recipients = useMemo(() => {
    const seen = new Set<string>()
    const list: { label: string; address: string }[] = []
    for (const v of state.validators) {
      if (v.address !== w.address && !seen.has(v.address)) {
        seen.add(v.address)
        list.push({ label: v.moniker, address: v.address })
      }
    }
    for (const a of state.accounts) {
      if (a.address !== w.address && !seen.has(a.address)) {
        seen.add(a.address)
        list.push({ label: a.name ?? shortAddr(a.address), address: a.address })
      }
    }
    for (const a of state.seen) {
      if (a.address !== w.address && !seen.has(a.address)) {
        seen.add(a.address)
        list.push({ label: shortAddr(a.address), address: a.address })
      }
    }
    return list
  }, [state, w.address])

  const faucet = async () => {
    setBusy(true)
    setMsg(null)
    const err = await postFaucet(w.address)
    setBusy(false)
    setMsg(err ?? 'sent 100 DYL, it lands in the next block or two')
    if (!err) setTimeout(refresh, 1500)
  }

  const send = async () => {
    const dest = to === CUSTOM ? customTo.trim() : to
    const amt = dylToUdyl(amount)
    if (!dest) return setMsg('pick a recipient')
    if (dest === w.address) return setMsg('that is your own address')
    if (!amt) return setMsg('enter an amount (up to 6 decimals)')

    setBusy(true)
    setMsg(null)
    try {
      const { nonce } = await getAccount(w.address)
      const tx = signTransfer(w, dest, amt, nonce)
      const err = await postTx(tx)
      setMsg(err ?? `sent ${formatDYL(Number(amt))} DYL, waiting for a block`)
      if (!err) {
        setAmount('')
        setTimeout(refresh, 1500)
      }
    } catch (e) {
      setMsg(String(e))
    } finally {
      setBusy(false)
    }
  }

  const copy = () => {
    navigator.clipboard?.writeText(w.address).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 1200)
    })
  }

  return (
    <section className="panel wallet">
      <header>
        <span>Your wallet</span>
        <button
          className="linkish"
          onClick={() => {
            setW(resetWallet())
            setMsg('new wallet created')
          }}
        >
          new wallet
        </button>
      </header>

      <div className="wallet-body">
        <div className="addr-row">
          <span className="mono">{shortAddr(w.address)}</span>
          <button className="linkish" onClick={copy}>
            {copied ? 'copied' : 'copy'}
          </button>
        </div>
        <div className="balance">
          <span className="mono">{acct ? formatDYL(acct.balance) : '—'}</span>
          <span className="unit">DYL</span>
        </div>

        <button className="btn" onClick={faucet} disabled={busy}>
          get 100 DYL from the faucet
        </button>

        <div className="send">
          <select value={to} onChange={(e) => setTo(e.target.value)}>
            <option value="">send to…</option>
            {recipients.map((r) => (
              <option key={r.address} value={r.address}>
                {r.label}
              </option>
            ))}
            <option value={CUSTOM}>paste an address…</option>
          </select>
          {to === CUSTOM && (
            <input
              className="mono"
              placeholder="dyl…"
              value={customTo}
              onChange={(e) => setCustomTo(e.target.value)}
            />
          )}
          <div className="amt-row">
            <input
              placeholder="amount"
              inputMode="decimal"
              value={amount}
              onChange={(e) => setAmount(e.target.value)}
            />
            <button className="btn accent" onClick={send} disabled={busy}>
              send
            </button>
          </div>
        </div>

        {msg && <div className="wallet-msg">{msg}</div>}
      </div>
    </section>
  )
}
