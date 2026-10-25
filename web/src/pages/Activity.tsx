// Activity: everything that happened on the network, newest first, by day.
import { useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import type { NetEvent } from '../lib/types'
import { fmtDate, todayISO, addDays } from '../lib/format'
import { Card, Empty, Loading, useToast } from '../components/ui'
import { EventRow } from '../components/EventRow'
import { useOnChange } from '../components/Layout'

const FILTERS: { id: string; label: string; kinds: string }[] = [
  { id: 'all', label: 'Everything', kinds: '' },
  { id: 'new', label: 'New devices', kinds: 'new' },
  { id: 'offline', label: 'Went offline', kinds: 'offline' },
  { id: 'online', label: 'Came back', kinds: 'online' },
  { id: 'ip', label: 'Addresses', kinds: 'ip,address,wan_ip' },
  { id: 'changes', label: 'Your changes', kinds: 'renamed,merged,imported,wake' },
  { id: 'sources', label: 'Sources', kinds: 'source_failed,source_ok' },
]

const localDay = (iso: string) => { const d = new Date(iso); return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}` }

export default function Activity() {
  const [sp, setSp] = useSearchParams()
  const { toast } = useToast()
  const filter = FILTERS.find(f => f.id === sp.get('kind')) || FILTERS[0]
  const [events, setEvents] = useState<NetEvent[] | null>(null)
  const [more, setMore] = useState(false)
  const [busy, setBusy] = useState(false)
  const load = async (before = '') => {
    setBusy(true)
    try {
      const page = await api.get<NetEvent[]>(`${V1}/events?limit=150${filter.kinds ? `&kind=${filter.kinds}` : ''}${before ? `&before=${encodeURIComponent(before)}` : ''}`)
      setEvents(prev => before && prev ? [...prev, ...page] : page)
      setMore(page.length === 150)
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  useEffect(() => { setEvents(null); load() }, [filter.id]) // eslint-disable-line react-hooks/exhaustive-deps
  useOnChange(() => load())
  const days: [string, NetEvent[]][] = []
  for (const e of events || []) {
    const d = localDay(e.at)
    if (!days.length || days[days.length - 1][0] !== d) days.push([d, []])
    days[days.length - 1][1].push(e)
  }
  const today = todayISO()
  const dayName = (d: string) => d === today ? 'Today' : d === addDays(today, -1) ? 'Yesterday' : fmtDate(d)
  return (
    <div className="stack">
      <div className="page-header"><div className="grow"><h1>Activity</h1><div className="sub">What happened on your network.</div></div></div>
      <div className="scroll-x"><div className="row chips-row">
        {FILTERS.map(f => <button key={f.id} type="button" className={`chip ${f.id === filter.id ? 'active' : ''}`} onClick={() => setSp(f.id === 'all' ? new URLSearchParams() : new URLSearchParams({ kind: f.id }), { replace: true })}>{f.label}</button>)}
      </div></div>
      {!events ? <Loading /> : events.length === 0 ? <Empty title="Nothing here yet" icon="activity">New devices, devices going offline and coming back, and changed addresses show up here.</Empty> : <>
        {days.map(([d, evs]) => <Card key={d} title={dayName(d)} flush><div className="list">{evs.map(e => <EventRow key={e.id} e={e} />)}</div></Card>)}
        {more && <div className="row" style={{ justifyContent: 'center' }}><button type="button" className="btn" disabled={busy} onClick={() => load(events[events.length - 1].at)}>{busy ? 'Loading…' : 'Show older'}</button></div>}
      </>}
    </div>
  )
}
