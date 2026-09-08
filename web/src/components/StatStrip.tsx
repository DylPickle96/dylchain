import type { ReactNode } from 'react'
import type { State } from '../api'
import { compactDYL, duration, useTick } from '../api'
import { Sparkline } from './bits'

export function StatStrip({ state }: { state: State }) {
  const now = useTick(1000)

  const slashed = state.validators.filter((v) => v.slashed).length
  const active = state.validators.length - slashed

  // The faucet is seeded with an effectively unlimited balance, so subtract
  // it to show the supply that actually circulates.
  const faucet = state.accounts.find((a) => a.name === 'faucet')?.balance ?? 0
  const circulating = Math.max(0, state.supply - faucet)
  const minted = state.height * state.blockReward

  const bonded = state.validators.reduce((s, v) => s + v.stake, 0)
  const top = Math.max(0, ...state.validators.map((v) => v.votingPower))

  const blocks = state.blocks
  let blockTime = ''
  if (blocks.length > 2) {
    const span = blocks[blocks.length - 1].time - blocks[0].time
    blockTime = `${(span / (blocks.length - 1)).toFixed(1)}s`
  }
  const txsPerBlock = blocks.map((b) => b.txs)
  const recentTxs = txsPerBlock.reduce((s, n) => s + n, 0)
  const uptime = state.genesis ? duration(now - state.genesis) : ''

  return (
    <div className="stats">
      <Stat
        label="Block height"
        value={state.height.toLocaleString('en-US')}
        sub={blockTime ? `${blockTime} avg block time · up ${uptime}` : `up ${uptime}`}
      />
      <Stat
        label="Transactions"
        value={recentTxs.toLocaleString('en-US')}
        unit={`in last ${blocks.length} blocks`}
        sub={
          <span className="tx-sub">
            <Sparkline values={txsPerBlock} />
            <span className={state.pending > 0 ? 'pending on' : 'pending'}>
              {state.pending > 0 ? `${state.pending} waiting for a block` : 'mempool empty'}
            </span>
          </span>
        }
      />
      <Stat
        label="Circulating supply"
        value={compactDYL(circulating)}
        unit="DYL"
        sub={`${compactDYL(minted)} DYL minted as block rewards`}
      />
      <Stat
        label="Bonded stake"
        value={compactDYL(bonded)}
        unit="DYL"
        sub={
          slashed > 0 ? (
            <span className="danger">
              {active} active · {slashed} slashed
            </span>
          ) : (
            `${active} validators · largest holds ${(top * 100).toFixed(1)}%`
          )
        }
        alert={slashed > 0}
      />
    </div>
  )
}

function Stat({
  label,
  value,
  unit,
  sub,
  alert,
}: {
  label: string
  value: string
  unit?: string
  sub?: ReactNode
  alert?: boolean
}) {
  return (
    <div className={alert ? 'stat alert' : 'stat'}>
      <div className="label">{label}</div>
      <div className="value">
        {value}
        {unit && <span className="unit">{unit}</span>}
      </div>
      {sub && <div className="sub">{sub}</div>}
    </div>
  )
}
