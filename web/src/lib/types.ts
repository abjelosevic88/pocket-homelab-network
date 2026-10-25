export interface User { id: number; email: string; name: string; role: 'admin' | 'member'; created_at: string }
export interface APIToken { id: number; user_id: number; name: string; prefix: string; last_used_at: string | null; created_at: string }

export type ConnMode = 'core' | 'direct' | 'off'
export type ConnVia = '' | 'core' | 'direct'

export interface Settings {
  core_configured: boolean
  core_registered?: boolean
  title: string; date_format: string; time_format: string; week_start: number
  offline_min: number; auto_merge: boolean; auto_watch: boolean; keep_days: number; track_wan: boolean; forget_guests_days: number; owners: string[]
  notify_new: boolean; notify_offline: boolean; notify_back: boolean; notify_wan: boolean; notify_source: boolean
  quiet_from: string; quiet_to: string; notify_digest_min: number
  notify_url: string; notify_format: string; notify_configured: boolean
  inventory_mode: ConnMode; inventory_via: ConnVia; inventory_detail: string; inventory_open_url: string; inventory_configured: boolean
  inventory_url: string; inventory_token: string; inventory_public_url: string
  monitoring_mode: ConnMode; monitoring_via: ConnVia; monitoring_detail: string; monitoring_open_url: string; monitoring_configured: boolean
  monitoring_url: string; monitoring_token: string; monitoring_public_url: string
  ai_provider: 'auto' | 'pocket' | 'openai'; ai_pocket_model: string; openai_api_key: string; openai_model: string; openai_base_url: string
  setup_complete: boolean; base_url: string; local_scan: boolean
}

export type Review = 'new' | 'known' | 'guest'

export interface Device {
  id: number; name: string; hostname: string; owner: string; kind: string; kind_guess: string; vendor: string; model: string; os: string
  location: string; notes: string; link_url: string; review: Review; watch: boolean; muted: boolean; pinned: boolean; online: boolean; status_at: string
  first_seen: string; last_seen: string; last_ip: string; last_mac: string; last_network: string; last_source: string; random_mac: boolean
  dns_queries: number; dns_blocked: number; ports: string; inventory_id: number; inventory_name: string; monitoring_id: number; monitoring_name: string
  source: string; created_at: string; updated_at: string
}

export interface DeviceID { id: number; device_id: number; kind: 'mac' | 'tailscale' | string; value: string; first_seen: string; last_seen: string; private: boolean; vendor: string }
export interface Address { id: number; device_id: number; ip: string; mac: string; network: string; source_id: number; first_seen: string; last_seen: string }
export interface NetEvent { id: number; device_id: number | null; kind: string; at: string; text: string; ip: string; source_id: number; alerted: boolean; device_name?: string; device_kind?: string }
export interface PresenceHour { hour: string; minutes: number }
export interface Service { port: number; name: string; web?: string; notes?: string }
export interface ServiceCheck { ip: string; checked_at: string; services: Service[] }

export interface DeviceDetail {
  device: Device; ids: DeviceID[]; addresses: Address[]; events: NetEvent[]; presence: PresenceHour[]; similar: Device[]
  sources: Record<string, string>; services: ServiceCheck | null; can_wake: boolean
}

export type SourceKind = 'scan' | 'agent' | 'tailscale' | 'adguard' | 'openwrt'
export interface SourceCfg { interface?: string; subnet?: string; names?: boolean; dns?: string; url?: string; username?: string; socket?: string; tailnet?: string; via_core?: boolean }
export interface Source {
  id: number; kind: SourceKind; name: string; network: string; token_hint: string; enabled: boolean; interval_s: number; presence: boolean
  last_run: string; last_ok: string; last_error: string; last_count: number; last_ms: number; sort: number; created_at: string; updated_at: string; has_secret: boolean
  cfg: SourceCfg; kind_name: string; status: 'ok' | 'error' | 'waiting' | 'off' | 'running'; running: boolean
}
export interface NetInterface { name: string; ip: string; subnet: string; mac: string; hosts: number }
export interface SourcesData {
  sources: Source[]; kinds: Record<SourceKind, string>; base_url: string
  can: { local_scan: boolean; tailscale_local: boolean; interfaces: NetInterface[]; core: boolean }
}
export interface ScanRun { id: number; source_id: number; at: string; found: number; new: number; ms: number; error: string }

export interface Counts { total: number; online: number; new: number; guests: number; watched: number; watched_offline: number }
export interface NetworkCount { name: string; online: number; total: number }
export interface HomePerson { owner: string; devices: Device[]; since: string }
export interface Dashboard { counts: Counts; networks: NetworkCount[]; home: HomePerson[]; sources: number; sources_failing: number }
export interface Overview {
  counts: Counts; down: Device[]; fresh: Device[]; home: HomePerson[]; networks: NetworkCount[]
  hours: { hour: string; count: number }[]; events: NetEvent[]; sources: Source[]; wan: { ip?: string; since?: string }; online_now: number
}
export interface ReviewGroup { key: string; name: string; devices: Device[]; known?: Device }
export interface ReviewData { groups: ReviewGroup[]; devices: Device[]; owners: string[]; locations: string[]; total: number }
export interface DeviceMeta { owners: string[]; locations: string[]; kinds: { id: string; name: string }[] }

export interface InventoryItem { id: number; name: string; brand: string; model: string; serial: string; purchase_date: string; price: number; expiry_date: string; location_name: string; category_name: string; notes: string; url: string; archived: boolean }
export interface MonitoringServer {
  id: number; name: string; kind: string; status: string; last_seen: string; last_error: string; alerts_active: number
  summary: { hostname: string; platform: string; cpu: number; mem_pct: number; temp: number; fs_pct: number; uptime_s: number } | null
}
export interface LinkedState { mode: ConnMode; connected: boolean; detail: string; error?: string; open_url?: string }
export interface DeviceLinked { inventory: LinkedState & { item?: InventoryItem }; monitoring: LinkedState & { server?: MonitoringServer } }
