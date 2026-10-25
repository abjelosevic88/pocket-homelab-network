// Online history: a week as a grid of hours, the last 30 days as bars, and the devices online per hour.
import { useMemo, useState } from 'react'
import type { PresenceHour } from '../lib/types'
import { addDays, fmtDate, todayISO, weekday } from '../lib/format'

const pad = (n: number) => String(n).padStart(2, '0')

/** The last seven days, one row each, 24 hour cells shaded by minutes online. */
export function WeekGrid({ hours }: { hours: PresenceHour[] }) {
  const by = useMemo(() => Object.fromEntries(hours.map(h => [h.hour, h.minutes])), [hours])
  const today = todayISO()
  const days = Array.from({ length: 7 }, (_, i) => addDays(today, i - 6))
  const [tip, setTip] = useState('')
  return (
    <div className="weekgrid">
      <div className="wg-rows">
        {days.map(d => (
          <div key={d} className="wg-row">
            <span className="wg-day">{d === today ? 'Today' : weekday(d)}</span>
            {Array.from({ length: 24 }, (_, h) => {
              const m = by[`${d}T${pad(h)}`] || 0
              return <span key={h} className="wg-cell" style={{ opacity: m ? 0.25 + 0.75 * (m / 60) : 1 }} data-on={m > 0 || undefined}
                onMouseEnter={() => setTip(`${fmtDate(d, false)} ${pad(h)}:00 — ${m ? `online ${m} min` : 'not seen'}`)} onMouseLeave={() => setTip('')} />
            })}
          </div>
        ))}
        <div className="wg-row wg-axis"><span className="wg-day" />{Array.from({ length: 24 }, (_, h) => <span key={h} className="wg-tick">{h % 6 === 0 ? pad(h) : ''}</span>)}</div>
      </div>
      <div className="wg-tip muted small">{tip || ' '}</div>
    </div>
  )
}

/** Hours online per day over the last 30 days. */
export function DayBars({ hours }: { hours: PresenceHour[] }) {
  const today = todayISO()
  const days = Array.from({ length: 30 }, (_, i) => addDays(today, i - 29))
  const per = useMemo(() => {
    const m: Record<string, number> = {}
    for (const h of hours) m[h.hour.slice(0, 10)] = (m[h.hour.slice(0, 10)] || 0) + h.minutes
    return m
  }, [hours])
  return (
    <div className="daybars" role="img" aria-label="Hours online per day, last 30 days">
      {days.map(d => {
        const min = per[d] || 0
        return <span key={d} className="db-col" title={`${fmtDate(d, false)}: ${min ? `${(min / 60).toFixed(1)} h online` : 'not seen'}`}><i style={{ height: `${Math.max(min ? 4 : 0, (min / 1440) * 100)}%` }} /></span>
      })}
    </div>
  )
}

/** Devices seen per hour, the last 24 hours. */
export function HourBars({ hours }: { hours: { hour: string; count: number }[] }) {
  const max = Math.max(1, ...hours.map(h => h.count))
  return (
    <div className="hourbars">
      <div className="hb-bars">
        {hours.map(h => <span key={h.hour} className="hb-col" title={`${h.hour.slice(11)}:00 — ${h.count} ${h.count === 1 ? 'device' : 'devices'}`}><i style={{ height: `${(h.count / max) * 100}%` }} /></span>)}
      </div>
      <div className="hb-axis">{hours.map((h, i) => <span key={h.hour}>{i % 6 === 0 || i === hours.length - 1 ? h.hour.slice(11) + ':00' : ''}</span>)}</div>
    </div>
  )
}
