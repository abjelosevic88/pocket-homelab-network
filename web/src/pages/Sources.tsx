// Sources: where devices are seen (a scan here, agents elsewhere, Tailscale, AdGuard Home, a router), and the NetAlertX import.
import React, { useEffect, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { api, upload, V1 } from '../lib/api'
import type { ScanRun, Source, SourceCfg, SourceKind, SourcesData } from '../lib/types'
import { ago, fmtInterval, SOURCE_HELP, SOURCE_ICON, sourceLine } from '../lib/network'
import { fmtDateTime } from '../lib/format'
import { Card, Confirm, CopyField, CoreHint, Field, Loading, Menu, Modal, Segmented, Switch, rectOf, useAsync, useToast, type Anchor } from '../components/ui'
import { Icon } from '../components/icons'
import { Select } from '../components/Select'
import { announceChange, useOnChange } from '../components/Layout'

const INTERVALS = [60, 120, 300, 600, 900, 1800, 3600, 21600].map(s => ({ value: String(s), label: fmtInterval(s).replace(/^every /, 'Every ') }))

export default function Sources() {
  const [sp, setSp] = useSearchParams()
  const { toast } = useToast()
  const { data, reload } = useAsync(() => api.get<SourcesData>(`${V1}/sources`), [])
  const [edit, setEdit] = useState<Source | 'new' | null>(null)
  const [token, setToken] = useState<{ source: Source; token: string } | null>(null)
  const importRef = useRef<HTMLDivElement>(null)
  useOnChange(reload)
  useEffect(() => {
    if (sp.get('add')) { setEdit('new'); setSp(new URLSearchParams(), { replace: true }) }
    if (sp.get('import')) { setTimeout(() => importRef.current?.scrollIntoView({ behavior: 'smooth', block: 'center' }), 200); setSp(new URLSearchParams(), { replace: true }) }
  }, [sp]) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => { const running = data?.sources.some(s => s.running); if (!running) return; const t = setTimeout(reload, 3000); return () => clearTimeout(t) }, [data, reload])
  if (!data) return <Loading />
  const run = async (s: Source) => {
    try {
      const r = await api.post<{ ok: boolean; found?: number; new?: number; error?: string }>(`${V1}/sources/${s.id}/run`)
      toast(r.ok ? `${s.name}: ${r.found} found${r.new ? `, ${r.new} new` : ''}` : `${s.name}: ${r.error}`, r.ok ? undefined : 'err')
      reload(); announceChange()
    } catch (e) { toast((e as Error).message, 'err') }
  }
  const toggle = async (s: Source, on: boolean) => { try { await api.put(`${V1}/sources/${s.id}`, { enabled: on }); reload() } catch (e) { toast((e as Error).message, 'err') } }
  return (
    <div className="stack loose">
      <div className="page-header">
        <div className="grow"><h1>Sources</h1><div className="sub">Where Pocket Network sees devices. More sources see more: a scan only sees the network this server is on.</div></div>
        <div className="actions"><button type="button" className="btn primary" onClick={() => setEdit('new')}><Icon name="plus" size={15} />Add a source</button></div>
      </div>
      {data.sources.length === 0 && <div className="callout info"><Icon name="info" size={15} /><div>No sources yet. Start with a scan of this server’s network{data.can.local_scan ? '' : ' (an admin can add it)'}, then Tailscale for phones and laptops, and an agent for a network behind its own router.</div></div>}
      <div className="stack">
        {data.sources.map(s => <SourceCard key={s.id} s={s} onRun={() => run(s)} onToggle={v => toggle(s, v)} onEdit={() => setEdit(s)} onChanged={reload} onToken={(t) => setToken({ source: s, token: t })} />)}
      </div>
      <div ref={importRef}><NetAlertXCard onDone={() => { reload(); announceChange() }} /></div>
      {edit && <SourceModal data={data} source={edit === 'new' ? null : edit} onClose={() => setEdit(null)} onSaved={(s, t) => { setEdit(null); reload(); announceChange(); if (t) setToken({ source: s, token: t }) }} />}
      {token && <AgentModal source={token.source} token={token.token} base={data.base_url || window.location.origin} onClose={() => setToken(null)} />}
    </div>
  )
}

function SourceCard({ s, onRun, onToggle, onEdit, onChanged, onToken }: { s: Source; onRun: () => void; onToggle: (v: boolean) => void; onEdit: () => void; onChanged: () => void; onToken: (t: string) => void }) {
  const { toast } = useToast()
  const [menu, setMenu] = useState<Anchor | null>(null)
  const [del, setDel] = useState(false)
  const [runs, setRuns] = useState<ScanRun[] | null>(null)
  const detail = s.kind === 'scan' ? [s.cfg.interface, s.cfg.subnet || 'its own network', s.cfg.names === false ? 'no names' : ''].filter(Boolean).join(' · ')
    : s.kind === 'adguard' ? (s.cfg.via_core ? 'through Pocket Core' : s.cfg.url) : s.kind === 'openwrt' ? s.cfg.url : s.kind === 'tailscale' ? (s.has_secret ? 'Tailscale API' : 'this server’s Tailscale') : s.token_hint ? `token ${s.token_hint}…` : ''
  const newToken = async () => { try { const r = await api.post<{ token: string }>(`${V1}/sources/${s.id}/token`); onToken(r.token); onChanged() } catch (e) { toast((e as Error).message, 'err') } }
  return (
    <Card className={`src-card ${s.enabled ? '' : 'off'}`}>
      <div className="src-head">
        <span className="list-ico lg"><Icon name={SOURCE_ICON[s.kind] || 'layers'} size={18} /></span>
        <div className="grow min0">
          <h3 className="ellipsis">{s.name}</h3>
          <div className="muted small ellipsis">{s.kind_name}{s.network && s.network !== s.kind_name ? ` · ${s.network}` : ''}{detail ? ` · ${detail}` : ''}</div>
          <div className={`small src-line ${s.status}`}><span className={`src-dot ${s.status}`} />{sourceLine(s)}</div>
        </div>
        <div className="row src-actions">
          {s.kind !== 'agent' && <button type="button" className="btn sm" onClick={onRun} disabled={s.running || !s.enabled}>{s.running ? 'Running…' : 'Run now'}</button>}
          <Switch checked={s.enabled} onChange={onToggle} label={s.enabled ? 'Pause' : 'Resume'} size="sm" />
          <button type="button" className="btn ghost icon-only sm" aria-label="More" onClick={e => setMenu(menu ? null : rectOf(e.currentTarget))}><Icon name="more" size={16} /></button>
        </div>
      </div>
      <div className="muted small src-foot">
        <span>{fmtInterval(s.interval_s)}{s.kind === 'agent' ? ' (the agent decides when)' : ''}</span>
        {s.last_ok && <span>last worked {ago(s.last_ok)}</span>}
        {!s.presence && <span>names only (does not count as online)</span>}
        <button type="button" className="link-btn" onClick={async () => setRuns(runs ? null : await api.get<ScanRun[]>(`${V1}/sources/${s.id}/scans?limit=12`))}>{runs ? 'Hide runs' : 'Recent runs'}</button>
      </div>
      {runs && <table className="table compact mt"><thead><tr><th>When</th><th>Found</th><th>New</th><th>Took</th><th /></tr></thead><tbody>
        {runs.length === 0 ? <tr><td colSpan={5} className="muted">No runs yet.</td></tr> : runs.map(r => <tr key={r.id}><td>{fmtDateTime(r.at)}</td><td className="tnum">{r.found}</td><td className="tnum">{r.new || ''}</td><td className="tnum muted">{r.ms ? `${(r.ms / 1000).toFixed(1)} s` : ''}</td><td className="bad small">{r.error}</td></tr>)}
      </tbody></table>}
      {menu && <Menu anchor={menu} align="right" onClose={() => setMenu(null)} items={[
        { label: 'Edit', icon: 'edit', onSelect: onEdit },
        ...(s.kind === 'agent' ? [{ label: 'New agent token…', icon: 'key', onSelect: newToken }] : []),
        'sep', { label: 'Remove', icon: 'trash', danger: true, onSelect: () => setDel(true) },
      ]} />}
      {del && <Confirm title={`Remove ${s.name}?`} confirmLabel="Remove" message="The devices it found stay in the list; it just stops looking." onCancel={() => setDel(false)}
        onConfirm={async () => { setDel(false); await api.del(`${V1}/sources/${s.id}`); onChanged() }} />}
    </Card>
  )
}

type Form = { kind: SourceKind; name: string; network: string; cfg: SourceCfg; secret: string; interval_s: number; presence: boolean }

function SourceModal({ data, source, onClose, onSaved }: { data: SourcesData; source: Source | null; onClose: () => void; onSaved: (s: Source, token?: string) => void }) {
  const { toast } = useToast()
  const [f, setF] = useState<Form | null>(source ? { kind: source.kind, name: source.name, network: source.network, cfg: source.cfg, secret: source.has_secret ? '••••••••' : '', interval_s: source.interval_s, presence: source.presence } : null)
  const [busy, setBusy] = useState(false)
  const can = data.can
  const pick = (kind: SourceKind) => {
    const iface = can.interfaces[0]
    setF({
      kind, name: '', network: kind === 'scan' ? (iface?.subnet || '') : kind === 'tailscale' ? 'Tailscale' : '', secret: '', presence: true,
      interval_s: kind === 'tailscale' ? 120 : 300,
      cfg: kind === 'scan' ? { interface: iface?.name || '', subnet: '', names: true } : kind === 'tailscale' ? (can.tailscale_local ? {} : { tailnet: '-' }) : kind === 'openwrt' ? { username: 'root' } : kind === 'adguard' && can.core ? { via_core: true } : {},
    })
  }
  const kinds: { kind: SourceKind; disabled?: string }[] = [
    { kind: 'scan', disabled: can.local_scan ? undefined : 'Only admins can scan this server’s own networks' },
    { kind: 'tailscale' }, { kind: 'adguard' }, { kind: 'openwrt' }, { kind: 'agent' },
  ]
  if (!f) return (
    <Modal title="Add a source" onClose={onClose} size="lg">
      <div className="kind-grid">
        {kinds.map(k => (
          <button key={k.kind} type="button" className="kind-tile" disabled={!!k.disabled} title={k.disabled} onClick={() => pick(k.kind)}>
            <span className="list-ico lg"><Icon name={SOURCE_ICON[k.kind]} size={18} /></span>
            <span className="grow"><b>{data.kinds[k.kind]}</b><span className="muted small block">{k.disabled || SOURCE_HELP[k.kind]}</span></span>
          </button>
        ))}
      </div>
    </Modal>
  )
  const cfg = (p: Partial<SourceCfg>) => setF({ ...f, cfg: { ...f.cfg, ...p } })
  const save = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    const body = { ...f, secret: f.secret.includes('••') ? undefined : f.secret }
    try {
      if (source) { onSaved(await api.put<Source>(`${V1}/sources/${source.id}`, body)) }
      else { const r = await api.post<{ source: Source; token: string }>(`${V1}/sources`, body); onSaved(r.source, r.token || undefined) }
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  const tsLocal = f.kind === 'tailscale' && !f.secret && can.tailscale_local
  return (
    <Modal title={source ? `Edit ${source.name}` : data.kinds[f.kind]} onClose={onClose} footer={<><button type="button" className="btn ghost" onClick={onClose}>Cancel</button><button className="btn primary" form="src-form" disabled={busy}>{source ? 'Save' : f.kind === 'agent' ? 'Create the agent' : 'Add and run'}</button></>}>
      <form id="src-form" className="stack" onSubmit={save}>
        <p className="muted small">{SOURCE_HELP[f.kind]}</p>
        <div className="two-col tight">
          <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} placeholder={data.kinds[f.kind]} /></Field>
          <Field label="Network name" help="How its devices are labelled, e.g. “Home Wi-Fi”."><input value={f.network} onChange={e => setF({ ...f, network: e.target.value })} placeholder={f.kind === 'agent' ? 'Garden office' : 'Home network'} /></Field>
        </div>
        {f.kind === 'scan' && <>
          <div className="two-col tight">
            <Field group label="Interface"><Select ariaLabel="Interface" value={f.cfg.interface || ''} onChange={v => { const i = can.interfaces.find(x => x.name === v); setF({ ...f, cfg: { ...f.cfg, interface: v }, network: f.network || i?.subnet || '' }) }}
              options={[{ value: '', label: 'Automatic' }, ...can.interfaces.map(i => ({ value: i.name, label: `${i.name} — ${i.subnet}`, hint: `${i.hosts} addresses` }))]} /></Field>
            <Field label="Network to sweep (optional)" help="Default: the interface's own. At most a /22."><input value={f.cfg.subnet || ''} onChange={e => cfg({ subnet: e.target.value })} placeholder={can.interfaces.find(i => i.name === f.cfg.interface)?.subnet || '192.168.1.0/24'} /></Field>
          </div>
          <label className="check-row"><input type="checkbox" checked={f.cfg.names !== false} onChange={e => cfg({ names: e.target.checked })} />Ask each device its name (mDNS, NetBIOS, reverse DNS)</label>
          <Field label="DNS server for names (optional)" help="Your router or AdGuard Home often knows the names DHCP handed out."><input value={f.cfg.dns || ''} onChange={e => cfg({ dns: e.target.value })} placeholder="192.168.1.1" /></Field>
        </>}
        {f.kind === 'tailscale' && <>
          {can.tailscale_local && <div className="callout info"><Icon name="info" size={15} /><div>This server runs Tailscale: leave the key empty to read its peers directly.</div></div>}
          <div className="two-col tight">
            <Field label={`API key${can.tailscale_local ? ' (optional)' : ''}`} help="Tailscale admin console → Settings → Keys → API access token."><input value={f.secret} onChange={e => setF({ ...f, secret: e.target.value })} placeholder="tskey-api-…" autoComplete="off" /></Field>
            {!tsLocal && <Field label="Tailnet" help="“-” means the key’s own."><input value={f.cfg.tailnet || ''} onChange={e => cfg({ tailnet: e.target.value })} placeholder="-" /></Field>}
          </div>
        </>}
        {f.kind === 'adguard' && <>
          <Segmented size="sm" value={f.cfg.via_core ? 'core' : 'direct'} onChange={v => cfg({ via_core: v === 'core' })} options={[{ value: 'core', label: 'Pocket Core', disabled: !can.core, title: can.core ? 'Your AdGuard Home login from Pocket Core (My logins)' : 'Needs Pocket Core' }, { value: 'direct', label: 'Direct' }]} />
          {!can.core && <CoreHint />}
          {f.cfg.via_core ? <p className="muted small">Uses your own AdGuard Home login from Pocket Core → My logins.</p> : <>
            <Field label="Address"><input value={f.cfg.url || ''} onChange={e => cfg({ url: e.target.value })} placeholder="http://192.168.1.2:3000" /></Field>
            <div className="two-col tight">
              <Field label="User"><input value={f.cfg.username || ''} onChange={e => cfg({ username: e.target.value })} autoComplete="off" /></Field>
              <Field label="Password"><input type="password" value={f.secret} onChange={e => setF({ ...f, secret: e.target.value })} autoComplete="new-password" /></Field>
            </div>
          </>}
        </>}
        {f.kind === 'openwrt' && <>
          <Field label="Router address" help="LuCI must be installed (GL.iNet: Advanced → LuCI)."><input value={f.cfg.url || ''} onChange={e => cfg({ url: e.target.value })} placeholder="http://192.168.8.1" /></Field>
          <div className="two-col tight">
            <Field label="User"><input value={f.cfg.username || ''} onChange={e => cfg({ username: e.target.value })} placeholder="root" autoComplete="off" /></Field>
            <Field label="Password"><input type="password" value={f.secret} onChange={e => setF({ ...f, secret: e.target.value })} autoComplete="new-password" /></Field>
          </div>
        </>}
        {f.kind === 'agent' && <p className="small">After saving you get a token and the command to run on a computer or router on that network. It sweeps that network and reports here; nothing has to reach it from outside.</p>}
        <div className="two-col tight">
          {f.kind !== 'agent' && <Field group label="How often"><Select ariaLabel="How often" value={String(f.interval_s)} onChange={v => setF({ ...f, interval_s: Number(v) })} options={INTERVALS} /></Field>}
          {f.kind === 'agent' && <Field group label="How often the agent reports"><Select ariaLabel="How often" value={String(f.interval_s)} onChange={v => setF({ ...f, interval_s: Number(v) })} options={INTERVALS} /></Field>}
        </div>
        {f.kind === 'adguard' && <label className="check-row"><input type="checkbox" checked={f.presence} onChange={e => setF({ ...f, presence: e.target.checked })} />A DNS lookup in the last ten minutes counts as online</label>}
      </form>
    </Modal>
  )
}

function AgentModal({ source, token, base, onClose }: { source: Source; token: string; base: string; onClose: () => void }) {
  const [how, setHow] = useState<'binary' | 'docker'>('binary')
  const cmd = `pocket-network agent -server ${base} -token ${token}`
  const docker = `docker run -d --name pocket-network-agent --restart unless-stopped --network host --cap-add NET_RAW \\\n  ghcr.io/abjelosevic88/pocket-homelab-network:latest agent -server ${base} -token ${token}`
  return (
    <Modal title={`Set up the agent for ${source.name}`} onClose={onClose} size="lg" footer={<button type="button" className="btn primary" onClick={onClose}>Done</button>}>
      <div className="stack">
        <div className="set-fresh"><div className="row"><Icon name="key" size={16} /><b>The token is shown once.</b></div><CopyField value={token} /></div>
        <p className="small">Run this on a computer or router on that network. It needs to reach <code>{base}</code> (over Tailscale is fine) and, for the best results, permission to send raw network packets (run as root, or give it <code>CAP_NET_RAW</code>).</p>
        <Segmented size="sm" value={how} onChange={v => setHow(v as 'binary' | 'docker')} options={[{ value: 'binary', label: 'The program' }, { value: 'docker', label: 'Docker' }]} />
        {how === 'binary' ? <>
          <p className="muted small">Download <code>pocket-network</code> for that machine (Linux, macOS, Windows; ARM and MIPS for routers) from the <a href="https://github.com/abjelosevic88/pocket-homelab-network/releases/latest" target="_blank" rel="noreferrer">latest release</a>, then:</p>
          <CopyField value={cmd} />
        </> : <CopyField value={docker} />}
        <p className="muted small">It reports every {Math.round(source.interval_s / 60)} minutes. On an OpenWrt or GL.iNet router it also reads the DHCP names. Lost the token? Sources → ⋯ → New agent token.</p>
      </div>
    </Modal>
  )
}

function NetAlertXCard({ onDone }: { onDone: () => void }) {
  const { toast } = useToast()
  const [busy, setBusy] = useState(false)
  const [res, setRes] = useState<Record<string, number> | null>(null)
  const go = async (file: File) => {
    setBusy(true)
    try { const r = await upload<Record<string, number>>(`${V1}/import/netalertx`, 'file', file); setRes(r); toast(`Imported: ${r.created} new, ${r.updated} updated`); onDone() } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <Card title="Move over from NetAlertX" sub="Bring its devices with their names, owners, types, notes and alert settings.">
      <ol className="steps small">
        <li>In NetAlertX: <b>Maintenance → Backup / Restore → CSV Export</b>. Or take <code>config/devices.csv</code> from its data folder (the CSV backup plugin writes it).</li>
        <li>Upload it here. What you named there arrives named and known; the rest waits in Review, with look-alikes grouped.</li>
        <li>Importing the same file again only fills in what is still empty.</li>
      </ol>
      <div className="row wrap mt">
        <label className="btn"><Icon name="upload" size={15} />{busy ? 'Importing…' : 'Upload devices.csv…'}<input type="file" accept=".csv,text/csv" hidden onChange={e => e.target.files?.[0] && go(e.target.files[0])} /></label>
        {res && <span className="small ok">{res.created} new · {res.updated} updated · {res.skipped} skipped</span>}
      </div>
    </Card>
  )
}
