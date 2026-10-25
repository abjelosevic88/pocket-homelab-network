import { AskButton } from '../components/AskButton'
import React, { useEffect, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { APIToken, ConnMode, Settings as SettingsT, User } from '../lib/types'
import { DATE_FORMATS, TIME_FORMATS, fmtBytes, fmtDateTime, type DateFormat, type TimeFormat } from '../lib/format'
import { Avatar, Confirm, CoreHint, Field, Loading, Modal, Segmented, SettingRow, Switch, Tabs, useAsync, useIsMobile, useToast } from '../components/ui'
import { Icon } from '../components/icons'
import { Select } from '../components/Select'
import { ShortcutsSheet } from '../components/ShortcutsSheet'
import { PocketModelSelect } from '../components/PocketModelSelect'

type TabId = 'general' | 'alerts' | 'apps' | 'ai' | 'profile' | 'people' | 'tokens' | 'data' | 'about'

const TABS: { id: TabId; label: string; icon: string; sub: string }[] = [
  { id: 'general', label: 'Devices & history', icon: 'sliders', sub: 'When a device counts as offline, recognising phones that change their address, the household, and how long history is kept.' },
  { id: 'alerts', label: 'Alerts', icon: 'bell', sub: 'What you hear about — new devices, watched devices going offline and coming back — where it goes, and quiet hours.' },
  { id: 'apps', label: 'Connected apps', icon: 'layers', sub: 'Pocket Inventory (the item a device is) and Pocket Monitoring (the health of a server or NAS).' },
  { id: 'ai', label: 'AI', icon: 'sparkles', sub: 'Who answers “what could this device be?” and questions about your network: Pocket AI through Core, or your own OpenAI key.' },
  { id: 'profile', label: 'Profile', icon: 'user', sub: 'Your name, email and password.' },
  { id: 'people', label: 'People', icon: 'users', sub: 'Everyone who has an account on this box.' },
  { id: 'tokens', label: 'API & shortcuts', icon: 'key', sub: 'Personal tokens for Homepage widgets, Shortcuts and scripts.' },
  { id: 'data', label: 'Backup & data', icon: 'database', sub: 'Download the database, export or import your devices, move over from NetAlertX.' },
  { id: 'about', label: 'About', icon: 'info', sub: 'Version, numbers and the server.' },
]

export default function Settings() {
  const { tab: raw } = useParams()
  const navigate = useNavigate()
  const mobile = useIsMobile()
  const tab: TabId = TABS.find(t => t.id === raw)?.id || 'general'
  const current = TABS.find(t => t.id === tab)!
  const go = (id: TabId) => navigate(`/settings/${id}`, { replace: true })
  useEffect(() => { document.querySelector('.set-content')?.scrollTo({ top: 0 }) }, [tab])
  useEffect(() => {
    const h = (e: KeyboardEvent) => { if (e.key === 'Escape' && !document.querySelector('.modal-backdrop, .menu, .sel-pop')) navigate('/') }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [navigate])
  return (
    <div className="set-page">
      <header className="set-header">
        <button type="button" className="btn ghost icon-only" aria-label="Back" title="Back (esc)" onClick={() => navigate('/')}><Icon name="chevron-left" size={18} /></button>
        <img src="/favicon.svg" alt="" width={24} height={24} className="set-logo" />
        <h1>Settings</h1>
        <span style={{ flex: 1 }} />
        <AskButton />
      </header>
      {mobile && <div className="set-tabs-mobile"><Tabs tabs={TABS.map(t => ({ id: t.id, label: t.label, icon: t.icon }))} value={tab} onChange={go} /></div>}
      <div className="set-body">
        {!mobile && (
          <nav className="set-nav" aria-label="Settings sections">
            {TABS.map(t => (
              <button key={t.id} type="button" className={`set-nav-item ${t.id === tab ? 'active' : ''}`} aria-current={t.id === tab ? 'page' : undefined} onClick={() => go(t.id)}>
                <Icon name={t.icon} size={16} />{t.label}
              </button>
            ))}
          </nav>
        )}
        <main className="set-content">
          <div className="set-inner" key={tab}>
            <div className="set-title"><h2>{current.label}</h2><p>{current.sub}</p></div>
            {tab === 'general' && <General />}
            {tab === 'alerts' && <Alerts />}
            {tab === 'apps' && <Integrations />}
            {tab === 'ai' && <AITab />}
            {tab === 'profile' && <Profile />}
            {tab === 'people' && <Users />}
            {tab === 'tokens' && <Tokens />}
            {tab === 'data' && <Data />}
            {tab === 'about' && <About />}
          </div>
        </main>
      </div>
    </div>
  )
}

function Section({ title, sub, icon, actions, children }: { title: React.ReactNode; sub?: React.ReactNode; icon?: string; actions?: React.ReactNode; children?: React.ReactNode }) {
  return (
    <section className="set-section">
      <div className="set-section-head">
        {icon && <span className="set-app-ico"><Icon name={icon} size={17} /></span>}
        <div className="grow"><h3>{title}</h3>{sub && <p>{sub}</p>}</div>
        {actions && <div className="row">{actions}</div>}
      </div>
      {children && <div className="set-section-body">{children}</div>}
    </section>
  )
}

function useSettingsForm() {
  const { settings, setSettings, user } = useApp()
  const { toast } = useToast()
  const [f, setF] = useState<SettingsT | null>(settings)
  const [busy, setBusy] = useState(false)
  useEffect(() => { if (!f && settings) setF(settings) }, [settings]) // eslint-disable-line react-hooks/exhaustive-deps
  const save = async (patch?: Partial<SettingsT>) => {
    if (!f) return
    setBusy(true)
    try { const s = await api.put<SettingsT>(`${V1}/settings`, { ...f, ...patch }); setSettings(s); setF(s); toast('Saved') } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return { f, setF, save, busy, isAdmin: user?.role === 'admin' }
}

function useTester() {
  const [test, setTest] = useState<Record<string, string>>({})
  const run = async (which: string, url: string, key: string, extra: Record<string, string> = {}) => {
    setTest(t => ({ ...t, [which]: 'testing…' }))
    try { const r = await api.post<{ ok: boolean; message?: string; error?: string }>(`${V1}/integrations/test`, { which, url, key, ...extra }); setTest(t => ({ ...t, [which]: r.ok ? `✓ ${r.message}` : `✕ ${r.error}` })) } catch (e) { setTest(t => ({ ...t, [which]: `✕ ${(e as Error).message}` })) }
  }
  const status = (which: string) => <span className={`small ${test[which]?.startsWith('✓') ? 'ok' : test[which]?.startsWith('✕') ? 'bad' : 'muted'}`}>{test[which]}</span>
  return { run, status }
}

const MINUTES = [5, 10, 15, 20, 30, 45, 60, 120].map(m => ({ value: String(m), label: m < 60 ? `${m} minutes` : `${m / 60} hour${m > 60 ? 's' : ''}` }))
const KEEP = [30, 90, 180, 365, 730].map(d => ({ value: String(d), label: d < 365 ? `${d} days` : `${d / 365} year${d > 365 ? 's' : ''}` }))
const FORGET = [0, 7, 30, 90].map(d => ({ value: String(d), label: d ? `After ${d} days away` : 'Never' }))
const DIGEST = [0, 5, 15, 60].map(m => ({ value: String(m), label: m ? `Gather for ${m} minutes` : 'Right away' }))
const TIMES = Array.from({ length: 48 }, (_, i) => { const t = `${String(Math.floor(i / 2)).padStart(2, '0')}:${i % 2 ? '30' : '00'}`; return { value: t, label: t } })

function General() {
  const { f, setF, save, busy, isAdmin } = useSettingsForm()
  const [owner, setOwner] = useState('')
  if (!f) return <Loading />
  const addOwner = () => { const n = owner.trim(); if (!n) return; setOwner(''); save({ owners: [...f.owners, n] }) }
  return (
    <div className="stack loose">
      <Section title="Online and offline" icon="wifi">
        <SettingRow title="Offline after" help="A device nobody has seen for this long counts as offline. Phones sleep and drop off now and then, so a few minutes of silence is normal.">
          <Select ariaLabel="Offline after" value={String(f.offline_min)} onChange={v => save({ offline_min: Number(v) })} options={MINUTES} />
        </SettingRow>
        <SettingRow title="Recognise phones that change their address" help="Phones, watches and laptops make up a new private address now and then. With the same name as a device you already have, it joins that device instead of showing up as new.">
          <Switch checked={f.auto_merge} onChange={v => save({ auto_merge: v })} label="Recognise private addresses" />
        </SettingRow>
        <SettingRow title="Watch routers, servers and NAS boxes" help="New devices that look like network gear or servers are watched from the start, so you hear when they go down.">
          <Switch checked={f.auto_watch} onChange={v => save({ auto_watch: v })} label="Watch infrastructure" />
        </SettingRow>
        {f.local_scan && <SettingRow title="Follow the internet address" help="Checked once an hour; you see when your provider gives you a new public address.">
          <Switch checked={f.track_wan} onChange={v => save({ track_wan: v })} label="Internet address" />
        </SettingRow>}
      </Section>
      <Section title="The household" icon="users" sub="The owners offered when you name a device. People you type in a device’s owner field are added too.">
        <div className="row wrap">{f.owners.length === 0 ? <span className="muted small">Nobody yet.</span> : f.owners.map(o => <span key={o} className="chip">{o}<button type="button" className="chip-x" aria-label={`Remove ${o}`} onClick={() => save({ owners: f.owners.filter(x => x !== o) })}><Icon name="x" size={11} /></button></span>)}</div>
        <form className="row mt" onSubmit={e => { e.preventDefault(); addOwner() }}><input value={owner} onChange={e => setOwner(e.target.value)} placeholder="A name, e.g. Maya or Kids" /><button className="btn" disabled={!owner.trim()}>Add</button></form>
      </Section>
      <Section title="History" icon="clock">
        <SettingRow title="Keep history" help="Activity and online history older than this is removed. The devices themselves stay."><Select ariaLabel="Keep history" value={String(f.keep_days)} onChange={v => save({ keep_days: Number(v) })} options={KEEP} /></SettingRow>
        <SettingRow title="Forget guests" help="A guest’s device that has not been back for this long disappears from the list."><Select ariaLabel="Forget guests" value={String(f.forget_guests_days)} onChange={v => save({ forget_guests_days: Number(v) })} options={FORGET} /></SettingRow>
      </Section>
      <Section title="Dates and times" icon="calendar">
        <div className="two-col tight">
          <Field group label="Dates"><Select ariaLabel="Date format" value={f.date_format} onChange={v => save({ date_format: v as DateFormat })} options={DATE_FORMATS.map(d => ({ value: d.id, label: d.label }))} /></Field>
          <Field group label="Times"><Select ariaLabel="Time format" value={f.time_format} onChange={v => save({ time_format: v as TimeFormat })} options={TIME_FORMATS.map(d => ({ value: d.id, label: d.label }))} /></Field>
        </div>
      </Section>
      {isAdmin && <Section title="This box" icon="home">
        <Field label="App name" help="Shown in the sidebar and the browser tab, for everyone."><input value={f.title} onChange={e => setF({ ...f, title: e.target.value })} /></Field>
        <div className="row mt"><button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save</button></div>
      </Section>}
    </div>
  )
}

function Alerts() {
  const { f, setF, save, busy } = useSettingsForm()
  const { run, status } = useTester()
  if (!f) return <Loading />
  const quiet = !!f.quiet_from && !!f.quiet_to
  return (
    <div className="stack loose">
      <Section title="What you hear about" icon="bell">
        <SettingRow title="A new device joins" help="Something the network has never seen. Guests and ignored devices stay quiet."><Switch checked={f.notify_new} onChange={v => save({ notify_new: v })} label="New devices" /></SettingRow>
        <SettingRow title="A watched device goes offline" help="Only devices you watch (the switch next to each one)."><Switch checked={f.notify_offline} onChange={v => save({ notify_offline: v })} label="Offline" /></SettingRow>
        <SettingRow title="A watched device comes back"><Switch checked={f.notify_back} onChange={v => save({ notify_back: v })} label="Back online" /></SettingRow>
        <SettingRow title="The internet address changes"><Switch checked={f.notify_wan} onChange={v => save({ notify_wan: v })} label="Internet address" /></SettingRow>
        <SettingRow title="A source stops working" help="A scan fails, the router refuses the password, an agent stops reporting."><Switch checked={f.notify_source} onChange={v => save({ notify_source: v })} label="Sources" /></SettingRow>
      </Section>
      <Section title="When" icon="moon">
        <SettingRow title="Several at once" help="Wait a little and send one message for everything that happened, instead of one per device."><Select ariaLabel="Gather alerts" value={String(f.notify_digest_min)} onChange={v => save({ notify_digest_min: Number(v) })} options={DIGEST} /></SettingRow>
        <SettingRow title="Quiet hours" help="Nothing is sent in this window; what happened arrives as one message when it ends.">
          <Switch checked={quiet} onChange={v => save(v ? { quiet_from: '23:00', quiet_to: '07:00' } : { quiet_from: '', quiet_to: '' })} label="Quiet hours" />
        </SettingRow>
        {quiet && <div className="two-col tight">
          <Field group label="From"><Select ariaLabel="Quiet from" value={f.quiet_from} onChange={v => save({ quiet_from: v })} options={TIMES} /></Field>
          <Field group label="Until"><Select ariaLabel="Quiet until" value={f.quiet_to} onChange={v => save({ quiet_to: v })} options={TIMES} /></Field>
        </div>}
      </Section>
      <Section title="Where alerts go" icon="send" sub="An Apprise gateway, an ntfy topic or any JSON webhook.">
        <div className="two-col tight">
          <Field label="Notification URL" help="Apprise: https://apprise.example/notify/network · ntfy: https://ntfy.sh/your-topic"><input value={f.notify_url} onChange={e => setF({ ...f, notify_url: e.target.value })} placeholder="https://…" /></Field>
          <Field group label="Format"><Select ariaLabel="Format" value={f.notify_format || 'apprise'} onChange={v => setF({ ...f, notify_format: v })} options={[{ value: 'apprise', label: 'Apprise' }, { value: 'ntfy', label: 'ntfy' }, { value: 'json', label: 'JSON webhook' }]} /></Field>
        </div>
        <div className="row wrap mt"><button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save</button><button type="button" className="btn" onClick={() => run('notify', f.notify_url, '', { format: f.notify_format })} disabled={!f.notify_url}>Send a test</button>{status('notify')}</div>
      </Section>
    </div>
  )
}

function Integrations() {
  const { f, setF, save, busy } = useSettingsForm()
  const { run, status } = useTester()
  if (!f) return <Loading />
  const coreOn = f.core_configured && f.core_registered !== false
  const eff = (m: ConnMode): ConnMode => !coreOn && m === 'core' ? 'off' : m
  return (
    <div className="stack loose">
      <div className="callout info"><Icon name="info" size={15} /><div>These connections are yours. Through <b>Pocket Core</b> Network acts as you in your own Pocket apps with nothing to paste (switch it off for yourself in Pocket Core → Settings → Apps acting for me); <b>Direct</b> uses a URL and an API token you enter.{!coreOn && <CoreHint />}</div></div>
      <Section title="Pocket Inventory — the item a device is" icon="package" actions={connBadge(eff(f.inventory_mode), f.inventory_via)}
        sub="On a device’s page, point it to its item: when it was bought, the price, the warranty, where it lives. Network only reads your items.">
        <Connection app="Pocket Inventory" mode={f.inventory_mode} via={f.inventory_via} detail={f.inventory_detail} core={coreOn} hasToken={!!f.inventory_token}
          onMode={m => save({ inventory_mode: m })} onDisconnect={() => save({ inventory_url: '', inventory_token: '' })}
          test={<><button type="button" className="btn sm" onClick={() => run('inventory', f.inventory_url, f.inventory_token)}>Test connection</button>{status('inventory')}</>}
          direct={<div className="two-col tight">
            <Field label="URL" help="Inside Docker use http://host.docker.internal:PORT or the service name."><input value={f.inventory_url} onChange={e => setF({ ...f, inventory_url: e.target.value })} placeholder="http://host.docker.internal:8186" /></Field>
            <Field label="API token" help="Pocket Inventory → Settings → API tokens"><input value={f.inventory_token} onChange={e => setF({ ...f, inventory_token: e.target.value })} autoComplete="off" /></Field>
          </div>} />
      </Section>
      <Section title="Pocket Monitoring — the health of servers" icon="gauge" actions={connBadge(eff(f.monitoring_mode), f.monitoring_via)}
        sub="A server or NAS shows its CPU, memory, temperature and status on its page here, with a jump to Pocket Monitoring.">
        <Connection app="Pocket Monitoring" mode={f.monitoring_mode} via={f.monitoring_via} detail={f.monitoring_detail} core={coreOn} hasToken={!!f.monitoring_token}
          onMode={m => save({ monitoring_mode: m })} onDisconnect={() => save({ monitoring_url: '', monitoring_token: '' })}
          test={<><button type="button" className="btn sm" onClick={() => run('monitoring', f.monitoring_url, f.monitoring_token)}>Test connection</button>{status('monitoring')}</>}
          direct={<div className="two-col tight">
            <Field label="URL"><input value={f.monitoring_url} onChange={e => setF({ ...f, monitoring_url: e.target.value })} placeholder="http://host.docker.internal:8192" /></Field>
            <Field label="API token" help="Pocket Monitoring → Settings → API tokens"><input value={f.monitoring_token} onChange={e => setF({ ...f, monitoring_token: e.target.value })} autoComplete="off" /></Field>
          </div>} />
      </Section>
      <Section title="Other Pocket apps" icon="layers" sub="Network offers your devices to the rest of the box through Pocket Core: Pocket Day notes new devices and outages in your journal, Pocket AI can answer “is the NAS up?”, Pocket Hub can show who is home. Source passwords never leave this app.">
        <dl className="set-dl">
          <div><dt>network:read</dt><dd className="muted">Devices, online state, activity and who is home.</dd></div>
          <div><dt>network:write</dt><dd className="muted">Add devices and change their name, owner, kind, place and notes — not reviews or alerts of devices it did not add.</dd></div>
          <div><dt>Events</dt><dd className="muted"><code>network.device_new</code>, <code>network.device_offline</code>, <code>network.device_back</code>, <code>network.wan_ip_changed</code></dd></div>
        </dl>
      </Section>
      <div className="row"><button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save changes</button></div>
    </div>
  )
}

function AITab() {
  const { f, setF, save, busy } = useSettingsForm()
  const { run, status } = useTester()
  const { data: ai } = useAsync(() => api.get<{ ready: boolean; provider: string; detail: string }>(`${V1}/ai/status`), [])
  if (!f) return <Loading />
  const coreOn = f.core_configured && f.core_registered !== false
  const shown = !coreOn && f.ai_provider === 'pocket' ? 'openai' : (f.ai_provider || 'auto')
  return (
    <div className="stack loose">
      <Section title="What the AI does" icon="sparkles" sub="Guesses what an unknown device is (from its maker, name, services and when it is online) and answers questions about your network from your own device list. Nothing is sent unless you press the button.">
        {ai && (ai.ready ? <p className="muted small row" style={{ gap: 6 }}><Icon name="sparkles" size={13} />{ai.provider === 'pocket' ? 'Pocket AI' : 'Your OpenAI key'} is ready.</p>
          : <div className="callout info"><Icon name="info" size={15} /><div>No AI connected{ai.detail ? ` (${ai.detail})` : ''}.{!coreOn && <CoreHint />}</div></div>)}
      </Section>
      <Section title="Provider" icon="sparkles">
        <div className="grid" style={{ gap: 12 }}>
          <Field group label="Who writes" help="Automatic prefers your enabled Pocket AI connection; without one it uses your OpenAI settings.">
            <Select ariaLabel="AI provider" value={shown} onChange={v => setF({ ...f, ai_provider: v as SettingsT['ai_provider'] })} options={[{ value: 'auto', label: coreOn ? 'Automatic — prefer Pocket AI' : 'Automatic' }, { value: 'pocket', label: 'Pocket AI', disabled: !coreOn, hint: coreOn ? undefined : 'needs Pocket Core' }, { value: 'openai', label: 'OpenAI / compatible API' }]} />
          </Field>
          {!coreOn && <CoreHint />}
          {coreOn && shown !== 'openai' && <PocketModelSelect value={f.ai_pocket_model || ''} onChange={v => setF({ ...f, ai_pocket_model: v })} />}
          {(shown === 'openai' || (!coreOn && shown === 'auto')) && <>
            <Field label="OpenAI API key" help="platform.openai.com → API keys. Stored on the server; shown masked."><input value={f.openai_api_key} onChange={e => setF({ ...f, openai_api_key: e.target.value })} autoComplete="off" placeholder="sk-…" /></Field>
            <div className="two-col tight">
              <Field label="Model" help="Default gpt-4o-mini."><input value={f.openai_model} onChange={e => setF({ ...f, openai_model: e.target.value })} placeholder="gpt-4o-mini" /></Field>
              <Field label="Base URL (optional)" help="Ollama, LM Studio, OpenRouter…"><input value={f.openai_base_url} onChange={e => setF({ ...f, openai_base_url: e.target.value })} placeholder="https://api.openai.com/v1" /></Field>
            </div>
          </>}
        </div>
      </Section>
      <div className="row wrap">
        <button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save changes</button>
        {(shown === 'openai' || (!coreOn && shown === 'auto')) && <><button type="button" className="btn" onClick={() => run('openai', f.openai_base_url, f.openai_api_key)} disabled={!f.openai_api_key}>Test the OpenAI key</button>{status('openai')}</>}
      </div>
    </div>
  )
}

function Connection({ app, mode: saved, via, detail, core, onMode, onDisconnect, hasToken, test, direct }: {
  app: string; mode: ConnMode; via: string; detail: string; core: boolean; onMode: (m: ConnMode) => void; onDisconnect: () => void
  hasToken: boolean; test: React.ReactNode; direct: React.ReactNode
}) {
  const [advanced, setAdvanced] = useState(saved === 'direct')
  const mode: ConnMode = !core && saved === 'core' ? 'off' : saved
  const ok = via !== '' && mode !== 'off'
  const showDirect = !core || mode === 'direct' || advanced
  const line = mode === 'off' ? `Off: Network does not talk to ${app}.`
    : mode === 'core' ? (ok ? `Connected through Pocket Core, as you${detail ? ` (${detail})` : ''}. Nothing to fill in; switch it off for yourself in Pocket Core → Settings → Apps acting for me.` : `Not connected: ${detail}.`)
    : (ok ? `Direct connection with your own ${app} URL and token; use Test connection to check it.` : `Not connected: ${detail || 'add the URL and a token'}.`)
  return (
    <div className="conn-block">
      <div className="conn-mode">
        <Segmented size="sm" value={mode} onChange={m => onMode(m as ConnMode)} options={[
          { value: 'core', label: 'Pocket Core', title: core ? 'Through Pocket Core: per person, nothing to paste' : 'Needs Pocket Core: there is none on this box', disabled: !core },
          ...(showDirect ? [{ value: 'direct', label: 'Direct', title: 'Your own URL and API token' }] : []),
          { value: 'off', label: 'Off' },
        ]} />
        {!showDirect && <button type="button" className="conn-advanced" onClick={() => setAdvanced(true)}>Advanced: connect directly with a URL and key</button>}
      </div>
      <div className={`conn-status ${mode === 'off' ? '' : ok ? 'ok' : 'bad'}`}><span className="dot" />{line}</div>
      {mode === 'direct' && <>
        {direct}
        <div className="row wrap">{test}{hasToken && <button type="button" className="btn sm ghost danger-text" onClick={onDisconnect} title="Remove the saved URL and token">Disconnect</button>}</div>
      </>}
      {mode === 'core' && <div className="row wrap">{test}</div>}
    </div>
  )
}

const connBadge = (mode: ConnMode, via: string) => mode === 'off' ? <span className="badge">off</span>
  : via ? <span className="badge ok">{via === 'core' ? 'Pocket Core' : 'direct'}</span>
  : <span className="badge warn">not connected</span>

function Users() {
  const { user, users, reloadMeta, status } = useApp()
  const { toast } = useToast()
  const [edit, setEdit] = useState<Partial<User> | null>(null)
  const [del, setDel] = useState<User | null>(null)
  const isAdmin = user?.role === 'admin'
  const run = async (fn: () => Promise<unknown>, msg: string) => { try { await fn(); await reloadMeta(); toast(msg) } catch (e) { toast((e as Error).message, 'err') } }
  return (
    <Section title="People on this box" actions={isAdmin && <button type="button" className="btn primary sm" onClick={() => setEdit({ role: 'member' })}><Icon name="plus" size={14} />Add person</button>}
      sub="Everyone signs in with their own account and has their own devices, sources, alerts and connections. Admins manage accounts; they do not see anyone else’s devices.">
      <div className="set-people">
        {users.map(u => (
          <div key={u.id} className="set-person">
            <Avatar name={u.name} email={u.email} size={34} />
            <div className="grow">
              <div className="set-person-name">{u.name || <span className="muted">—</span>}{u.id === user?.id && <span className="badge">you</span>}{u.role === 'admin' && <span className="badge accent">admin</span>}</div>
              <div className="muted small ellipsis">{u.email}</div>
            </div>
            {isAdmin && <span className="row" style={{ justifyContent: 'flex-end' }}><button type="button" className="btn ghost sm" onClick={() => setEdit(u)}>Edit</button>{u.id !== user?.id && <button type="button" className="btn ghost sm danger-text" onClick={() => setDel(u)}>Remove</button>}</span>}
          </div>
        ))}
      </div>
      {status?.auth_mode !== 'local' && <p className="muted small mt">Auth mode is <code>{status?.auth_mode}</code>: users are created automatically on first visit; passwords are not used here.</p>}
      {edit && <UserModal user={edit} local={status?.auth_mode === 'local'} onClose={() => setEdit(null)} onSaved={() => { setEdit(null); reloadMeta() }} />}
      {del && <Confirm confirmLabel="Remove" title={`Remove ${del.name || del.email}?`} message="Their account is deleted together with their devices, sources and history." onCancel={() => setDel(null)} onConfirm={() => { const d = del; setDel(null); run(() => api.del(`${V1}/users/${d.id}`), 'User removed') }} />}
    </Section>
  )
}

function UserModal({ user, local, onClose, onSaved }: { user: Partial<User>; local: boolean; onClose: () => void; onSaved: () => void }) {
  const { toast } = useToast()
  const [f, setF] = useState({ name: user.name || '', email: user.email || '', password: '', role: user.role || 'member' })
  const [busy, setBusy] = useState(false)
  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { if (user.id) await api.put(`${V1}/users/${user.id}`, f); else await api.post(`${V1}/users`, f); toast(user.id ? 'User saved' : 'User added'); onSaved() } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <Modal title={user.id ? 'Edit person' : 'Add a person'} onClose={onClose} size="sm">
      <form onSubmit={save} className="grid" style={{ gap: 14 }}>
        <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} autoFocus required maxLength={60} /></Field>
        <Field label="Email" help="Used to sign in."><input type="email" value={f.email} onChange={e => setF({ ...f, email: e.target.value })} required /></Field>
        {local && <Field label={user.id ? 'New password (leave empty to keep)' : 'Password'} help="At least 8 characters."><input type="password" value={f.password} onChange={e => setF({ ...f, password: e.target.value })} minLength={user.id ? 0 : 8} required={!user.id} autoComplete="new-password" /></Field>}
        <Field group label="Role"><Segmented value={f.role} onChange={v => setF({ ...f, role: v })} options={[{ value: 'member', label: 'Member' }, { value: 'admin', label: 'Admin' }]} /></Field>
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}>{user.id ? 'Save' : 'Add person'}</button></div>
      </form>
    </Modal>
  )
}

function Profile() {
  const { user, setUser, status } = useApp()
  const { toast } = useToast()
  const [f, setF] = useState({ name: user?.name || '', email: user?.email || '', current_password: '', new_password: '' })
  const [busy, setBusy] = useState(false)
  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { setUser(await api.put(`${V1}/auth/me`, f)); setF(x => ({ ...x, current_password: '', new_password: '' })); toast('Profile saved') } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <form onSubmit={save} className="stack loose">
      <div className="set-profile">
        <Avatar name={user?.name} email={user?.email} size={56} />
        <div className="grow"><h3>{user?.name || user?.email}</h3><p className="muted small">{user?.email} · {user?.role}</p></div>
      </div>
      <Section title="You">
        <div className="stack">
          <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} /></Field>
          <Field label="Email"><input type="email" value={f.email} onChange={e => setF({ ...f, email: e.target.value })} /></Field>
        </div>
      </Section>
      {status?.auth_mode === 'local' && <Section title="Password" sub="Leave empty to keep the current one."><div className="stack">
        <Field label="Current password"><input type="password" value={f.current_password} onChange={e => setF({ ...f, current_password: e.target.value })} autoComplete="current-password" /></Field>
        <Field label="New password" help="At least 8 characters."><input type="password" value={f.new_password} onChange={e => setF({ ...f, new_password: e.target.value })} autoComplete="new-password" /></Field>
      </div></Section>}
      <div className="row"><button className="btn primary" disabled={busy}>Save changes</button></div>
    </form>
  )
}

function Tokens() {
  const { toast } = useToast()
  const { settings } = useApp()
  const { data, reload } = useAsync(() => api.get<APIToken[]>(`${V1}/tokens`), [])
  const [name, setName] = useState('')
  const [fresh, setFresh] = useState('')
  const [del, setDel] = useState<APIToken | null>(null)
  const create = async (e: React.FormEvent) => {
    e.preventDefault()
    try { const r = await api.post<{ token: string }>(`${V1}/tokens`, { name: name || 'token' }); setFresh(r.token); setName(''); reload() } catch (e) { toast((e as Error).message, 'err') }
  }
  const base = settings?.base_url || window.location.origin
  return (
    <div className="stack loose">
      <Section title="Personal tokens" icon="key" sub={<>Use as <code>Authorization: Bearer &lt;token&gt;</code>. A token acts as you: it can read and change everything, feed a Homepage widget (<code>GET /api/v1/dashboard</code>) or start a scan from a Shortcut. See <code>docs/api.md</code>. Agents use their own tokens (Sources).</>}>
        <form onSubmit={create} className="row mb"><input value={name} onChange={e => setName(e.target.value)} placeholder="Token name (e.g. Homepage)" /><button className="btn primary"><Icon name="plus" size={15} />Create</button></form>
        {fresh && <div className="set-fresh mb"><div className="row"><Icon name="check-circle" size={16} /><b>Copy it now — it is shown once.</b></div><pre className="mono">{fresh}</pre></div>}
        {data && data.length > 0 ? (
          <table className="table compact">
            <thead><tr><th>Name</th><th>Prefix</th><th>Last used</th><th>Created</th><th /></tr></thead>
            <tbody>{data.map(t => <tr key={t.id}><td>{t.name}</td><td className="mono">{t.prefix}…</td><td className="muted">{t.last_used_at ? fmtDateTime(t.last_used_at) : 'never'}</td><td className="muted">{fmtDateTime(t.created_at)}</td><td className="right"><button type="button" className="btn ghost sm danger-text" onClick={() => setDel(t)}>Revoke</button></td></tr>)}</tbody>
          </table>
        ) : <p className="muted">No tokens yet.</p>}
        {del && <Confirm confirmLabel="Revoke" title={`Revoke “${del.name}”?`} message="Anything using this token stops working immediately." onCancel={() => setDel(null)} onConfirm={async () => { await api.del(`${V1}/tokens/${del.id}`); setDel(null); reload() }} />}
      </Section>
      <Section title="Endpoints" icon="code" sub={<>At <code>{base}/api/v1</code>.</>}>
        <dl className="set-dl">
          <div><dt><code>GET /dashboard</code></dt><dd className="muted">Devices online, new, watched offline, networks, who is home — for Homepage.</dd></div>
          <div><dt><code>GET /devices?online=1</code></dt><dd className="muted">The device list (filters: <code>online</code>, <code>review</code>, <code>watch</code>, <code>owner</code>, <code>kind</code>, <code>q</code>).</dd></div>
          <div><dt><code>POST /scan</code></dt><dd className="muted">Run every source now.</dd></div>
          <div><dt><code>GET /events?kind=new,offline</code></dt><dd className="muted">Activity, newest first.</dd></div>
          <div><dt><code>GET /summary/YYYY-MM-DD</code></dt><dd className="muted">One day on the network — what Pocket Day reads.</dd></div>
        </dl>
      </Section>
    </div>
  )
}

function Data() {
  const { toast } = useToast()
  const { user } = useApp()
  const [busy, setBusy] = useState(false)
  const importJSON = async (file: File) => {
    setBusy(true)
    try { const r = await api.post<Record<string, number>>(`${V1}/import`, JSON.parse(await file.text())); toast('Imported: ' + (Object.entries(r).map(([k, v]) => `${v} ${k}`).join(', ') || 'nothing new')); window.dispatchEvent(new CustomEvent('pnet:changed')) } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <div className="stack loose">
      {user?.role === 'admin' && <Section title="Database backup" icon="database" sub="The whole box, every account — admins only.">
        <p className="muted">A consistent copy of the SQLite database (<code>VACUUM INTO</code>). Restore by stopping the app and replacing <code>pocket-network.db</code> in the data directory.</p>
        <a className="btn primary" href={`${V1}/backup`}><Icon name="download" size={15} />Download database</a>
      </Section>}
      <Section title="Export & import" icon="archive" sub="Your devices with their addresses and identifiers as one JSON file (sources without their passwords), or the device list as a CSV. Importing the same file twice adds nothing: devices are matched by their MAC address.">
        <div className="row wrap"><a className="btn" href={`${V1}/export.json`}><Icon name="download" size={15} />Export JSON</a><a className="btn" href={`${V1}/export.csv`}><Icon name="download" size={15} />Devices CSV</a><label className="btn"><Icon name="upload" size={15} />{busy ? 'Importing…' : 'Import JSON…'}<input type="file" accept="application/json,.json" hidden onChange={e => e.target.files?.[0] && importJSON(e.target.files[0])} /></label></div>
      </Section>
      <Section title="Move over from NetAlertX" icon="upload" sub="Import its devices.csv with names, owners, types, notes and alert settings.">
        <Link className="btn" to="/sources?import=1"><Icon name="upload" size={15} />Import from NetAlertX…</Link>
      </Section>
    </div>
  )
}

function About() {
  const { data, error } = useAsync(() => api.get<Record<string, unknown>>(`${V1}/settings/system`), [])
  const [keys, setKeys] = useState(false)
  if (error) return <div className="callout danger"><Icon name="alert" size={15} /><span>{error}</span></div>
  if (!data) return <Loading />
  const counts = (data.counts || {}) as Record<string, number>
  const core = (data.core || {}) as { configured?: boolean; registered?: boolean; url?: string; error?: string }
  const tiles: [string, number | undefined, string][] = [['Devices', counts.devices, 'list'], ['Addresses', counts.addresses, 'route'], ['Events', counts.events, 'activity'], ['Sources', counts.sources, 'layers']]
  const rows: [string, React.ReactNode][] = [
    ['Version', <code key="v">{String(data.version)}</code>],
    ['Uptime', String(data.uptime)],
    ['Runtime', `${String(data.go)} · ${String(data.os)}/${String(data.arch)}`],
    ['Time zone', String(data.timezone)],
    ['Sign-in', String(data.auth_mode)],
    ['Pocket Core', core.configured ? (core.registered ? <span key="c" className="ok">connected · {core.url}</span> : <span key="c" className="bad">{core.error || 'not registered yet'}</span>) : <span key="c" className="muted">not configured</span>],
    ['Database', <span key="db"><code>{String(data.db_path)}</code> · {fmtBytes(Number(data.db_bytes))}</span>],
    ['People', `${counts.users ?? 0} accounts on this box`],
  ]
  return (
    <div className="stack loose">
      <div className="set-about-hero">
        <img src="/favicon.svg" alt="" width={48} height={48} />
        <div>
          <h3>Pocket Network</h3>
          <p className="muted small">Every device on your home network in one list: found by scanning, Tailscale, AdGuard Home and your router, with a simple review of new devices, online history and alerts when something new joins or an important device drops off. One binary, one SQLite file, one volume.</p>
        </div>
      </div>
      <div className="set-stats">
        {tiles.map(([label, n, icon]) => (
          <div key={label} className="set-stat"><Icon name={icon} size={16} /><span className="set-stat-n tnum">{(n ?? 0).toLocaleString()}</span><span className="set-stat-l">{label}</span></div>
        ))}
      </div>
      <Section title="This server">
        <dl className="set-dl">
          {rows.map(([k, v]) => <div key={k}><dt>{k}</dt><dd>{v}</dd></div>)}
        </dl>
      </Section>
      <div className="row wrap">
        <button type="button" className="btn" onClick={() => setKeys(true)}><Icon name="keyboard" size={15} />Keyboard shortcuts</button>
        <Link className="btn" to="/sources"><Icon name="layers" size={15} />Sources</Link>
        <a className="btn" href="https://github.com/abjelosevic88/pocket-homelab-network" target="_blank" rel="noreferrer"><Icon name="external" size={15} />Project on GitHub</a>
      </div>
      <p className="muted small">Open source (MIT). Part of the Pocket apps: Pocket Core connects them — Inventory, Monitoring, Day, Hub and more. Scanning only ever touches your own networks.</p>
      {keys && <ShortcutsSheet onClose={() => setKeys(false)} />}
    </div>
  )
}
