import type { State } from '../api'
import { hue } from '../api'

// PowerBar stacks every validator's share of voting power in one strip,
// with the two thresholds that matter to BFT consensus marked on it.
export function PowerBar({ state, highlight }: { state: State; highlight?: string | null }) {
  const ranked = [...state.validators].sort((a, b) => b.stake - a.stake)

  return (
    <div className="powerbar-wrap">
      <div className="powerbar">
        {ranked.map((v) => {
          const h = hue(v.address)
          return (
            <span
              key={v.address}
              className={`seg${v.slashed ? ' slashed' : ''}${highlight === v.address ? ' hot' : ''}`}
              style={{
                flexGrow: Math.max(v.votingPower, 0.001),
                background: v.slashed
                  ? undefined
                  : `linear-gradient(180deg, hsl(${h} 70% 68%), hsl(${h} 68% 52%))`,
              }}
              title={`${v.moniker}: ${(v.votingPower * 100).toFixed(1)}% of voting power`}
            />
          )
        })}
        <span className="mark third" style={{ left: '33.333%' }}>
          <i>⅓</i>
        </span>
        <span className="mark twothirds" style={{ left: '66.667%' }}>
          <i>⅔</i>
        </span>
      </div>
      <div className="powerbar-legend">
        <span>
          Voting power by validator. Blocks commit on <b>⅔</b> of stake. Slashing more than <b>⅓</b> would stall the chain, so the fault button stops there.
        </span>
      </div>
    </div>
  )
}
