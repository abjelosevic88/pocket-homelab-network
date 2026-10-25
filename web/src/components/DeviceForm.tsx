// The pieces every device form shares: the kind icon, kind / owner / place pickers, and "add a device by hand".
import React, { useEffect, useState } from 'react'
import { api, V1 } from '../lib/api'
import type { Device, DeviceMeta } from '../lib/types'
import { KINDS, kindColor, kindIcon, kindOf } from '../lib/network'
import { Icon } from './icons'
import { Select } from './Select'
import { Field, Modal, useToast } from './ui'

/** The device's kind as a coloured tile, with an online dot. */
export function DeviceIcon({ d, size = 32, dot = true }: { d: Pick<Device, 'kind' | 'kind_guess' | 'online'>; size?: number; dot?: boolean }) {
  const k = kindOf(d)
  const color = kindColor(k)
  return (
    <span className="dev-ico" style={{ width: size, height: size, color, background: `color-mix(in srgb, ${color} 14%, transparent)` }}>
      <Icon name={kindIcon(k)} size={Math.round(size * 0.5)} />
      {dot && <i className={`dev-dot ${d.online ? 'on' : ''}`} aria-label={d.online ? 'online' : 'offline'} />}
    </span>
  )
}

let metaCache: DeviceMeta | null = null
/** Owners, places and kinds for the pickers (refetched after a change). */
export function useMeta(): [DeviceMeta | null, () => void] {
  const [m, setM] = useState<DeviceMeta | null>(metaCache)
  const load = () => api.get<DeviceMeta>(`${V1}/devices/meta`).then(x => { metaCache = x; setM(x) }).catch(() => {})
  useEffect(() => { load() }, [])
  return [m, load]
}

export function KindSelect({ value, guess, onChange }: { value: string; guess?: string; onChange: (v: string) => void }) {
  const opts = [{ value: '', label: guess ? `${KINDS.find(k => k.id === guess)?.name || guess}?` : 'Not set', hint: guess ? 'a guess' : undefined }, ...KINDS.map(k => ({ value: k.id, label: k.name }))]
  return <Select ariaLabel="Kind of device" value={value} onChange={onChange} options={opts} />
}

function namesSelect(names: string[], value: string, extra: string[] = []) {
  const all = Array.from(new Set([...extra, ...names, value].filter(Boolean)))
  return all.map(n => ({ value: n, label: n }))
}

export function OwnerSelect({ value, onChange, meta, placeholder = 'Whose is it?' }: { value: string; onChange: (v: string) => void; meta: DeviceMeta | null; placeholder?: string }) {
  return <Select ariaLabel="Owner" value={value} onChange={onChange} placeholder={placeholder} options={namesSelect(meta?.owners || [], value, ['Guests'])}
    onCreate={l => l.trim()} createLabel="Add" clearLabel="Nobody" />
}

export function PlaceSelect({ value, onChange, meta }: { value: string; onChange: (v: string) => void; meta: DeviceMeta | null }) {
  return <Select ariaLabel="Place" value={value} onChange={onChange} placeholder="Where is it?" options={namesSelect(meta?.locations || [], value)}
    onCreate={l => l.trim()} createLabel="Add" clearLabel="Nowhere in particular" />
}

/** Add a device by hand: something no source can see, or before it shows up. */
export function DeviceModal({ onClose, onSaved }: { onClose: () => void; onSaved: (d: Device) => void }) {
  const { toast } = useToast()
  const [meta] = useMeta()
  const [f, setF] = useState({ name: '', kind: '', owner: '', location: '', mac: '', ip: '', notes: '' })
  const [busy, setBusy] = useState(false)
  const save = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try { onSaved(await api.post<Device>(`${V1}/devices`, f)) } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <Modal title="Add a device by hand" onClose={onClose} footer={<><button type="button" className="btn ghost" onClick={onClose}>Cancel</button><button className="btn primary" form="dev-add" disabled={busy || !f.name.trim()}>Add device</button></>}>
      <form id="dev-add" className="stack" onSubmit={save}>
        <p className="muted small">For a device no source sees (it is switched off, or on a network nothing scans). With its MAC address it joins the list as soon as a scan finds it.</p>
        <Field label="Name"><input autoFocus value={f.name} onChange={e => setF({ ...f, name: e.target.value })} placeholder="Garage door opener" /></Field>
        <div className="two-col tight">
          <Field group label="Kind"><KindSelect value={f.kind} onChange={v => setF({ ...f, kind: v })} /></Field>
          <Field group label="Owner"><OwnerSelect value={f.owner} onChange={v => setF({ ...f, owner: v })} meta={meta} /></Field>
        </div>
        <Field group label="Place"><PlaceSelect value={f.location} onChange={v => setF({ ...f, location: v })} meta={meta} /></Field>
        <div className="two-col tight">
          <Field label="MAC address (optional)" help="On a sticker, or in the device's network settings."><input value={f.mac} onChange={e => setF({ ...f, mac: e.target.value })} placeholder="aa:bb:cc:dd:ee:ff" autoComplete="off" /></Field>
          <Field label="Address (optional)"><input value={f.ip} onChange={e => setF({ ...f, ip: e.target.value })} placeholder="192.168.1.40" inputMode="decimal" /></Field>
        </div>
        <Field label="Notes"><textarea rows={2} value={f.notes} onChange={e => setF({ ...f, notes: e.target.value })} /></Field>
      </form>
    </Modal>
  )
}

export function Badge({ children, tone = '' }: { children: React.ReactNode; tone?: '' | 'ok' | 'warn' | 'bad' | 'accent' }) {
  return <span className={`badge ${tone}`}>{children}</span>
}

export const reviewBadge = (d: Device) => d.review === 'new' ? <Badge tone="accent">new</Badge> : d.review === 'guest' ? <Badge>guest</Badge> : null

export function PrivateHint() {
  return <span className="muted small row" style={{ gap: 4 }}><Icon name="shield" size={12} />private address</span>
}
