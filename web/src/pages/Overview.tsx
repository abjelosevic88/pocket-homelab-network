// Overview: how many devices are here, what needs a look, who is home, the last day, recent activity and the sources.
import { Link } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Device, Overview as OverviewT } from '../lib/types'
import { ago, greeting, howLong, label, SOURCE_ICON, sourceLine, statusLine } from '../lib/network'
import { Card, Loading, useAsync } from '../components/ui'
import { Icon } from '../components/icons'
import { DeviceIcon, reviewBadge } from '../components/DeviceForm'
import { EventRow } from '../components/EventRow'
import { HourBars } from '../components/Presence'
import { openCreate, useOnChange } from '../components/Layout'
import { useEffect } from 'react'
import type React from 'react'

export function DeviceRow({ d, right, sub }: { d: Device; right?: React.ReactNode; sub?: React.ReactNode }) {
  return (
    <Link to={`/devices/${d.id}`} className="list-row dev-row">
      <DeviceIcon d={d} />
      <span className="grow min0">
        <span className="dev-name">{label(d)}{reviewBadge(d)}</span>
        <span className="muted small ellipsis block">{sub ?? [d.owner, d.last_ip, d.hostname && d.name ? d.hostname : d.vendor].filter(Boolean).join(' · ')}</span>
      </span>
      {right ?? <span className={`dev-status small ${d.online ? 'ok' : 'muted'}`}>{statusLine(d)}</span>}
    </Link>
  )
}

export default function Overview() {
  const { user } = useApp()
  const { data: o, reload, error } = useAsync(() => api.get<OverviewT>(`${V1}/overview`), [])
  useOnChange(reload)
  useEffect(() => { const t = setInterval(reload, 60_000); return () => clearInterval(t) }, [reload])
  if (error) return <div className="callout danger"><Icon name="alert" size={15} />{error}</div>
  if (!o) return <Loading />
  const first = (user?.name || '').split(' ')[0]
  const c = o.counts
  const fresh = c.total === 0
  return (
    <div className="stack loose overview">
      <div className="ov-head">
        <h1 className="home-title">{greeting()}{first && first !== 'Admin' ? `, ${first}` : ''}</h1>
        {!fresh && <p className="muted">{c.online} of {c.total} devices are online{c.new ? ` · ${c.new} new to look at` : ''}{c.watched_offline ? ` · ${c.watched_offline} watched ${c.watched_offline === 1 ? 'device is' : 'devices are'} offline` : ''}.</p>}
      </div>

      {fresh ? (
        <Card className="welcome">
          <div className="welcome-body">
            <img src="/favicon.svg" alt="" width={56} height={56} />
            <div className="grow">
              <h2>Welcome to Pocket Network</h2>
              <p className="muted">It keeps a list of everything on your home network: found by scanning this server’s network, and from Tailscale, AdGuard Home and your router. New devices wait in the review, and you hear about it when something new joins or an important device drops off.</p>
              <div className="row wrap mt">
                <button type="button" className="btn primary" onClick={() => openCreate('scan')}><Icon name="radar" size={15} />Scan now</button>
                <button type="button" className="btn" onClick={() => openCreate('source')}><Icon name="layers" size={15} />Add a source</button>
                <button type="button" className="btn ghost" onClick={() => openCreate('import')}><Icon name="upload" size={15} />Import from NetAlertX</button>
              </div>
            </div>
          </div>
        </Card>
      ) : (
        <div className="ov-kpis">
          <Link to="/devices?show=online" className="stat kpi"><span className="stat-l">Online now</span><span className="kpi-v"><b className="tnum">{c.online}</b><small>of {c.total}</small></span><span className="muted small">{o.networks.length ? o.networks.map(n => `${n.online} on ${n.name}`).join(' · ') : 'devices'}</span></Link>
          <Link to="/review" className={`stat kpi ${c.new ? 'attn' : ''}`}><span className="stat-l">New to review</span><span className="kpi-v"><b className="tnum">{c.new}</b></span><span className="muted small">{c.new ? 'Say what they are' : 'Nothing waiting'}</span></Link>
          <Link to="/devices?show=watched" className={`stat kpi ${c.watched_offline ? 'bad' : ''}`}><span className="stat-l">Watched offline</span><span className="kpi-v"><b className="tnum">{c.watched_offline}</b><small>of {c.watched}</small></span><span className="muted small">{c.watched_offline ? 'Something is down' : 'All watched devices are up'}</span></Link>
          <div className="stat kpi"><span className="stat-l">Internet address</span><span className="kpi-v mono kpi-ip">{o.wan.ip || '—'}</span><span className="muted small">{o.wan.since ? `the same for ${howLong(o.wan.since)}` : 'checked every hour'}</span></div>
        </div>
      )}

      {!fresh && <div className="home-grid">
        <Card title="Needs a look" flush actions={c.new > 0 ? <Link to="/review" className="btn sm primary">Review {c.new}</Link> : undefined}>
          {o.down.length === 0 && o.fresh.length === 0 ? <div className="pad muted row"><Icon name="check-circle" size={16} className="ok" />All quiet: nothing new, everything you watch is up.</div> : (
            <div className="list">
              {o.down.map(d => <DeviceRow key={d.id} d={d} right={<span className="badge bad">offline for {howLong(d.status_at || d.last_seen)}</span>} />)}
              {o.fresh.map(d => <DeviceRow key={d.id} d={d} right={<span className="muted small">first seen {ago(d.first_seen)}</span>} />)}
            </div>
          )}
        </Card>
        <Card title="Online over the last day" sub="Devices seen each hour">
          <HourBars hours={o.hours} />
        </Card>
        {o.home.length > 0 && <Card title="At home now" sub="Phones and watches on your own network" flush>
          <div className="list">
            {o.home.map(p => (
              <Link key={p.owner} to={`/devices?owner=${encodeURIComponent(p.owner)}`} className="list-row">
                <span className="home-av">{p.owner.slice(0, 1).toUpperCase()}</span>
                <span className="grow"><b>{p.owner}</b><span className="muted small block">{p.devices.map(d => label(d)).join(', ')}</span></span>
                <span className="muted small">{p.since ? `for ${howLong(p.since)}` : ''}</span>
              </Link>
            ))}
          </div>
        </Card>}
        <Card title="Recent activity" flush actions={<Link to="/activity" className="btn sm ghost">All activity</Link>}>
          {o.events.length === 0 ? <p className="pad muted">Nothing has happened yet.</p> : <div className="list">{o.events.slice(0, 8).map(e => <EventRow key={e.id} e={e} />)}</div>}
        </Card>
        <Card title="Sources" flush actions={<Link to="/sources" className="btn sm ghost">Manage</Link>}>
          {o.sources.length === 0 ? <div className="pad"><p className="muted">No sources yet.</p><button type="button" className="btn sm" onClick={() => openCreate('source')}><Icon name="plus" size={14} />Add a source</button></div> : (
            <div className="list">
              {o.sources.map(s => (
                <Link key={s.id} to="/sources" className="list-row">
                  <span className="list-ico"><Icon name={SOURCE_ICON[s.kind] || 'layers'} size={16} /></span>
                  <span className="grow min0"><b>{s.name}</b><span className={`small block ellipsis ${s.status === 'error' ? 'bad' : 'muted'}`}>{sourceLine(s)}</span></span>
                  <span className={`src-dot ${s.status}`} title={s.status} />
                </Link>
              ))}
            </div>
          )}
        </Card>
      </div>}
    </div>
  )
}
