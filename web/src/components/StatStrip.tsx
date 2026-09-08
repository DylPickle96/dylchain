import type { State } from '../api'
import { formatDYL } from '../api'

export function StatStrip({ state }: { state: State }) {
  const slashed = state.validators.filter((v) => v.slashed).length

  // The faucet is seeded with an effectively unlimited balance, so subtract
  // it to show the supply that actually grows with block rewards.
  const faucet = state.accounts.find((a) => a.name === 'faucet')?.balance ?? 0
  const circulating = Math.max(0, state.supply - faucet)

  return (
    <div className="stats">
      <Stat label="Block height" value={state.height.toLocaleString('en-US')} />
      <Stat label="Circulating supply" value={formatDYL(circulating)} unit="DYL" />
      <Stat label="Validators" value={String(state.validators.length)} />
      <Stat label="Slashed" value={String(slashed)} alert={slashed > 0} />
    </div>
  )
}

function Stat({
  label,
  value,
  unit,
  alert,
}: {
  label: string
  value: string
  unit?: string
  alert?: boolean
}) {
  return (
    <div className={alert ? 'stat alert' : 'stat'}>
      <div className="label">{label}</div>
      <div className="value">
        {value}
        {unit && <span className="unit">{unit}</span>}
      </div>
    </div>
  )
}
