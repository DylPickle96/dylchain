import { useCluster } from './api'
import { BlockFeed } from './components/BlockFeed'
import { EventLog } from './components/EventLog'
import { StatStrip } from './components/StatStrip'
import { ValidatorTable } from './components/ValidatorTable'

export default function App() {
  const { state, events, live, error } = useCluster()

  return (
    <div className="app">
      <header className="topbar">
        <h1>
          <span className="accent">dyl</span> explorer
        </h1>
        <span className={`live${live ? ' on' : ''}`}>
          <span className="dot" />
          {live ? 'live' : 'reconnecting'}
        </span>
      </header>

      {error && !state ? (
        <div className="empty">
          cannot reach the node. Start it with <span className="mono">go run ./cmd/dyld</span>.
        </div>
      ) : !state ? (
        <div className="empty">loading…</div>
      ) : (
        <>
          <StatStrip state={state} />
          <div className="cols">
            <ValidatorTable state={state} />
            <BlockFeed state={state} />
          </div>
          <EventLog events={events} state={state} />
        </>
      )}
    </div>
  )
}
