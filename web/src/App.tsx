import { useCallback, useMemo, useState } from 'react'
import { makeNames, useCluster } from './api'
import { BlockFeed } from './components/BlockFeed'
import { EventLog } from './components/EventLog'
import { Header } from './components/Header'
import { Lookup } from './components/Lookup'
import { StatStrip } from './components/StatStrip'
import { TxFeed } from './components/TxFeed'
import { ValidatorTable } from './components/ValidatorTable'
import { Wallet } from './components/Wallet'
import { Icon } from './components/bits'
import { loadWallet, resetWallet } from './wallet'

const REPO = 'https://github.com/DylPickle96/blockchain'

export default function App() {
  const { state, events, live, error } = useCluster()
  const [wallet, setWallet] = useState(() => loadWallet())
  const [query, setQuery] = useState<string | null>(null)

  const names = useMemo(() => (state ? makeNames(state, wallet.address) : () => ({ label: '', kind: 'unknown' as const })), [state, wallet.address])

  const lookup = useCallback((q: string) => {
    setQuery(q)
    window.scrollTo({ top: 0, behavior: 'smooth' })
  }, [])

  return (
    <div className="app">
      <Header state={state} live={live} onSearch={lookup} />

      {error && !state ? (
        <div className="panel">
          <div className="empty">
            Cannot reach the node. Start it with <span className="mono">go run ./cmd/dyld</span>.
          </div>
        </div>
      ) : !state ? (
        <div className="panel">
          <div className="empty">connecting…</div>
        </div>
      ) : (
        <>
          {query !== null && (
            <Lookup query={query} state={state} names={names} onLookup={lookup} onClose={() => setQuery(null)} />
          )}

          <StatStrip state={state} />

          {state.halts.length > 0 && (
            <div className="banner">
              <Icon name="alert" />
              <span>
                {state.halts.length} node{state.halts.length > 1 ? 's' : ''} halted on an impossible consensus state. The rest keep going.
              </span>
            </div>
          )}

          <div className="cols">
            <ValidatorTable state={state} events={events} names={names} onLookup={lookup} />
            <div className="stack">
              <Wallet
                state={state}
                wallet={wallet}
                names={names}
                onReset={() => setWallet(resetWallet())}
              />
              <BlockFeed state={state} names={names} onLookup={lookup} />
              <EventLog events={events} names={names} />
            </div>
          </div>

          <TxFeed state={state} names={names} you={wallet.address} onLookup={lookup} />
        </>
      )}

      <footer className="foot">
        <span>
          dyl is a toy blockchain written from scratch in Go: hashing, Merkle commitments, ed25519 accounts, stake-weighted BFT
          consensus, equivocation slashing. Nothing here has value.
        </span>
        <a href={REPO} target="_blank" rel="noreferrer">
          <Icon name="github" /> source
        </a>
      </footer>
    </div>
  )
}
