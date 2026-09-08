import { useState } from 'react'
import type { State } from '../api'
import { Icon } from './bits'

export function Header({
  state,
  live,
  onSearch,
}: {
  state: State | null
  live: boolean
  onSearch: (q: string) => void
}) {
  const [q, setQ] = useState('')

  return (
    <header className="topbar">
      <a className="brand" href="/">
        <Logo />
        <span className="wordmark">dyl</span>
        <span className="chip">devnet</span>
      </a>

      <form
        className="search"
        onSubmit={(e) => {
          e.preventDefault()
          if (q.trim()) onSearch(q.trim())
        }}
      >
        <Icon name="search" />
        <input
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder="address, tx hash, or block height"
          spellCheck={false}
          aria-label="search"
        />
      </form>

      <div className="status">
        {state && (
          <span className="height mono" title="latest block height">
            #{state.height.toLocaleString('en-US')}
          </span>
        )}
        <span className={`live${live ? ' on' : ''}`}>
          <span className="dot" />
          {live ? 'live' : 'reconnecting'}
        </span>
      </div>
    </header>
  )
}

function Logo() {
  return (
    <svg className="logo" viewBox="0 0 64 64" width="26" height="26" aria-hidden>
      <defs>
        <linearGradient id="lg" x1="0" y1="0" x2="1" y2="1">
          <stop offset="0" stopColor="#c4b5fd" />
          <stop offset="1" stopColor="#7c5fd9" />
        </linearGradient>
      </defs>
      <path d="M32 4 56 18v28L32 60 8 46V18z" fill="url(#lg)" />
      <path d="M32 14 46 22v20L32 50 18 42V22z" fill="#0f0a1a" opacity=".9" />
      <circle cx="32" cy="32" r="7" fill="#c4b5fd" />
    </svg>
  )
}
