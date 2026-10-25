// Left column: the scan button, sections with live counts, networks and who is home — the Pocket Calendar sidebar.
import { useState } from 'react'
import { Link, NavLink, useNavigate } from 'react-router-dom'
import { useApp } from '../lib/app-context'
import type { Dashboard } from '../lib/types'
import { Icon } from './icons'
import { createItems, type CreateKind } from './Layout'
import { type Anchor, Menu, rectOf } from './ui'

export function Sidebar({ onCreate, onNavigate, counts, scanning }: { onCreate: (k: CreateKind) => void; onNavigate: () => void; counts: Dashboard | null; scanning: boolean }) {
  const { status, settings } = useApp()
  const navigate = useNavigate()
  const [menu, setMenu] = useState<Anchor | null>(null)
  const title = settings?.title || status?.title || 'Pocket Network'
  const c = counts?.counts
  const nav: { to: string; label: string; icon: string; end?: boolean; count?: string; badge?: string }[] = [
    { to: '/', label: 'Overview', icon: 'home', end: true },
    { to: '/devices', label: 'Devices', icon: 'list', count: c ? `${c.online}/${c.total}` : undefined },
    { to: '/review', label: 'Review', icon: 'user-check', badge: c && c.new > 0 ? String(c.new) : undefined },
    { to: '/activity', label: 'Activity', icon: 'activity' },
    { to: '/sources', label: 'Sources', icon: 'layers', badge: counts && counts.sources_failing > 0 ? '!' : undefined },
  ]
  const go = (url: string) => { onNavigate(); navigate(url) }
  return (
    <aside className="sidebar" aria-label="Network">
      <div className="sb-brand">
        <img src="/favicon.svg" alt="" className="sb-logo" />
        <span className="sb-title">{title !== 'Pocket Network' ? title : <>Pocket <b>Network</b></>}</span>
        {status?.version && <span className="sb-version" title={`Version ${status.version}`}>{/^\d/.test(status.version) ? 'v' + status.version : status.version}</span>}
      </div>
      <div className="create-split">
        <button type="button" className="btn primary create-btn grow" onClick={() => onCreate('scan')} disabled={scanning} title="Run every source now (S)">
          <Icon name="radar" size={15} strokeWidth={2.2} />{scanning ? 'Scanning…' : 'Scan now'}
        </button>
        <button type="button" className="btn primary create-caret-btn" aria-label="More ways to add" aria-haspopup="menu" aria-expanded={!!menu} onClick={e => setMenu(menu ? null : rectOf(e.currentTarget.parentElement!))}>
          <Icon name="chevron-down" size={15} />
        </button>
      </div>
      {menu && <Menu anchor={menu} items={createItems(onCreate)} onClose={() => setMenu(null)} width={248} />}

      <nav className="sb-nav" aria-label="Sections">
        {nav.map(n => (
          <NavLink key={n.to} to={n.to} end={n.end} onClick={onNavigate} className={({ isActive }) => `sb-nav-item ${isActive ? 'active' : ''}`}>
            <Icon name={n.icon} size={16} />{n.label}
            {!!n.count && <span className="sb-count">{n.count}</span>}
            {!!n.badge && <span className="sb-badge">{n.badge}</span>}
          </NavLink>
        ))}
      </nav>

      {counts && counts.networks.length > 0 && (
        <section className="sb-sec">
          <h4 className="sb-h"><span className="sb-h-btn">Networks</span></h4>
          <div className="sb-list">
            {counts.networks.map(n => (
              <button key={n.name} type="button" className="sb-list-row" onClick={() => go(`/devices?network=${encodeURIComponent(n.name)}`)}>
                <Icon name={n.name === 'Tailscale' ? 'link' : 'wifi'} size={14} /><span className="grow ellipsis">{n.name}</span><span className="sb-count tnum">{n.online}/{n.total}</span>
              </button>
            ))}
          </div>
        </section>
      )}

      {counts && counts.home.length > 0 && (
        <section className="sb-sec">
          <h4 className="sb-h"><span className="sb-h-btn">At home now</span></h4>
          <div className="sb-list">
            {counts.home.map(p => (
              <button key={p.owner} type="button" className="sb-list-row" onClick={() => go(`/devices?owner=${encodeURIComponent(p.owner)}`)}>
                <span className="sb-dot on" /><span className="grow ellipsis">{p.owner}</span><span className="muted small">{p.devices.length === 1 ? '1 device' : `${p.devices.length} devices`}</span>
              </button>
            ))}
          </div>
        </section>
      )}

      <div className="sb-foot">
        <Link to="/settings" className="sb-foot-link" onClick={onNavigate}><Icon name="settings" size={15} />Settings</Link>
        <button type="button" className="sb-foot-link" onClick={() => { onNavigate(); window.dispatchEvent(new CustomEvent('pnet:shortcuts')) }}><Icon name="keyboard" size={15} />Shortcuts</button>
      </div>
    </aside>
  )
}
