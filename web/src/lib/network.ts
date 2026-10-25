// Device kinds, names and the small wording helpers every page shares.
import type { Device, NetEvent, Source } from './types'

export const KINDS: { id: string; name: string; icon: string }[] = [
  { id: 'phone', name: 'Phone', icon: 'smartphone' }, { id: 'tablet', name: 'Tablet', icon: 'tablet' }, { id: 'laptop', name: 'Laptop', icon: 'laptop' },
  { id: 'computer', name: 'Computer', icon: 'monitor' }, { id: 'watch', name: 'Watch', icon: 'watch' }, { id: 'tv', name: 'TV or streamer', icon: 'tv' },
  { id: 'speaker', name: 'Speaker', icon: 'speaker' }, { id: 'console', name: 'Game console', icon: 'gamepad' }, { id: 'printer', name: 'Printer', icon: 'printer' },
  { id: 'camera', name: 'Camera', icon: 'camera' }, { id: 'smart_home', name: 'Smart home', icon: 'plug' }, { id: 'router', name: 'Router', icon: 'router' },
  { id: 'access_point', name: 'Wi-Fi access point', icon: 'wifi' }, { id: 'switch', name: 'Network switch', icon: 'switch' }, { id: 'server', name: 'Server', icon: 'server' },
  { id: 'nas', name: 'NAS (storage)', icon: 'hard-drive' }, { id: 'car', name: 'Car', icon: 'car' }, { id: 'other', name: 'Other', icon: 'cpu' },
]
const BY_ID = Object.fromEntries(KINDS.map(k => [k.id, k]))

/** The kind in effect: what you chose, else the guess. */
export const kindOf = (d: Pick<Device, 'kind' | 'kind_guess'>) => d.kind || d.kind_guess || ''
export const kindName = (k: string) => BY_ID[k]?.name || 'Not known yet'
export const kindIcon = (k: string) => BY_ID[k]?.icon || 'cpu'

/** The name to show (the same rule as the server's Device.Label). */
export function label(d: Pick<Device, 'name' | 'hostname' | 'vendor' | 'last_ip'>): string {
  return d.name || d.hostname || (d.vendor ? `${d.vendor} device` : d.last_ip ? `Device at ${d.last_ip}` : 'Unnamed device')
}

/** A colour per kind for the icon tiles (stable, calm). */
const KIND_COLOR: Record<string, string> = {
  phone: '#2b7a9e', tablet: '#2b7a9e', laptop: '#6366f1', computer: '#6366f1', watch: '#0d9488', tv: '#9333ea', speaker: '#9333ea', console: '#db2777',
  printer: '#64748b', camera: '#dc2626', smart_home: '#d97706', router: '#16a34a', access_point: '#16a34a', switch: '#16a34a', server: '#0f766e', nas: '#0f766e', car: '#c2410c', other: '#64748b',
}
export const kindColor = (k: string) => KIND_COLOR[k] || '#64748b'

/** "just now", "5 min ago", "3 h ago", "yesterday", "12 days ago". */
export function ago(iso: string): string {
  if (!iso) return 'never'
  const s = (Date.now() - new Date(iso).getTime()) / 1000
  if (s < 90) return 'just now'
  if (s < 3600) return `${Math.round(s / 60)} min ago`
  if (s < 86400) return `${Math.round(s / 3600)} h ago`
  const d = Math.round(s / 86400)
  return d === 1 ? 'yesterday' : d < 60 ? `${d} days ago` : `${Math.round(d / 30)} months ago`
}

/** How long since a time, without "ago": "a moment", "5 min", "3 h", "2 days". */
export function howLong(iso: string): string {
  if (!iso) return ''
  const s = (Date.now() - new Date(iso).getTime()) / 1000
  if (s < 90) return 'a moment'
  if (s < 3600) return `${Math.round(s / 60)} min`
  if (s < 86400) return `${Math.round(s / 3600)} h`
  const d = Math.round(s / 86400)
  return d === 1 ? '1 day' : `${d} days`
}

/** One line on how a device is doing. */
export function statusLine(d: Device): string {
  if (d.online) return d.status_at ? `Online for ${howLong(d.status_at)}` : 'Online'
  return d.last_seen ? `Last seen ${ago(d.last_seen)}` : 'Not seen yet'
}

export const EVENT_ICON: Record<string, string> = {
  new: 'sparkles', online: 'wifi', offline: 'wifi-off', ip: 'route', address: 'shield', merged: 'merge', renamed: 'edit', wan_ip: 'globe',
  source_failed: 'alert', source_ok: 'check-circle', imported: 'download', wake: 'power',
}
export const EVENT_NAME: Record<string, string> = {
  new: 'New devices', online: 'Came back', offline: 'Went offline', ip: 'Address changes', address: 'Private addresses', merged: 'Joined',
  renamed: 'Renamed', wan_ip: 'Internet address', source_failed: 'Source problems', source_ok: 'Source recovered', imported: 'Imported', wake: 'Wake-on-LAN',
}
export const eventTone = (e: NetEvent) => e.kind === 'offline' || e.kind === 'source_failed' ? 'bad' : e.kind === 'online' || e.kind === 'source_ok' ? 'ok' : e.kind === 'new' ? 'accent' : ''

export const SOURCE_ICON: Record<string, string> = { scan: 'radar', agent: 'router', tailscale: 'link', adguard: 'shield', openwrt: 'wifi' }
export const SOURCE_HELP: Record<string, string> = {
  scan: 'This server sweeps a network it is on and asks each device its name.',
  agent: 'A small program on a computer or router elsewhere scans that network and reports here — for a network behind its own router.',
  tailscale: 'Every phone and laptop on your tailnet, at home or away.',
  adguard: 'Names AdGuard Home knows and how much each device uses DNS. It adds to devices the others found.',
  openwrt: 'DHCP leases and Wi-Fi clients from an OpenWrt router (GL.iNet routers with LuCI too).',
}
export function sourceLine(s: Source): string {
  if (!s.enabled) return 'Paused'
  if (s.running) return 'Running now…'
  if (s.last_error) return s.last_error
  if (!s.last_run) return s.kind === 'agent' ? 'Waiting for the agent to report' : 'Not run yet'
  return `${s.last_count} ${s.last_count === 1 ? 'device' : 'devices'} · ${ago(s.last_run)}`
}

export function fmtInterval(s: number): string {
  if (s % 3600 === 0) return s === 3600 ? 'every hour' : `every ${s / 3600} hours`
  if (s % 60 === 0) return s === 60 ? 'every minute' : `every ${s / 60} minutes`
  return `every ${s} s`
}

export function greeting(): string {
  const h = new Date().getHours()
  return h < 5 ? 'Good night' : h < 12 ? 'Good morning' : h < 18 ? 'Good afternoon' : 'Good evening'
}
