import { useState } from 'react'
import type { Name } from '../api'
import { hue } from '../api'

// Avatar is a stable gradient derived from an address, so the same
// validator or account looks the same everywhere on the page.
export function Avatar({
  addr,
  kind,
  size = 22,
}: {
  addr: string
  kind?: Name['kind']
  size?: number
}) {
  const h = hue(addr)
  const style = {
    width: size,
    height: size,
    background: `linear-gradient(135deg, hsl(${h} 74% 64%), hsl(${(h + 48) % 360} 72% 42%))`,
  }
  return <span className={`avatar ${kind ?? ''}`} style={style} aria-hidden />
}

// Who renders an avatar and a readable name, with the full address on hover.
export function Who({
  addr,
  name,
  size,
  onClick,
}: {
  addr: string
  name: Name
  size?: number
  onClick?: (addr: string) => void
}) {
  const cls = `who ${name.kind}${onClick ? ' clickable' : ''}`
  const inner = (
    <>
      <Avatar addr={addr} kind={name.kind} size={size} />
      <span className={name.kind === 'unknown' ? 'label mono' : 'label'}>{name.label}</span>
    </>
  )
  if (onClick) {
    return (
      <button className={cls} title={addr} onClick={() => onClick(addr)}>
        {inner}
      </button>
    )
  }
  return (
    <span className={cls} title={addr}>
      {inner}
    </span>
  )
}

// Copy is a small button that copies text and briefly confirms it did.
export function Copy({ text, label }: { text: string; label?: string }) {
  const [done, setDone] = useState(false)
  const copy = () => {
    navigator.clipboard?.writeText(text).then(() => {
      setDone(true)
      setTimeout(() => setDone(false), 1200)
    })
  }
  return (
    <button className={`copy${done ? ' done' : ''}`} onClick={copy} title="copy">
      <Icon name={done ? 'check' : 'copy'} />
      {label && <span>{done ? 'copied' : label}</span>}
    </button>
  )
}

const paths: Record<string, string> = {
  search: 'M11 4a7 7 0 1 1 0 14 7 7 0 0 1 0-14zm9 16-4.3-4.3',
  copy: 'M9 9h10v11H9zM5 15V4h10',
  check: 'm5 12 4 4L19 6',
  x: 'M6 6l12 12M18 6 6 18',
  bolt: 'M13 2 4 14h7l-1 8 9-12h-7z',
  block: 'M12 3 4 7.5v9L12 21l8-4.5v-9zM4 7.5 12 12l8-4.5M12 12v9',
  arrow: 'M5 12h14m-6-6 6 6-6 6',
  alert: 'M12 3 2 20h20zM12 9v5m0 3h.01',
  github:
    'M12 2a10 10 0 0 0-3.2 19.5c.5.1.7-.2.7-.5v-1.8c-2.8.6-3.4-1.2-3.4-1.2-.4-1.1-1.1-1.5-1.1-1.5-.9-.6.1-.6.1-.6 1 .1 1.5 1 1.5 1 .9 1.6 2.4 1.1 3 .9.1-.7.4-1.1.6-1.4-2.2-.2-4.6-1.1-4.6-5a3.9 3.9 0 0 1 1-2.7c-.1-.3-.4-1.3.1-2.7 0 0 .8-.3 2.8 1a9.5 9.5 0 0 1 5 0c1.9-1.3 2.8-1 2.8-1 .5 1.4.2 2.4.1 2.7a3.9 3.9 0 0 1 1 2.7c0 3.9-2.4 4.8-4.6 5 .4.3.7.9.7 1.9v2.8c0 .3.2.6.7.5A10 10 0 0 0 12 2z',
}

// Icon is an inline stroke icon from a tiny built-in set.
export function Icon({ name, size = 15 }: { name: keyof typeof paths; size?: number }) {
  const filled = name === 'github'
  return (
    <svg
      className="icon"
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill={filled ? 'currentColor' : 'none'}
      stroke={filled ? 'none' : 'currentColor'}
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden
    >
      <path d={paths[name]} />
    </svg>
  )
}

// Sparkline is a tiny inline chart of a numeric series.
export function Sparkline({ values, width = 96, height = 26 }: { values: number[]; width?: number; height?: number }) {
  if (values.length < 2) return <svg className="spark" width={width} height={height} />
  const max = Math.max(1, ...values)
  const step = width / (values.length - 1)
  const pts = values.map((v, i) => `${(i * step).toFixed(1)},${(height - 2 - (v / max) * (height - 4)).toFixed(1)}`)
  const area = `M0,${height} L${pts.join(' L')} L${width},${height} Z`
  return (
    <svg className="spark" width={width} height={height} viewBox={`0 0 ${width} ${height}`} aria-hidden>
      <path className="area" d={area} />
      <polyline className="line" points={pts.join(' ')} />
    </svg>
  )
}
