// Devices: the whole list with filters and grouping, and one device's page.
import React, { useEffect, useMemo, useState } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import type { Device, DeviceDetail, DeviceLinked, InventoryItem, MonitoringServer, Review, ServiceCheck } from '../lib/types'
import { ago, kindName, kindOf, label, statusLine } from '../lib/network'
import { fmtDate, fmtDateTime } from '../lib/format'
import { Card, Confirm, CoreHint, Empty, Field, Loading, Menu, Modal, Segmented, SettingRow, Switch, rectOf, useAsync, useDebounce, useToast, type Anchor, type MenuItem } from '../components/ui'
import { Icon } from '../components/icons'
import { Select } from '../components/Select'
import { DeviceIcon, KindSelect, OwnerSelect, PlaceSelect, PrivateHint, reviewBadge, useMeta } from '../components/DeviceForm'
import { EventRow } from '../components/EventRow'
import { DayBars, WeekGrid } from '../components/Presence'
import { announceChange, openCreate, useCrumb, useOnChange } from '../components/Layout'

type Show = 'all' | 'online' | 'offline' | 'watched' | 'new' | 'guest'
type Group = 'kind' | 'owner' | 'location' | 'network' | 'none'
const GROUP_KEY = 'pnet-group'

function matches(d: Device, q: string) {
  if (!q) return true
  const hay = `${label(d)} ${d.hostname} ${d.last_ip} ${d.last_mac} ${d.vendor} ${d.owner} ${d.location} ${d.model} ${d.notes}`.toLowerCase()
  return q.toLowerCase().split(/\s+/).filter(Boolean).every(w => hay.includes(w))
}

export default function Devices() {
  const [sp, setSp] = useSearchParams()
  const { toast } = useToast()
  const show = (sp.get('show') as Show) || 'all'
  const owner = sp.get('owner') || ''
  const network = sp.get('network') || ''
  const [q, setQ] = useState('')
  const dq = useDebounce(q, 200)
  const [group, setGroupRaw] = useState<Group>(() => (localStorage.getItem(GROUP_KEY) as Group) || 'kind')
  const setGroup = (g: Group) => { setGroupRaw(g); localStorage.setItem(GROUP_KEY, g) }
  const { data, reload, setData } = useAsync(() => api.get<Device[]>(`${V1}/devices`), [])
  useOnChange(reload)
  useEffect(() => { const t = setInterval(reload, 30_000); return () => clearInterval(t) }, [reload])
  const setParam = (k: string, v: string) => { const n = new URLSearchParams(sp); if (v) n.set(k, v); else n.delete(k); setSp(n, { replace: true }) }

  const all = data || []
  const count = (s: Show) => all.filter(d => s === 'all' || (s === 'online' ? d.online : s === 'offline' ? !d.online : s === 'watched' ? d.watch : d.review === s)).length
  const list = all.filter(d => (show === 'all' || (show === 'online' ? d.online : show === 'offline' ? !d.online : show === 'watched' ? d.watch : d.review === show))
    && (!owner || d.owner === owner) && (!network || d.last_network === network) && matches(d, dq))
  const groups = useMemo(() => {
    if (group === 'none') return [['', list]] as [string, Device[]][]
    const key = (d: Device) => group === 'kind' ? kindName(kindOf(d)) : group === 'owner' ? (d.owner || 'Nobody yet') : group === 'location' ? (d.location || 'No place yet') : (d.last_network || 'No network yet')
    const m = new Map<string, Device[]>()
    for (const d of list) { const k = key(d); m.set(k, [...(m.get(k) || []), d]) }
    return [...m.entries()].sort((a, b) => (a[0].startsWith('No') || a[0] === 'Not known yet' ? 1 : 0) - (b[0].startsWith('No') || b[0] === 'Not known yet' ? 1 : 0) || a[0].localeCompare(b[0]))
  }, [list, group])

  const watch = async (d: Device, v: boolean) => {
    setData(all.map(x => x.id === d.id ? { ...x, watch: v } : x))
    try { await api.put(`${V1}/devices/${d.id}`, { watch: v }) } catch (e) { toast((e as Error).message, 'err'); reload() }
  }

  if (!data) return <Loading />
  return (
    <div className="stack">
      <div className="page-header">
        <div className="grow"><h1>Devices</h1><div className="sub">{all.filter(d => d.online).length} of {all.length} online</div></div>
        <div className="actions"><button type="button" className="btn" onClick={() => openCreate('device')}><Icon name="plus" size={15} />Add by hand</button></div>
      </div>
      <div className="dev-toolbar">
        <div className="search-box"><Icon name="search" size={15} /><input value={q} onChange={e => setQ(e.target.value)} placeholder="Name, address, MAC, maker…" aria-label="Search devices" /></div>
        <div className="scroll-x"><Segmented size="sm" value={show} onChange={v => setParam('show', v === 'all' ? '' : v)} label="Show" options={[
          { value: 'all', label: `All ${count('all')}` }, { value: 'online', label: `Online ${count('online')}` }, { value: 'offline', label: `Offline ${count('offline')}` },
          { value: 'watched', label: `Watched ${count('watched')}` }, { value: 'new', label: `New ${count('new')}` }, { value: 'guest', label: `Guests ${count('guest')}` }]} /></div>
        <div className="dev-group"><Select ariaLabel="Group by" value={group} onChange={v => setGroup(v as Group)} options={[{ value: 'kind', label: 'By kind' }, { value: 'owner', label: 'By owner' }, { value: 'location', label: 'By place' }, { value: 'network', label: 'By network' }, { value: 'none', label: 'No groups' }]} /></div>
      </div>
      {(owner || network) && <div className="row wrap">
        {owner && <button type="button" className="chip active" onClick={() => setParam('owner', '')}>{owner}<Icon name="x" size={12} /></button>}
        {network && <button type="button" className="chip active" onClick={() => setParam('network', '')}>{network}<Icon name="x" size={12} /></button>}
      </div>}
      {all.length === 0 ? <Empty title="No devices yet" icon="radar">Run a scan, add a source or import your NetAlertX list.<div className="row wrap mt" style={{ justifyContent: 'center' }}><button type="button" className="btn primary" onClick={() => openCreate('scan')}>Scan now</button><Link to="/sources" className="btn">Sources</Link></div></Empty>
        : list.length === 0 ? <Empty title="Nothing matches" icon="search">Try another word or filter.</Empty>
          : groups.map(([g, ds]) => (
            <Card key={g || 'all'} title={g ? <>{g} <span className="muted small tnum">{ds.filter(d => d.online).length}/{ds.length}</span></> : undefined} flush>
              <div className="list">{ds.map(d => <ListRow key={d.id} d={d} onWatch={v => watch(d, v)} />)}</div>
            </Card>
          ))}
    </div>
  )
}

function ListRow({ d, onWatch }: { d: Device; onWatch: (v: boolean) => void }) {
  return (
    <div className="list-row dev-row">
      <Link to={`/devices/${d.id}`} className="dev-link">
        <DeviceIcon d={d} />
        <span className="grow min0">
          <span className="dev-name">{d.pinned && <Icon name="pin" size={12} className="muted" />}{label(d)}{reviewBadge(d)}{d.muted && <Icon name="bell-off" size={12} className="muted" />}</span>
          <span className="muted small ellipsis block">{[d.owner, d.last_ip, d.name && d.hostname && d.hostname !== d.name ? d.hostname : '', d.random_mac ? 'private address' : d.vendor].filter(Boolean).join(' · ')}</span>
        </span>
        <span className={`dev-status small ${d.online ? 'ok' : 'muted'}`}>{statusLine(d)}</span>
      </Link>
      <span className="dev-watch" title={d.watch ? 'Watched: you hear when it goes offline or comes back' : 'Not watched'}><Switch size="sm" checked={d.watch} onChange={onWatch} label={`Watch ${label(d)}`} /></span>
    </div>
  )
}

// ---------------- one device ----------------

export function DevicePage() {
  const { id } = useParams()
  const navigate = useNavigate()
  const { toast } = useToast()
  const { data, reload, error } = useAsync(() => api.get<DeviceDetail>(`${V1}/devices/${id}`), [id])
  const { data: ai } = useAsync(() => api.get<{ ready: boolean }>(`${V1}/ai/status`), [])
  useOnChange(reload)
  useCrumb(data ? label(data.device) : undefined)
  const [menu, setMenu] = useState<Anchor | null>(null)
  const [modal, setModal] = useState<'merge' | 'forget' | 'identify' | null>(null)
  const [busy, setBusy] = useState('')
  if (error) return <div className="callout danger"><Icon name="alert" size={15} />{error === 'not found' ? 'This device is not here (any more).' : error}</div>
  if (!data) return <Loading />
  const d = data.device
  const put = async (patch: Partial<Device>, msg?: string) => {
    try { await api.put(`${V1}/devices/${d.id}`, patch); if (msg) toast(msg); reload(); announceChange() } catch (e) { toast((e as Error).message, 'err') }
  }
  const review = (r: Review, extra: Partial<Device> = {}) => put({ review: r, ...extra }, r === 'known' ? 'Marked as yours' : r === 'guest' ? 'Marked as a guest’s' : 'Saved')
  const wake = async () => {
    setBusy('wake')
    try { const r = await api.post<{ ok: boolean; error?: string }>(`${V1}/devices/${d.id}/wake`); toast(r.ok ? 'Wake-on-LAN sent' : r.error || 'Failed', r.ok ? undefined : 'err'); reload() } catch (e) { toast((e as Error).message, 'err') } finally { setBusy('') }
  }
  const items: MenuItem[] = [
    { label: d.pinned ? 'Unpin from the top' : 'Pin to the top', icon: 'pin', onSelect: () => put({ pinned: !d.pinned }) },
    { label: 'Join with another device…', icon: 'merge', onSelect: () => setModal('merge') },
    ...(ai?.ready ? [{ label: 'What could this be? (AI)', icon: 'sparkles', onSelect: () => setModal('identify') }] : []),
    'sep',
    { label: 'Forget this device', icon: 'trash', danger: true, onSelect: () => setModal('forget') },
  ]
  return (
    <div className="stack loose device-page">
      <div className="dp-head">
        <DeviceIcon d={d} size={52} />
        <div className="grow min0">
          <h1 className="dp-title">{label(d)}{reviewBadge(d)}</h1>
          <div className="muted small dp-sub">
            <span className={d.online ? 'ok' : ''}>{statusLine(d)}</span>
            {d.last_ip && <span className="mono">{d.last_ip}</span>}
            {d.last_network && <span>{d.last_network}</span>}
            {d.random_mac && <PrivateHint />}
          </div>
        </div>
        <div className="row dp-actions">
          {d.link_url && <a className="btn" href={d.link_url} target="_blank" rel="noreferrer"><Icon name="external" size={15} />Open its page</a>}
          {data.can_wake && <button type="button" className="btn" onClick={wake} disabled={busy === 'wake'} title="Send Wake-on-LAN"><Icon name="power" size={15} />Wake</button>}
          <button type="button" className="btn ghost icon-only" aria-label="More" aria-haspopup="menu" onClick={e => setMenu(menu ? null : rectOf(e.currentTarget))}><Icon name="more" size={18} /></button>
        </div>
      </div>
      {menu && <Menu anchor={menu} align="right" items={items} onClose={() => setMenu(null)} width={250} />}

      {d.review === 'new' && (
        <div className="callout info review-callout">
          <Icon name="user-check" size={16} />
          <div className="grow">
            <b>Is this yours?</b> It joined {d.last_network || 'your network'} {ago(d.first_seen)}{d.vendor ? `, made by ${d.vendor}` : d.random_mac ? ', with a private address (phones, watches and laptops make these up)' : ''}. Name it below, then say whose it is.
            <div className="row wrap mt">
              <button type="button" className="btn primary sm" onClick={() => review('known')}><Icon name="check" size={14} />Yes, it’s ours</button>
              <button type="button" className="btn sm" onClick={() => review('guest')}>A guest’s</button>
              <button type="button" className="btn sm ghost" onClick={() => review('guest', { muted: true })} title="No alerts about it; forgotten when it has not been seen for a while">Ignore it</button>
              {ai?.ready && <button type="button" className="btn sm ghost" onClick={() => setModal('identify')}><Icon name="sparkles" size={14} />What could it be?</button>}
            </div>
          </div>
        </div>
      )}

      {data.similar.length > 0 && (
        <div className="callout"><Icon name="merge" size={16} /><div className="grow">
          <b>{data.similar.length === 1 ? 'Another device has the same name.' : `${data.similar.length} other devices have the same name.`}</b> It may be this one under an older private address.
          <div className="list mt">{data.similar.map(o => <div key={o.id} className="row sim-row"><Link to={`/devices/${o.id}`} className="grow">{label(o)} <span className="muted small">· {o.last_ip} · {o.last_seen ? `seen ${ago(o.last_seen)}` : 'never seen'}</span></Link>
            <button type="button" className="btn sm" onClick={async () => { try { await api.post(`${V1}/devices/${d.id}/merge`, { ids: [o.id] }); toast('Joined into one device'); reload(); announceChange() } catch (e) { toast((e as Error).message, 'err') } }}>Join into this one</button></div>)}</div>
        </div></div>
      )}

      <div className="dp-grid">
        <div className="stack loose">
          <AboutCard d={d} onSaved={() => { reload(); announceChange() }} />
          <Card title="Alerts">
            <SettingRow title="Watch it" help="Tell me when it goes offline or comes back. Good for the router, the NAS, a camera."><Switch checked={d.watch} onChange={v => put({ watch: v })} label="Watch" /></SettingRow>
            <SettingRow title="No alerts at all" help="Never send anything about this device, not even when it is new."><Switch checked={d.muted} onChange={v => put({ muted: v })} label="Mute" /></SettingRow>
            <SettingRow title="Whose it is" help={d.review === 'guest' ? 'A guest’s: it never alerts when it leaves, and is forgotten after a while away.' : d.review === 'known' ? 'Yours (or the household’s).' : 'Not reviewed yet.'}>
              <Segmented size="sm" value={d.review} onChange={v => review(v as Review)} options={[{ value: 'known', label: 'Ours' }, { value: 'guest', label: 'Guest' }, { value: 'new', label: 'Not sure' }]} />
            </SettingRow>
          </Card>
          <LinkedCard d={d} onChanged={reload} />
          <ServicesCard d={d} last={data.services} onChecked={reload} />
        </div>
        <div className="stack loose">
          <Card title="Online" sub={d.first_seen ? `First seen ${fmtDate(d.first_seen)} · ${d.online ? statusLine(d).toLowerCase() : `last seen ${ago(d.last_seen)}`}` : undefined}>
            {data.presence.length === 0 ? <p className="muted small">No online history yet.</p> : <>
              <WeekGrid hours={data.presence} />
              <h4 className="mini-h">Last 30 days</h4>
              <DayBars hours={data.presence} />
            </>}
            {d.dns_queries > 0 && <p className="muted small mt"><Icon name="shield" size={12} /> {d.dns_queries.toLocaleString()} DNS lookups in the last day{d.dns_blocked ? `, ${d.dns_blocked.toLocaleString()} blocked recently by AdGuard Home` : ''}.</p>}
          </Card>
          <Card title="How it is recognised" flush sub="The network card addresses and accounts that mean “this device”.">
            <div className="list">
              {data.ids.length === 0 && <p className="pad muted small">Nothing yet: it was added by hand without a MAC address.</p>}
              {data.ids.map(i => (
                <div key={i.id} className="list-row id-row">
                  <span className="list-ico"><Icon name={i.kind === 'tailscale' ? 'link' : i.private ? 'shield' : 'cpu'} size={15} /></span>
                  <span className="grow min0"><span className="mono">{i.value}</span>
                    <span className="muted small block">{i.kind === 'tailscale' ? 'Tailscale device' : i.private ? 'Private address (made up by the device)' : i.vendor || 'Maker unknown'}{i.last_seen ? ` · seen ${ago(i.last_seen)}` : ''}</span></span>
                  {data.ids.length > 1 && <button type="button" className="btn ghost sm" title="Not this device: the next sighting becomes a device of its own" onClick={async () => { await api.del(`${V1}/devices/${d.id}/ids/${i.id}`); reload() }}>Not this one</button>}
                </div>
              ))}
            </div>
          </Card>
          {data.addresses.length > 0 && <Card title="Addresses" flush>
            <table className="table compact">
              <thead><tr><th>Address</th><th>Network</th><th>Seen</th></tr></thead>
              <tbody>{data.addresses.slice(0, 12).map(a => <tr key={a.id}><td className="mono">{a.ip}</td><td className="muted">{a.network || data.sources[a.source_id] || '—'}</td><td className="muted small">{fmtDate(a.first_seen, false)} – {ago(a.last_seen)}</td></tr>)}</tbody>
            </table>
          </Card>}
          <Card title="Timeline" flush>
            {data.events.length === 0 ? <p className="pad muted small">Nothing yet.</p> : <div className="list">{data.events.slice(0, 40).map(e => <EventRow key={e.id} e={e} showDevice={false} />)}</div>}
          </Card>
        </div>
      </div>
      {modal === 'merge' && <MergeModal d={d} onClose={() => setModal(null)} onDone={() => { setModal(null); reload(); announceChange() }} />}
      {modal === 'identify' && <IdentifyModal d={d} onClose={() => setModal(null)} onApply={async p => { setModal(null); await put(p, 'Saved') }} />}
      {modal === 'forget' && <Confirm title={`Forget ${label(d)}?`} confirmLabel="Forget" message="Its history goes with it. If a source sees it again, it comes back as a new device." onCancel={() => setModal(null)}
        onConfirm={async () => { await api.del(`${V1}/devices/${d.id}`); announceChange(); navigate('/devices') }} />}
    </div>
  )
}

function AboutCard({ d, onSaved }: { d: Device; onSaved: () => void }) {
  const { toast } = useToast()
  const [meta, reloadMeta] = useMeta()
  const init = { name: d.name, kind: d.kind, owner: d.owner, location: d.location, notes: d.notes, link_url: d.link_url }
  const [f, setF] = useState(init)
  const [busy, setBusy] = useState(false)
  useEffect(() => { setF({ name: d.name, kind: d.kind, owner: d.owner, location: d.location, notes: d.notes, link_url: d.link_url }) }, [d.id, d.updated_at]) // eslint-disable-line react-hooks/exhaustive-deps
  const dirty = JSON.stringify(f) !== JSON.stringify(init)
  const save = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try { await api.put(`${V1}/devices/${d.id}`, f); toast('Saved'); reloadMeta(); onSaved() } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  const facts: [string, React.ReactNode][] = ([
    ['Name it gave itself', d.hostname], ['Maker', d.vendor || (d.random_mac ? 'Not known (private address)' : '')], ['Model', d.model], ['System', d.os],
    ['MAC address', d.last_mac && <span className="mono">{d.last_mac}</span>], ['Added', d.source === 'manual' ? 'by hand' : d.source === 'import' ? 'from an import' : ''],
  ] as [string, React.ReactNode][]).filter(([, v]) => !!v)
  return (
    <Card title="About it">
      <form className="stack" onSubmit={save}>
        <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} placeholder={d.hostname || (d.vendor ? `${d.vendor} device` : 'Give it a name')} /></Field>
        <div className="two-col tight">
          <Field group label="Kind"><KindSelect value={f.kind} guess={d.kind_guess} onChange={v => setF({ ...f, kind: v })} /></Field>
          <Field group label="Owner"><OwnerSelect value={f.owner} onChange={v => setF({ ...f, owner: v })} meta={meta} /></Field>
        </div>
        <div className="two-col tight">
          <Field group label="Place"><PlaceSelect value={f.location} onChange={v => setF({ ...f, location: v })} meta={meta} /></Field>
          <Field label="Its own page" help="A router or printer admin page."><input value={f.link_url} onChange={e => setF({ ...f, link_url: e.target.value })} placeholder="http://192.168.1.1" /></Field>
        </div>
        <Field label="Notes"><textarea rows={2} value={f.notes} onChange={e => setF({ ...f, notes: e.target.value })} placeholder="Where the password is, what it is for…" /></Field>
        <div className="row"><button className="btn primary" disabled={busy || !dirty}>Save</button>{dirty && <button type="button" className="btn ghost" onClick={() => setF(init)}>Undo</button>}</div>
      </form>
      {facts.length > 0 && <dl className="facts mt">{facts.map(([k, v]) => <div key={k}><dt>{k}</dt><dd>{v}</dd></div>)}</dl>}
    </Card>
  )
}

function LinkedCard({ d, onChanged }: { d: Device; onChanged: () => void }) {
  const { toast } = useToast()
  const { data, reload } = useAsync(() => api.get<DeviceLinked>(`${V1}/devices/${d.id}/linked`), [d.id, d.inventory_id, d.monitoring_id])
  const [pick, setPick] = useState<'inventory' | 'monitoring' | null>(null)
  const link = async (which: 'inventory' | 'monitoring', id: number, name: string) => {
    try { await api.put(`${V1}/devices/${d.id}/${which}`, { id, name }); setPick(null); onChanged(); reload() } catch (e) { toast((e as Error).message, 'err') }
  }
  const inv = data?.inventory, mon = data?.monitoring
  const off = (s?: { connected: boolean; detail: string }) => !s?.connected
  return (
    <Card title="In your other apps">
      {!data ? <Loading /> : <div className="stack">
        <div className="linked">
          <span className="list-ico"><Icon name="package" size={16} /></span>
          <div className="grow min0">
            <b>Pocket Inventory</b>
            {inv?.item ? <div className="small">
              <div>{inv.item.name}{inv.item.brand || inv.item.model ? <span className="muted"> · {[inv.item.brand, inv.item.model].filter(Boolean).join(' ')}</span> : null}</div>
              <div className="muted">{[inv.item.purchase_date && `bought ${fmtDate(inv.item.purchase_date)}`, inv.item.expiry_date && `warranty until ${fmtDate(inv.item.expiry_date)}`, inv.item.location_name, inv.item.serial && `serial ${inv.item.serial}`].filter(Boolean).join(' · ')}</div>
            </div> : d.inventory_id ? <div className="small bad">{d.inventory_name || 'The linked item'}: {inv?.error || 'not reachable'}</div>
              : <div className="muted small">{off(inv) ? `Not connected${inv?.detail ? ` (${inv.detail})` : ''}. Connect it in Settings → Connected apps.` : 'Point this device to its item: when it was bought, the warranty, where it lives.'}</div>}
          </div>
          <div className="row">
            {inv?.open_url && <a className="btn sm ghost" href={inv.open_url} target="_blank" rel="noreferrer">Open</a>}
            {d.inventory_id ? <button type="button" className="btn sm ghost" onClick={() => link('inventory', 0, '')}>Unlink</button>
              : <button type="button" className="btn sm" disabled={off(inv)} title={off(inv) ? 'Needs Pocket Inventory (Settings → Connected apps)' : undefined} onClick={() => setPick('inventory')}>Link an item</button>}
          </div>
        </div>
        <div className="linked">
          <span className="list-ico"><Icon name="gauge" size={16} /></span>
          <div className="grow min0">
            <b>Pocket Monitoring</b>
            {mon?.server ? <div className="small">
              <div>{mon.server.name} <span className={`badge ${mon.server.status === 'up' || mon.server.status === 'ok' ? 'ok' : mon.server.status === 'down' ? 'bad' : ''}`}>{mon.server.status}</span></div>
              {mon.server.summary && <div className="muted">CPU {Math.round(mon.server.summary.cpu)}% · memory {Math.round(mon.server.summary.mem_pct)}%{mon.server.summary.temp ? ` · ${Math.round(mon.server.summary.temp)} °C` : ''}{mon.server.summary.fs_pct ? ` · disk ${Math.round(mon.server.summary.fs_pct)}%` : ''}</div>}
            </div> : d.monitoring_id ? <div className="small bad">{d.monitoring_name || 'The linked server'}: {mon?.error || 'not reachable'}</div>
              : <div className="muted small">{off(mon) ? `Not connected${mon?.detail ? ` (${mon.detail})` : ''}. Connect it in Settings → Connected apps.` : 'For a server or NAS: its health here, and a jump to its page.'}</div>}
          </div>
          <div className="row">
            {mon?.open_url && <a className="btn sm ghost" href={mon.open_url} target="_blank" rel="noreferrer">Open</a>}
            {d.monitoring_id ? <button type="button" className="btn sm ghost" onClick={() => link('monitoring', 0, '')}>Unlink</button>
              : <button type="button" className="btn sm" disabled={off(mon)} title={off(mon) ? 'Needs Pocket Monitoring (Settings → Connected apps)' : undefined} onClick={() => setPick('monitoring')}>Link a server</button>}
          </div>
        </div>
        {off(inv) && off(mon) && <CoreHint />}
      </div>}
      {pick === 'inventory' && <ItemPicker d={d} onClose={() => setPick(null)} onPick={it => link('inventory', it.id, it.name)} />}
      {pick === 'monitoring' && <ServerPicker d={d} onClose={() => setPick(null)} onPick={sv => link('monitoring', sv.id, sv.name)} />}
    </Card>
  )
}

function ItemPicker({ d, onClose, onPick }: { d: Device; onClose: () => void; onPick: (it: InventoryItem) => void }) {
  const [q, setQ] = useState(d.name || d.model || '')
  const dq = useDebounce(q, 300)
  const { data } = useAsync(() => api.get<{ ok: boolean; error?: string; items: InventoryItem[] }>(`${V1}/inventory/items?q=${encodeURIComponent(dq)}`), [dq])
  return (
    <Modal title="Link an item from Pocket Inventory" onClose={onClose}>
      <input autoFocus value={q} onChange={e => setQ(e.target.value)} placeholder="Search your items" />
      <div className="list mt picker">
        {!data ? <Loading /> : !data.ok ? <p className="bad small">{data.error}</p> : data.items.length === 0 ? <p className="muted small">No item matches.</p>
          : data.items.map(it => <button key={it.id} type="button" className="list-row pick-row" onClick={() => onPick(it)}><span className="grow"><b>{it.name}</b><span className="muted small block">{[it.brand, it.model, it.location_name, it.purchase_date && `bought ${fmtDate(it.purchase_date)}`].filter(Boolean).join(' · ')}</span></span><Icon name="chevron-right" size={14} /></button>)}
      </div>
    </Modal>
  )
}

function ServerPicker({ d, onClose, onPick }: { d: Device; onClose: () => void; onPick: (s: MonitoringServer) => void }) {
  const { data } = useAsync(() => api.get<{ ok: boolean; error?: string; servers: MonitoringServer[] }>(`${V1}/monitoring/servers`), [])
  const near = (s: MonitoringServer) => [s.name, s.summary?.hostname].some(n => n && [d.name, d.hostname].some(m => m && m.toLowerCase().includes(n.toLowerCase().split(/[ .]/)[0])))
  const list = data?.servers ? [...data.servers].sort((a, b) => Number(near(b)) - Number(near(a))) : []
  return (
    <Modal title="Link a server from Pocket Monitoring" onClose={onClose}>
      <div className="list picker">
        {!data ? <Loading /> : !data.ok ? <p className="bad small">{data.error}</p> : list.length === 0 ? <p className="muted small">No servers there yet.</p>
          : list.map(s => <button key={s.id} type="button" className="list-row pick-row" onClick={() => onPick(s)}><span className="grow"><b>{s.name}</b>{near(s) && <span className="badge accent">same name</span>}<span className="muted small block">{[s.summary?.hostname, s.summary?.platform, s.status].filter(Boolean).join(' · ')}</span></span><Icon name="chevron-right" size={14} /></button>)}
      </div>
    </Modal>
  )
}

function ServicesCard({ d, last, onChecked }: { d: Device; last: ServiceCheck | null; onChecked: () => void }) {
  const { toast } = useToast()
  const [busy, setBusy] = useState(false)
  const private_ = /^(10\.|192\.168\.|172\.(1[6-9]|2\d|3[01])\.|100\.)/.test(d.last_ip)
  const check = async () => {
    setBusy(true)
    try { const r = await api.post<{ ok: boolean; error?: string }>(`${V1}/devices/${d.id}/services`); if (!r.ok) toast(r.error || 'Failed', 'err'); onChecked() } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <Card title="What it answers on" sub="Common services, checked by knocking once on each (nothing more)." actions={private_ ? <button type="button" className="btn sm" onClick={check} disabled={busy}>{busy ? 'Checking…' : last ? 'Check again' : 'Check now'}</button> : undefined}>
      {!private_ ? <p className="muted small">It has no address on your own network to check.</p> : !last ? <p className="muted small">Not checked yet. It takes a few seconds.</p> : last.services.length === 0 ? <p className="muted small">Nothing answered on {last.ip} ({ago(last.checked_at)}). That is normal for phones and smart plugs.</p> : <>
        <div className="list">
          {last.services.map(s => <div key={s.port} className="list-row svc-row"><span className="mono svc-port">{s.port}</span><span className="grow">{s.name}{s.notes && <span className="muted small"> · {s.notes}</span>}</span>
            {s.web && <a className="btn sm ghost" href={`${s.web}://${last.ip}${(s.web === 'http' && s.port === 80) || (s.web === 'https' && s.port === 443) ? '' : ':' + s.port}`} target="_blank" rel="noreferrer">Open</a>}</div>)}
        </div>
        <p className="muted small mt">On {last.ip}, {fmtDateTime(last.checked_at)}.</p>
      </>}
    </Card>
  )
}

function MergeModal({ d, onClose, onDone }: { d: Device; onClose: () => void; onDone: () => void }) {
  const { toast } = useToast()
  const { data } = useAsync(() => api.get<Device[]>(`${V1}/devices`), [])
  const [q, setQ] = useState('')
  const [sel, setSel] = useState<number[]>([])
  const list = (data || []).filter(o => o.id !== d.id && matches(o, q))
  const go = async () => {
    try { await api.post(`${V1}/devices/${d.id}/merge`, { ids: sel }); toast('Joined into one device'); onDone() } catch (e) { toast((e as Error).message, 'err') }
  }
  return (
    <Modal title={`Join with ${label(d)}`} onClose={onClose} footer={<><button type="button" className="btn ghost" onClick={onClose}>Cancel</button><button type="button" className="btn primary" disabled={!sel.length} onClick={go}>Join {sel.length || ''}</button></>}>
      <p className="muted small">Pick devices that are really this one (the same phone under an older private address, say). Their addresses and history move here; the others disappear.</p>
      <input value={q} onChange={e => setQ(e.target.value)} placeholder="Search" className="mt" />
      <div className="list mt picker">
        {list.slice(0, 60).map(o => <label key={o.id} className="list-row pick-row check-row"><input type="checkbox" checked={sel.includes(o.id)} onChange={e => setSel(e.target.checked ? [...sel, o.id] : sel.filter(x => x !== o.id))} />
          <DeviceIcon d={o} size={26} /><span className="grow min0"><span className="dev-name">{label(o)}</span><span className="muted small block ellipsis">{[o.last_ip, o.last_mac, o.last_seen && `seen ${ago(o.last_seen)}`].filter(Boolean).join(' · ')}</span></span></label>)}
      </div>
    </Modal>
  )
}

interface Identified { ok: boolean; error?: string; kind?: string; name?: string; confidence?: string; why?: string; check?: string }

export function IdentifyModal({ d, onClose, onApply }: { d: Device; onClose: () => void; onApply: (p: Partial<Device>) => void }) {
  const { data, error } = useAsync(() => api.post<Identified>(`${V1}/devices/${d.id}/identify`), [d.id])
  return (
    <Modal title="What could this be?" onClose={onClose} footer={data?.ok && (data.name || data.kind) ? <><button type="button" className="btn ghost" onClick={onClose}>Close</button>
      <button type="button" className="btn primary" onClick={() => onApply({ ...(data.name && !d.name ? { name: data.name } : {}), ...(data.kind && !d.kind ? { kind: data.kind } : {}) })}>Use this</button></> : undefined}>
      {error ? <p className="bad">{error}</p> : !data ? <Loading label="Asking…" /> : !data.ok ? <p className="bad">{data.error}</p> : (
        <div className="stack">
          {data.name && <p><b>{data.name}</b>{data.kind && <span className="muted"> · {kindName(data.kind)}</span>}{data.confidence && <span className="badge">{data.confidence} confidence</span>}</p>}
          {data.why && <p>{data.why}</p>}
          {data.check && <p className="muted small"><Icon name="info" size={12} /> {data.check}</p>}
          <p className="muted small">A guess from what the network sees; only you can know for sure.</p>
        </div>
      )}
    </Modal>
  )
}

