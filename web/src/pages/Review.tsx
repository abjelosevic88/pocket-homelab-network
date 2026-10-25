// Review: the devices nobody has looked at yet. Look-alikes (one phone under several private
// addresses) are grouped so they can be joined first; then one card per device: is this yours?
import { useState } from 'react'
import { Link } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import type { Device, DeviceMeta, Review as ReviewT, ReviewData, ReviewGroup } from '../lib/types'
import { ago, kindName, kindOf, label } from '../lib/network'
import { Card, Confirm, Empty, Field, Loading, useAsync, useToast } from '../components/ui'
import { Icon } from '../components/icons'
import { DeviceIcon, KindSelect, OwnerSelect, PrivateHint } from '../components/DeviceForm'
import { announceChange, useOnChange } from '../components/Layout'
import { IdentifyModal } from './Devices'

export default function Review() {
  const { data, reload, error } = useAsync(() => api.get<ReviewData>(`${V1}/review`), [])
  const { data: ai } = useAsync(() => api.get<{ ready: boolean }>(`${V1}/ai/status`), [])
  const { toast } = useToast()
  const [all, setAll] = useState(false)
  useOnChange(reload)
  if (error) return <div className="callout danger"><Icon name="alert" size={15} />{error}</div>
  if (!data) return <Loading />
  const done = () => { reload(); announceChange() }
  const meta: DeviceMeta = { owners: data.owners, locations: data.locations, kinds: [] }
  const allIDs = [...data.groups.flatMap(g => g.devices), ...data.devices].map(d => d.id)
  return (
    <div className="stack loose">
      <div className="page-header">
        <div className="grow"><h1>Review new devices</h1><div className="sub">{data.total ? `${data.total} ${data.total === 1 ? 'device' : 'devices'} nobody has looked at yet. Say what each one is; nothing changes on the network itself.` : 'Nothing waiting.'}</div></div>
        {data.total > 1 && <div className="actions"><button type="button" className="btn" onClick={() => setAll(true)}><Icon name="check" size={15} />All are ours</button></div>}
      </div>
      {data.total === 0 && <Empty title="All reviewed" icon="check-circle">New devices show up here the moment a source sees them, and you get an alert if you want one (Settings → Alerts).<div className="mt"><Link to="/devices" className="btn">See all devices</Link></div></Empty>}
      {data.groups.map(g => <GroupCard key={g.key} g={g} onDone={done} />)}
      <div className="review-grid">
        {data.devices.map(d => <ReviewCard key={d.id} d={d} meta={meta} ai={!!ai?.ready} onDone={done} />)}
      </div>
      {all && <Confirm danger={false} title={`Mark all ${data.total} as yours?`} confirmLabel="Mark as ours" message="They stop showing up here. You can still name them one by one later."
        onCancel={() => setAll(false)} onConfirm={async () => { setAll(false); try { await api.post(`${V1}/devices/review`, { ids: allIDs, review: 'known' }); toast('Marked as yours'); done() } catch (e) { toast((e as Error).message, 'err') } }} />}
    </div>
  )
}

function GroupCard({ g, onDone }: { g: ReviewGroup; onDone: () => void }) {
  const { toast } = useToast()
  const target = g.known || [...g.devices].sort((a, b) => (b.last_seen > a.last_seen ? 1 : -1))[0]
  const others = g.devices.filter(d => d.id !== target.id)
  const join = async () => {
    try { await api.post(`${V1}/devices/${target.id}/merge`, { ids: others.map(d => d.id) }); toast(`Joined into ${label(target)}`); onDone() } catch (e) { toast((e as Error).message, 'err') }
  }
  return (
    <Card className="group-card" title={<><Icon name="merge" size={16} /> {g.devices.length + (g.known ? 1 : 0)} look like one device: “{g.name}”</>}
      sub={g.known ? `It is already here as ${label(g.known)}, and showed up again under new private addresses.` : 'Phones, watches and laptops make up a new private address now and then. Join them so it is one device with one history.'}
      actions={<button type="button" className="btn primary sm" onClick={join}><Icon name="merge" size={14} />{g.known ? `Join to ${label(g.known)}` : 'Join into one'}</button>}>
      <div className="list">
        {g.devices.map(d => (
          <Link key={d.id} to={`/devices/${d.id}`} className="list-row">
            <DeviceIcon d={d} size={28} />
            <span className="grow min0"><span className="mono small">{d.last_mac}</span><span className="muted small block">{d.last_ip} · first seen {ago(d.first_seen)}{d.last_seen ? ` · last seen ${ago(d.last_seen)}` : ''}</span></span>
          </Link>
        ))}
      </div>
    </Card>
  )
}

function ReviewCard({ d, meta, ai, onDone }: { d: Device; meta: DeviceMeta; ai: boolean; onDone: () => void }) {
  const { toast } = useToast()
  const [f, setF] = useState({ name: d.name, kind: d.kind, owner: d.owner })
  const [ask, setAsk] = useState(false)
  const [busy, setBusy] = useState(false)
  const decide = async (review: ReviewT, extra: Partial<Device> = {}) => {
    setBusy(true)
    try {
      await api.put(`${V1}/devices/${d.id}`, { ...f, review, ...extra })
      toast(review === 'known' ? `${f.name || label(d)} is yours` : review === 'guest' ? (extra.muted ? 'Ignored' : 'Marked as a guest’s') : 'Saved')
      onDone()
    } catch (e) { toast((e as Error).message, 'err'); setBusy(false) }
  }
  const facts = [
    d.online ? 'online now' : d.last_seen ? `last seen ${ago(d.last_seen)}` : '',
    `first seen ${ago(d.first_seen)}`,
    d.last_ip, d.last_network,
  ].filter(Boolean).join(' · ')
  return (
    <Card className="review-card">
      <div className="rc-head">
        <DeviceIcon d={d} size={40} />
        <div className="grow min0">
          <h3 className="ellipsis">{label(d)}</h3>
          <div className="muted small">{d.random_mac ? <PrivateHint /> : d.vendor ? `Made by ${d.vendor}` : 'Maker unknown'}{kindOf(d) && <> · looks like a {kindName(kindOf(d)).toLowerCase()}</>}</div>
          <div className="muted small">{facts}</div>
        </div>
        <Link to={`/devices/${d.id}`} className="btn ghost icon-only" aria-label="Open the device page" title="Open the device page"><Icon name="chevron-right" size={16} /></Link>
      </div>
      <div className="stack tight-stack">
        <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} placeholder={d.hostname || 'What is it?'} /></Field>
        <div className="two-col tight">
          <Field group label="Kind"><KindSelect value={f.kind} guess={d.kind_guess} onChange={v => setF({ ...f, kind: v })} /></Field>
          <Field group label="Owner"><OwnerSelect value={f.owner} onChange={v => setF({ ...f, owner: v })} meta={meta} /></Field>
        </div>
      </div>
      <div className="row wrap rc-actions">
        <button type="button" className="btn primary sm" disabled={busy} onClick={() => decide('known')}><Icon name="check" size={14} />Yes, it’s ours</button>
        <button type="button" className="btn sm" disabled={busy} onClick={() => decide('guest')}>A guest’s</button>
        <button type="button" className="btn sm ghost" disabled={busy} onClick={() => decide('guest', { muted: true })} title="No alerts about it; forgotten when it has not been seen for a while">Ignore</button>
        {ai && <button type="button" className="btn sm ghost" onClick={() => setAsk(true)} title="Ask the AI what it could be"><Icon name="sparkles" size={14} /></button>}
      </div>
      {ask && <IdentifyModal d={d} onClose={() => setAsk(false)} onApply={p => { setAsk(false); setF({ name: p.name ?? f.name, kind: p.kind ?? f.kind, owner: f.owner }) }} />}
    </Card>
  )
}
