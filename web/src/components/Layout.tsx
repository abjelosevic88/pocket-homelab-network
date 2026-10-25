// The app shell: sidebar, top bar, search palette, shortcuts — the Pocket Calendar layout.
import { createContext, useCallback, useContext, useEffect, useRef, useState } from 'react'
import { Outlet, useLocation, useNavigate, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Dashboard } from '../lib/types'
import { Icon } from './icons'
import { Sidebar } from './Sidebar'
import { CommandPalette, type PaletteCommand } from './CommandPalette'
import { ShortcutsSheet } from './ShortcutsSheet'
import { DeviceModal } from './DeviceForm'
import { Avatar, type Anchor, type MenuItem, Menu, MenuButton, modalOpen, rectOf, useIsMobile, useMediaQuery, useToast } from './ui'
import { AskButton } from './AskButton'

const SIDEBAR_KEY = 'pnet-sidebar'

/** Something was added or changed from the shell (or a page): the counts and the pages reload. */
export const announceChange = () => window.dispatchEvent(new CustomEvent('pnet:changed'))
export function useOnChange(cb: () => void) {
  const ref = useRef(cb)
  ref.current = cb
  useEffect(() => {
    const h = () => ref.current()
    window.addEventListener('pnet:changed', h)
    return () => window.removeEventListener('pnet:changed', h)
  }, [])
}

/** The page's own name for the top bar breadcrumb. */
const CrumbCtx = createContext<(t: string) => void>(() => {})
export function useCrumb(title: string | undefined) {
  const set = useContext(CrumbCtx)
  useEffect(() => { set(title || ''); return () => set('') }, [title, set])
}

/** The sidebar numbers, for pages that show them too. */
const CountsCtx = createContext<Dashboard | null>(null)
export const useCounts = () => useContext(CountsCtx)

export type CreateKind = 'scan' | 'device' | 'source' | 'import'

/** Open a create action from anywhere (a page's button). */
export const openCreate = (k: CreateKind) => window.dispatchEvent(new CustomEvent('pnet:create', { detail: { kind: k } }))

/** The add menu: the sidebar's button caret and the phone's + button. */
export function createItems(onCreate: (k: CreateKind) => void): MenuItem[] {
  return [
    { label: 'Scan now', icon: 'radar', hint: 'S', onSelect: () => onCreate('scan') },
    { label: 'Add a device by hand', icon: 'plus', hint: 'N', onSelect: () => onCreate('device') },
    'sep',
    { label: 'Add a source…', icon: 'layers', onSelect: () => onCreate('source') },
    { label: 'Import from NetAlertX…', icon: 'upload', onSelect: () => onCreate('import') },
  ]
}

export default function Layout() {
  const navigate = useNavigate()
  const loc = useLocation()
  const [sp, setSp] = useSearchParams()
  const { toast } = useToast()
  const mobile = useIsMobile()
  const narrow = useMediaQuery('(max-width: 1099px)')
  const drawer = mobile || narrow
  const [sidebarOpen, setSidebarOpen] = useState(() => !drawer && localStorage.getItem(SIDEBAR_KEY) !== '0')
  useEffect(() => { setSidebarOpen(!drawer && localStorage.getItem(SIDEBAR_KEY) !== '0') }, [drawer])
  const drawerOpenedAt = useRef(0)
  const toggleSidebar = () => setSidebarOpen(o => { drawerOpenedAt.current = performance.now(); const n = !o; if (!drawer) localStorage.setItem(SIDEBAR_KEY, n ? '1' : '0'); return n })
  const closeDrawer = () => { if (drawer) setSidebarOpen(false) }
  useEffect(() => { document.querySelector('.content')?.scrollTo({ top: 0 }) }, [loc.pathname])

  const [crumb, setCrumbRaw] = useState('')
  const setCrumb = useCallback((t: string) => setCrumbRaw(t), [])

  const [counts, setCounts] = useState<Dashboard | null>(null)
  const load = useCallback(() => { api.get<Dashboard>(`${V1}/dashboard`).then(setCounts).catch(() => {}) }, [])
  useEffect(() => { load() }, [loc.pathname, load])
  useEffect(() => { const t = setInterval(load, 60_000); return () => clearInterval(t) }, [load])
  useOnChange(load)

  const [palette, setPalette] = useState(false)
  const [shortcuts, setShortcuts] = useState(false)
  const [modal, setModal] = useState<'device' | null>(null)
  const [fabMenu, setFabMenu] = useState<Anchor | null>(null)
  const [scanning, setScanning] = useState(false)

  // /?add=1 (the PWA shortcut) opens the add-a-device dialog
  useEffect(() => { if (sp.get('add')) { setModal('device'); setSp(new URLSearchParams(), { replace: true }) } }, []) // eslint-disable-line react-hooks/exhaustive-deps

  const scan = async () => {
    if (scanning) return
    setScanning(true)
    try {
      const r = await api.post<{ started: number }>(`${V1}/scan`)
      if (!r.started) { toast('Nothing to run: add a source first, or it is running already'); setScanning(false); return }
      toast(r.started === 1 ? 'Scanning…' : `Running ${r.started} sources…`)
      setTimeout(() => { announceChange(); setScanning(false) }, 9000)
    } catch (e) { toast((e as Error).message, 'err'); setScanning(false) }
  }

  const create = (k: CreateKind) => {
    closeDrawer()
    switch (k) {
      case 'scan': scan(); break
      case 'device': setModal('device'); break
      case 'source': navigate('/sources?add=1'); break
      case 'import': navigate('/sources?import=1'); break
    }
  }
  const createRef = useRef(create)
  createRef.current = create

  useEffect(() => {
    const open = () => setShortcuts(true)
    const make = (e: Event) => { const d = (e as CustomEvent<{ kind: CreateKind }>).detail; createRef.current(d.kind) }
    window.addEventListener('pnet:shortcuts', open)
    window.addEventListener('pnet:create', make)
    return () => { window.removeEventListener('pnet:shortcuts', open); window.removeEventListener('pnet:create', make) }
  }, [])
  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') { e.preventDefault(); setPalette(p => !p); return }
      if (e.metaKey || e.ctrlKey || e.altKey) return
      if (t.closest('input, textarea, select, [contenteditable="true"]') || modalOpen() || document.querySelector('.menu, .popover, .sel-pop')) return
      const go = (to: string) => { e.preventDefault(); navigate(to) }
      const mk = (k: CreateKind) => { e.preventDefault(); createRef.current(k) }
      switch (e.key) {
        case '/': e.preventDefault(); setPalette(true); break
        case '?': e.preventDefault(); setShortcuts(true); break
        case 'n': mk('device'); break
        case 's': mk('scan'); break
        case 'h': go('/'); break
        case 'd': go('/devices'); break
        case 'r': go('/review'); break
        case 'a': go('/activity'); break
        case 'o': go('/sources'); break
        case ',': go('/settings'); break
        case '[': e.preventDefault(); toggleSidebar(); break
      }
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }) // eslint-disable-line react-hooks/exhaustive-deps

  const commands: PaletteCommand[] = [
    { id: 'scan', label: 'Scan now', icon: 'radar', hint: 'S', keywords: 'refresh sweep find run', run: () => create('scan') },
    { id: 'add', label: 'Add a device by hand', icon: 'plus', hint: 'N', keywords: 'new manual', run: () => create('device') },
    { id: 'home', label: 'Overview', icon: 'home', hint: 'H', run: () => navigate('/') },
    { id: 'devices', label: 'Devices', icon: 'list', hint: 'D', keywords: 'all list online offline', run: () => navigate('/devices') },
    { id: 'online', label: 'Devices online now', icon: 'wifi', keywords: 'connected here', run: () => navigate('/devices?show=online') },
    { id: 'review', label: 'Review new devices', icon: 'user-check', hint: 'R', keywords: 'new unknown strangers', run: () => navigate('/review') },
    { id: 'activity', label: 'Activity', icon: 'activity', hint: 'A', keywords: 'history events timeline offline', run: () => navigate('/activity') },
    { id: 'sources', label: 'Sources', icon: 'layers', hint: 'O', keywords: 'scan agent tailscale adguard router openwrt', run: () => navigate('/sources') },
    { id: 'import', label: 'Import from NetAlertX', icon: 'upload', keywords: 'migrate csv', run: () => create('import') },
    { id: 'settings', label: 'Settings', icon: 'settings', hint: ',', run: () => navigate('/settings') },
    { id: 'alerts', label: 'Settings: alerts', icon: 'bell', keywords: 'notifications ntfy apprise quiet', run: () => navigate('/settings/alerts') },
    { id: 'apps', label: 'Settings: connected apps', icon: 'layers', keywords: 'inventory monitoring core integrations', run: () => navigate('/settings/apps') },
    { id: 'shortcuts', label: 'Keyboard shortcuts', icon: 'keyboard', hint: '?', run: () => setShortcuts(true) },
  ]

  return (
    <CrumbCtx.Provider value={setCrumb}>
      <CountsCtx.Provider value={counts}>
        <div className={`app ${sidebarOpen ? 'with-sidebar' : 'no-sidebar'} ${mobile ? 'is-mobile' : ''} ${drawer ? 'drawer-mode' : ''}`}>
          {drawer && sidebarOpen && <div className="drawer-backdrop" onClick={e => { if (e.timeStamp - drawerOpenedAt.current > 400) setSidebarOpen(false) }} />}
          <div className={`sidebar-wrap ${sidebarOpen ? 'open' : ''}`}>
            <Sidebar onCreate={create} onNavigate={closeDrawer} counts={counts} scanning={scanning} />
          </div>
          <div className="main">
            <TopBar onMenu={toggleSidebar} onPalette={() => setPalette(true)} mobile={mobile} compact={narrow} sidebarOpen={sidebarOpen} crumb={crumb} />
            <main className="content"><div className="content-inner"><Outlet /></div></main>
          </div>
          {mobile && <button type="button" className="fab" aria-label="Add" title="Add" aria-haspopup="menu" aria-expanded={!!fabMenu} onClick={e => setFabMenu(fabMenu ? null : rectOf(e.currentTarget))}><Icon name="plus" size={24} strokeWidth={2.2} /></button>}
          {fabMenu && <Menu anchor={fabMenu} align="right" items={createItems(create)} onClose={() => setFabMenu(null)} width={250} />}
          {palette && <CommandPalette onClose={() => setPalette(false)} commands={commands} />}
          {shortcuts && <ShortcutsSheet onClose={() => setShortcuts(false)} />}
          {modal === 'device' && <DeviceModal onClose={() => setModal(null)} onSaved={d => { setModal(null); announceChange(); navigate(`/devices/${d.id}`) }} />}
        </div>
      </CountsCtx.Provider>
    </CrumbCtx.Provider>
  )
}

const SECTION: Record<string, [string, string]> = { '': ['Overview', '/'], devices: ['Devices', '/devices'], review: ['Review', '/review'], activity: ['Activity', '/activity'], sources: ['Sources', '/sources'] }

function TopBar({ onMenu, onPalette, mobile, compact, sidebarOpen, crumb }: { onMenu: () => void; onPalette: () => void; mobile: boolean; compact: boolean; sidebarOpen: boolean; crumb: string }) {
  const { user, themeMode, setThemeMode, status, setUser } = useApp()
  const navigate = useNavigate()
  const loc = useLocation()
  const [section, sectionTo] = SECTION[loc.pathname.split('/')[1] || ''] || ['', '/']
  const signOut = async () => { await api.post(`${V1}/auth/logout`).catch(() => {}); setUser(null); navigate('/login') }
  const avatarMenu = (): MenuItem[] => [
    { heading: user?.name || user?.email || '' },
    { label: 'Profile', icon: 'user', onSelect: () => navigate('/settings/profile') },
    { label: 'Settings', icon: 'settings', onSelect: () => navigate('/settings'), hint: ',' },
    { label: 'Keyboard shortcuts', icon: 'keyboard', onSelect: () => window.dispatchEvent(new CustomEvent('pnet:shortcuts')), hint: '?' },
    'sep',
    { heading: 'Appearance' },
    { label: 'System', checked: themeMode === 'system', onSelect: () => setThemeMode('system') },
    { label: 'Light', checked: themeMode === 'light', onSelect: () => setThemeMode('light') },
    { label: 'Dark', checked: themeMode === 'dark', onSelect: () => setThemeMode('dark') },
    ...(status?.auth_mode === 'local' ? ['sep' as const, { label: 'Sign out', icon: 'log-out', onSelect: signOut }] : []),
  ]
  return (
    <header className="topbar">
      <button type="button" className="btn ghost icon-only" onClick={onMenu} aria-label={sidebarOpen ? 'Hide the sidebar' : 'Show the sidebar'} title="Menu ([)"><Icon name={mobile || compact ? 'menu' : 'sidebar'} size={18} /></button>
      <h1 className="range-title">
        {crumb ? <><button type="button" className="crumb" onClick={() => navigate(sectionTo)}>{section}</button><Icon name="chevron-right" size={16} className="crumb-sep" /><span className="ellipsis">{crumb}</span></> : section}
      </h1>
      <span className="spacer" />
      <button type="button" className="btn ghost icon-only" onClick={onPalette} aria-label="Search" title="Search (⌘K or /)"><Icon name="search" size={18} /></button>
      <AskButton />
      <MenuButton label="Account" className="avatar-btn" align="right" width={220} items={avatarMenu}>
        <Avatar name={user?.name} email={user?.email} size={30} />
      </MenuButton>
    </header>
  )
}
