import { lazy, type KeyboardEvent as ReactKeyboardEvent, type ReactNode, Suspense, useEffect, useMemo, useRef, useState } from 'react'
import {
  Activity,
  Boxes,
  ChevronDown,
  Clock3,
  Copy,
  FileCode2,
  KeyRound,
  Layers3,
  LogIn,
  LogOut,
  Minus,
  MoreHorizontal,
  Palette,
  PanelTopClose,
  Pencil,
  Plus,
  RefreshCcw,
  Search,
  Server,
  Settings,
  ShieldAlert,
  ShieldCheck,
  Square,
  SlidersHorizontal,
  Tags,
  TerminalSquare,
  Trash2,
  Unplug,
  X,
} from 'lucide-react'

import './App.css'
import appIconURL from '../../build/appicon.svg'
import {
  type Account,
  type Alias,
  type Asset,
  type AssetDetail,
  type AssetPage,
  type AuthStatus,
  type Backend,
  type BootstrapState,
  type HostKeyPrompt,
  type LoginAttempt,
  type Organization,
  type Preferences,
  type ProfileSummary,
  type SessionLatency,
  type SessionState,
  type ThemeMode,
  type Workspace,
  wailsBackend,
} from './lib/backend'
import {
  type AppTab,
  emptyTabWorkspace,
  reduceTabs,
  type SSHDescriptor,
  type SSHTab,
  type SingletonTabKind,
  type TabAction,
  type TabWorkspace,
} from './model/tabs'

type AliasFilter = 'all' | 'with-alias' | 'without-alias'

interface PendingConnection {
  asset: Asset
  target: string
  alias: string
}

interface AppProps {
  backend?: Backend
}

const pageSize = 25
const terminalBufferLimit = 1024 * 1024
const disconnectedMessage = 'Connection closed.\r\n\r\nPress Enter to reconnect ...\r\n'
const TerminalPane = lazy(() => import('./components/TerminalPane').then((module) => ({ default: module.TerminalPane })))

function hydrateWorkspace(workspace: Workspace): TabWorkspace {
  return reduceTabs(emptyTabWorkspace, {
    type: 'hydrate',
    workspace: {
      activeTabID: workspace.activeTabId,
      tabs: (workspace.tabs ?? []).map((tab): AppTab => {
        if (tab.type === 'ssh') return {
          id: tab.id,
          kind: 'ssh',
          connectionStatus: 'disconnected',
          descriptor: {
            profile: tab.profile ?? '',
            organization: tab.organization ?? '',
            target: tab.target ?? '',
            account: tab.account ?? '',
            assetID: tab.assetId ?? '',
            assetName: tab.assetName ?? '',
            alias: tab.alias ?? '',
          },
        }
        const kind = tab.type as SingletonTabKind
        return { id: `system:${kind}`, kind } as AppTab
      }),
    },
  })
}

function startupWorkspace(state: BootstrapState): TabWorkspace {
  const restored = hydrateWorkspace(state.workspace)
  const currentProfile = state.profiles.find((item) => item.name === state.currentProfile)
  return currentProfile?.auth.loggedIn
    ? restored
    : reduceTabs(restored, { type: 'open-singleton', kind: 'profiles' })
}

function persistableWorkspace(workspace: TabWorkspace): Workspace {
  return {
    activeTabId: workspace.activeTabID,
    tabs: workspace.tabs.map((tab) => tab.kind === 'ssh' ? {
      id: tab.id,
      type: 'ssh',
      profile: tab.descriptor.profile,
      organization: tab.descriptor.organization,
      target: tab.descriptor.target,
      account: tab.descriptor.account,
      assetId: tab.descriptor.assetID,
      assetName: tab.descriptor.assetName,
      alias: tab.descriptor.alias,
    } : { id: tab.id, type: tab.kind }),
  }
}

function newSSHTabID(): string {
  return globalThis.crypto?.randomUUID?.() ?? `ssh-${Date.now()}-${Math.random().toString(36).slice(2)}`
}

function tabTitle(tab: AppTab): string {
  if (tab.kind === 'ssh') return tab.descriptor.alias || tab.descriptor.assetName || tab.descriptor.target
  return { assets: '资产', profiles: 'Profile', settings: '设置' }[tab.kind]
}

function tabTooltip(tab: SSHTab): string {
  const descriptor = tab.descriptor
  return [
    descriptor.alias ? `Alias: ${descriptor.alias}` : '',
    `Asset: ${descriptor.assetName || descriptor.target}`,
    descriptor.assetID ? `ID: ${descriptor.assetID}` : '',
    `Profile: ${descriptor.profile}`,
    `Organization: ${descriptor.organization}`,
    descriptor.account ? `Account: ${descriptor.account}` : '',
  ].filter(Boolean).join('\n')
}

function AppLogo({ labelled = false, className = '' }: { labelled?: boolean; className?: string }) {
  return <img alt={labelled ? 'JumpAccess 应用图标' : ''} className={`app-logo ${className}`.trim()} src={appIconURL} />
}

function errorMessage(error: unknown): string {
  const message = error instanceof Error ? error.message : String(error)
  return message.startsWith('login required')
    ? '当前 Profile 需要登录，请在 Profile 页面完成认证。'
    : message
}

function accountLabel(account: Account): string {
  return account.username || account.alias || account.name || account.id
}

function formatSyncTime(value: Date | null): string {
  if (!value) return '尚未同步'
  return value.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', second: '2-digit' })
}

function filterQuickAssets(assets: Asset[], query: string, limit = 20): Asset[] {
  const normalizedQuery = query.trim().toLowerCase()
  if (!normalizedQuery) return assets.slice(0, limit)
  return assets.filter((asset) => [
    asset.id,
    asset.name,
    asset.address,
    ...asset.aliases.map((alias) => alias.name),
  ].some((value) => value.toLowerCase().includes(normalizedQuery))).slice(0, limit)
}

function authPresentation(auth?: AuthStatus): { description: string; offline: boolean; title: string } {
  if (!auth?.loggedIn) return { title: '需要登录', description: '打开 Profile 管理', offline: true }
  const remaining = auth.expiresAt ? new Date(auth.expiresAt).getTime() - Date.now() : Number.POSITIVE_INFINITY
  if (auth.expired || remaining <= 0) {
    return auth.refreshAvailable
      ? { title: '已认证', description: '可自动续期', offline: false }
      : { title: '认证已过期', description: '请重新登录', offline: true }
  }
  return { title: '已认证', description: auth.expiresAt ? `${Math.max(1, Math.round(remaining / 60_000))} 分钟后到期` : 'Token 有效', offline: false }
}

function useDismissiblePopover(open: boolean, onDismiss: () => void) {
  const root = useRef<HTMLDivElement>(null)
  const dismiss = useRef(onDismiss)
  dismiss.current = onDismiss
  useEffect(() => {
    if (!open) return
    const onPointerDown = (event: PointerEvent) => {
      if (!root.current?.contains(event.target as Node)) dismiss.current()
    }
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') dismiss.current()
    }
    document.addEventListener('pointerdown', onPointerDown)
    document.addEventListener('keydown', onKeyDown)
    return () => {
      document.removeEventListener('pointerdown', onPointerDown)
      document.removeEventListener('keydown', onKeyDown)
    }
  }, [open])
  return root
}

function ContextSelect({ ariaLabel, icon, label, onChange, options, value }: {
  ariaLabel: string
  icon?: ReactNode
  label: string
  onChange: (value: string) => void
  options: { value: string; label: string }[]
  value: string
}) {
  const [open, setOpen] = useState(false)
  const root = useDismissiblePopover(open, () => setOpen(false))
  const selected = options.find((option) => option.value === value)
  const selectedLabel = selected?.label || value || '未选择'
  return <div className="context-select" ref={root}>{icon ? <span className="context-icon">{icon}</span> : null}<div className="context-select-control"><button aria-expanded={open} aria-haspopup="listbox" aria-label={`${ariaLabel}：${selectedLabel}`} className="context-trigger" onClick={() => setOpen((current) => !current)} type="button"><span><small>{label}</small><strong>{selectedLabel}</strong></span><ChevronDown /></button>{open ? <div aria-label={ariaLabel} className="context-options" role="listbox">{options.map((option) => <button aria-selected={option.value === value} className={option.value === value ? 'selected' : ''} key={option.value} onClick={() => { setOpen(false); if (option.value !== value) onChange(option.value) }} role="option" type="button"><span>{option.label}</span>{option.value === value ? <span className="selected-mark">✓</span> : null}</button>)}</div> : null}</div></div>
}

function AliasFilterMenu({ onChange, value }: { onChange: (value: AliasFilter) => void; value: AliasFilter }) {
  const [open, setOpen] = useState(false)
  const root = useDismissiblePopover(open, () => setOpen(false))
  return <div className="filter-menu" ref={root}><button aria-expanded={open} aria-haspopup="true" aria-label="筛选" className="button secondary" onClick={() => setOpen((current) => !current)} type="button"><SlidersHorizontal />筛选{value !== 'all' ? <em>1</em> : null}<ChevronDown /></button>{open ? <div className="popover filter-popover" role="group" aria-label="当前页 Alias 筛选"><strong>当前页 Alias 状态</strong>{([['all', '全部'], ['with-alias', '已有 Alias'], ['without-alias', '未创建 Alias']] as const).map(([filter, label]) => <label key={filter}><input type="radio" name="alias-filter" checked={value === filter} onChange={() => { onChange(filter); setOpen(false) }} />{label}</label>)}</div> : null}</div>
}

export default function App({ backend = wailsBackend }: AppProps) {
  const searchRef = useRef<HTMLInputElement>(null)
  const [bootstrap, setBootstrap] = useState<BootstrapState | null>(null)
  const [workspace, setWorkspace] = useState<TabWorkspace>(emptyTabWorkspace)
  const workspaceRef = useRef<TabWorkspace>(emptyTabWorkspace)
  const workspaceReady = useRef(false)
  const workspaceSaveQueue = useRef(Promise.resolve())
  const connectionAttempts = useRef(new Map<string, symbol>())
  const pendingSessionStates = useRef(new Map<string, SessionState>())
  const [profile, setProfile] = useState('')
  const [organization, setOrganization] = useState('')
  const [organizations, setOrganizations] = useState<Organization[]>([])
  const [assets, setAssets] = useState<AssetPage>({ count: 0, offset: 0, limit: pageSize, aliasCount: 0, results: [] })
  const [details, setDetails] = useState<Record<string, AssetDetail>>({})
  const [selectedAssetID, setSelectedAssetID] = useState('')
  const [search, setSearch] = useState('')
  const [debouncedSearch, setDebouncedSearch] = useState('')
  const [offset, setOffset] = useState(0)
  const [refreshKey, setRefreshKey] = useState(0)
  const [refreshing, setRefreshing] = useState(false)
  const [lastSynced, setLastSynced] = useState<Date | null>(null)
  const [aliasFilter, setAliasFilter] = useState<AliasFilter>('all')
  const [sessionDirectories, setSessionDirectories] = useState<Record<string, string>>({})
  const [sessionLatencies, setSessionLatencies] = useState<Record<string, SessionLatency>>({})
  const [sessionOutput, setSessionOutput] = useState<Record<string, string>>({})
  const [error, setError] = useState('')
  const [aliasAsset, setAliasAsset] = useState<Asset | null>(null)
  const [pendingConnection, setPendingConnection] = useState<PendingConnection | null>(null)
  const [quickOpen, setQuickOpen] = useState(false)
  const [quickQuery, setQuickQuery] = useState('')
  const [quickResults, setQuickResults] = useState<Asset[]>([])
  const [profileDialog, setProfileDialog] = useState(false)
  const [editingProfile, setEditingProfile] = useState<ProfileSummary | null>(null)
  const [loginAttempt, setLoginAttempt] = useState<LoginAttempt | null>(null)
  const [licenseOpen, setLicenseOpen] = useState(false)
  const [licenseText, setLicenseText] = useState('')
  const [hostKeyPrompt, setHostKeyPrompt] = useState<HostKeyPrompt | null>(null)
  const [pendingDisconnect, setPendingDisconnect] = useState<SSHTab | null>(null)
  const [pendingProfileLogout, setPendingProfileLogout] = useState<ProfileSummary | null>(null)
  const [pendingProfileDeletion, setPendingProfileDeletion] = useState<ProfileSummary | null>(null)
  const [terminalFontFamilies, setTerminalFontFamilies] = useState<string[]>([])
  const [terminalFontFamiliesLoaded, setTerminalFontFamiliesLoaded] = useState(false)

  const preferences = bootstrap?.preferences
  const currentProfile = bootstrap?.profiles.find((item) => item.name === profile)
  const currentProfileLoggedIn = currentProfile?.auth.loggedIn === true
  const currentAuth = authPresentation(currentProfile?.auth)
  const selectedAsset = assets.results.find((asset) => asset.id === selectedAssetID) ?? assets.results[0]
  const selectedDetail = selectedAsset ? details[selectedAsset.id] : undefined
  const activeTab = workspace.tabs.find((tab) => tab.id === workspace.activeTabID)
  const filteredAssets = useMemo(() => assets.results.filter((asset) => {
    if (aliasFilter === 'with-alias') return asset.aliases.length > 0
    if (aliasFilter === 'without-alias') return asset.aliases.length === 0
    return true
  }), [aliasFilter, assets.results])
  const localQuickResults = useMemo(
    () => filterQuickAssets(assets.results, quickQuery),
    [assets.results, quickQuery],
  )
  const displayedQuickResults = assets.results.length > 0 ? localQuickResults : quickResults

  function dispatchTabs(action: TabAction) {
    const next = reduceTabs(workspaceRef.current, action)
    workspaceRef.current = next
    setWorkspace(next)
  }

  function openSingleton(kind: SingletonTabKind) {
    dispatchTabs({ type: 'open-singleton', kind })
  }

  function appendDisconnectedPrompt(tabID: string) {
    setSessionOutput((current) => {
      const previous = current[tabID] ?? ''
      const output = previous.endsWith(disconnectedMessage)
        ? previous
        : `${previous}${previous ? '\r\n' : ''}${disconnectedMessage}`
      return { ...current, [tabID]: output.slice(-terminalBufferLimit) }
    })
  }

  function clearSessionDirectory(tabID: string) {
    setSessionDirectories((current) => {
      if (!(tabID in current)) return current
      const next = { ...current }
      delete next[tabID]
      return next
    })
  }

  function clearSessionLatency(sessionID: string) {
    if (!sessionID) return
    setSessionLatencies((current) => {
      if (!(sessionID in current)) return current
      const next = { ...current }
      delete next[sessionID]
      return next
    })
  }

  function applySessionState(event: SessionState, tabID: string) {
    if (event.status !== 'active' && event.status !== 'closed' && event.status !== 'failed') return
    dispatchTabs({ type: 'session-state', sessionID: event.id, status: event.status, error: event.error })
    if (event.status === 'closed' || event.status === 'failed') {
      clearSessionLatency(event.id)
      clearSessionDirectory(tabID)
      appendDisconnectedPrompt(tabID)
      void backend.closeSSHSession(event.id).catch(() => undefined)
    }
  }

  useEffect(() => {
    let cancelled = false
    backend.bootstrap()
      .then((state) => {
        if (cancelled) return
        const restored = startupWorkspace(state)
        setBootstrap(state)
        setProfile(state.currentProfile)
        setOrganization(state.currentOrganization)
        workspaceRef.current = restored
        setWorkspace(restored)
        setSessionOutput(Object.fromEntries(restored.tabs
          .filter((tab): tab is SSHTab => tab.kind === 'ssh')
          .map((tab) => [tab.id, disconnectedMessage])))
        setSessionDirectories({})
        setSessionLatencies({})
        workspaceReady.current = true
      })
      .catch((reason) => !cancelled && setError(errorMessage(reason)))
    const offState = backend.onSessionState((event) => {
      const tab = workspaceRef.current.tabs.find((item): item is SSHTab => item.kind === 'ssh' && item.sessionID === event.id)
      if (!tab) {
        pendingSessionStates.current.set(event.id, event)
        if (pendingSessionStates.current.size > 64) {
          const oldest = pendingSessionStates.current.keys().next().value
          if (oldest) pendingSessionStates.current.delete(oldest)
        }
        return
      }
      applySessionState(event, tab.id)
    })
    const offOutput = backend.onSessionOutput((event) => {
      const tab = workspaceRef.current.tabs.find((item): item is SSHTab => item.kind === 'ssh' && item.sessionID === event.id)
      if (!tab) return
      setSessionOutput((current) => ({
        ...current,
        [tab.id]: ((current[tab.id] ?? '') + event.data).slice(-terminalBufferLimit),
      }))
    })
    const offLatency = backend.onSessionLatency((event) => {
      setSessionLatencies((current) => ({ ...current, [event.id]: event }))
    })
    const offHostKey = backend.onHostKeyPrompt(setHostKeyPrompt)
    return () => {
      cancelled = true
      offState()
      offOutput()
      offLatency()
      offHostKey()
      connectionAttempts.current.clear()
      pendingSessionStates.current.clear()
    }
  }, [backend])

  useEffect(() => {
    workspaceRef.current = workspace
    if (!workspaceReady.current) return
    const snapshot = persistableWorkspace(workspace)
    workspaceSaveQueue.current = workspaceSaveQueue.current
      .catch(() => undefined)
      .then(() => backend.saveWorkspace(snapshot))
      .catch((reason) => setError(errorMessage(reason)))
  }, [backend, workspace])

  useEffect(() => {
    const timer = window.setTimeout(() => {
      setDebouncedSearch(search.trim())
      setOffset(0)
    }, 220)
    return () => window.clearTimeout(timer)
  }, [search])

  useEffect(() => {
    if (!profile || !currentProfileLoggedIn) {
      setOrganizations([])
      return
    }
    let cancelled = false
    backend.listOrganizations(profile)
      .then((values) => !cancelled && setOrganizations(values))
      .catch((reason) => !cancelled && setError(errorMessage(reason)))
    return () => { cancelled = true }
  }, [backend, currentProfileLoggedIn, profile])

  useEffect(() => {
    if (!profile || !organization || !currentProfileLoggedIn) {
      setAssets({ count: 0, offset: 0, limit: pageSize, aliasCount: 0, results: [] })
      setRefreshing(false)
      return
    }
    let cancelled = false
    setRefreshing(true)
    backend.listAssets({ profile, organization, search: debouncedSearch, offset, limit: pageSize })
      .then((page) => {
        if (cancelled) return
        setAssets(page)
        setLastSynced(new Date())
        setSelectedAssetID((current) => page.results.some((asset) => asset.id === current) ? current : (page.results[0]?.id ?? ''))
        void syncProfileAuth(profile).catch((reason) => !cancelled && setError(errorMessage(reason)))
      })
      .catch((reason) => !cancelled && setError(errorMessage(reason)))
      .finally(() => !cancelled && setRefreshing(false))
    return () => { cancelled = true }
  }, [backend, currentProfileLoggedIn, debouncedSearch, offset, organization, profile, refreshKey])

  useEffect(() => {
    if (!selectedAsset || details[selectedAsset.id] || !profile || !organization || !currentProfileLoggedIn) return
    let cancelled = false
    backend.getAsset({ profile, organization, asset: selectedAsset.id })
      .then((detail) => !cancelled && setDetails((current) => ({ ...current, [detail.id]: detail })))
      .catch((reason) => !cancelled && setError(errorMessage(reason)))
    return () => { cancelled = true }
  }, [backend, currentProfileLoggedIn, details, organization, profile, selectedAsset])

  useEffect(() => {
    const theme = preferences?.theme ?? 'system'
    const media = window.matchMedia('(prefers-color-scheme: dark)')
    const apply = () => {
      const dark = theme === 'dark' || (theme === 'system' && media.matches)
      document.documentElement.classList.toggle('dark', dark)
      document.documentElement.style.colorScheme = dark ? 'dark' : 'light'
    }
    apply()
    media.addEventListener('change', apply)
    return () => media.removeEventListener('change', apply)
  }, [preferences?.theme])

  useEffect(() => {
    if (activeTab?.kind !== 'settings' || terminalFontFamiliesLoaded) return
    let cancelled = false
    backend.listMonospaceFonts()
      .then((families) => {
        if (!cancelled) setTerminalFontFamilies(families)
      })
      .catch(() => {
        if (!cancelled) setTerminalFontFamilies([])
      })
      .finally(() => {
        if (!cancelled) setTerminalFontFamiliesLoaded(true)
      })
    return () => { cancelled = true }
  }, [activeTab?.kind, backend, terminalFontFamiliesLoaded])

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'k') {
        event.preventDefault()
        setQuickOpen(true)
      } else if (event.key === '/' && activeTab?.kind === 'assets' && !(event.target instanceof HTMLInputElement)) {
        event.preventDefault()
        searchRef.current?.focus()
      }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [activeTab?.kind])

  useEffect(() => {
    if (!quickOpen || !profile || !organization || !currentProfileLoggedIn || assets.results.length > 0) return
    const timer = window.setTimeout(() => {
      backend.quickSearch({ profile, organization, query: quickQuery.trim(), limit: 20 })
        .then(setQuickResults)
        .catch((reason) => setError(errorMessage(reason)))
    }, 160)
    return () => window.clearTimeout(timer)
  }, [assets.results.length, backend, currentProfileLoggedIn, organization, profile, quickOpen, quickQuery])

  async function reloadBootstrap(preferredProfile?: string) {
    const state = await backend.bootstrap()
    setBootstrap(state)
    const nextProfile = preferredProfile ?? state.currentProfile
    setProfile(nextProfile)
    setOrganization(state.profiles.find((item) => item.name === nextProfile)?.organization ?? state.currentOrganization)
  }

  async function run(action: () => Promise<void>) {
    try {
      setError('')
      await action()
    } catch (reason) {
      setError(errorMessage(reason))
    }
  }

  async function addProfile(name: string, url: string) {
    setError('')
    await backend.addProfile(name, url)
    setProfileDialog(false)
    void run(async () => {
      await reloadBootstrap()
      setLoginAttempt(await backend.startLogin(name))
    })
  }

  async function updateProfileURL(item: ProfileSummary, url: string) {
    setError('')
    await backend.updateProfileURL(item.name, url)
    if (item.name === profile) {
      setAssets({ count: 0, offset: 0, limit: pageSize, aliasCount: 0, results: [] })
      setDetails({})
      setSelectedAssetID('')
      setLastSynced(null)
    }
    setEditingProfile(null)
    void run(() => reloadBootstrap())
  }

  async function syncProfileAuth(profileName: string) {
    const auth = await backend.getAuthStatus(profileName)
    setBootstrap((current) => current ? {
      ...current,
      profiles: current.profiles.map((item) => item.name === profileName ? { ...item, auth } : item),
    } : current)
  }

  async function ensureDetail(asset: Asset): Promise<AssetDetail> {
    const cached = details[asset.id]
    if (cached) return cached
    const detail = await backend.getAsset({ profile, organization, asset: asset.id })
    setDetails((current) => ({ ...current, [detail.id]: detail }))
    return detail
  }

  async function connectAsset(asset: Asset) {
    await run(async () => {
      const detail = await ensureDetail(asset)
      if (detail.accounts.length === 0) throw new Error('该资产没有可用于 SSH 的账号。')
      if (detail.accounts.length === 1) {
        await startConnection(asset, asset.id, '', detail.accounts[0].id || detail.accounts[0].username)
        return
      }
      setPendingConnection({ asset, target: asset.id, alias: '' })
    })
  }

  async function connectAlias(asset: Asset, alias: Alias) {
    await run(async () => {
      if (alias.account) {
        await startConnection(asset, alias.name, alias.name, alias.account)
        return
      }
      const detail = await ensureDetail(asset)
      if (detail.accounts.length === 0) throw new Error('该资产没有可用于 SSH 的账号。')
      if (detail.accounts.length === 1) {
        await startConnection(asset, alias.name, alias.name, detail.accounts[0].id || detail.accounts[0].username)
        return
      }
      setPendingConnection({ asset, target: alias.name, alias: alias.name })
    })
  }

  async function startConnection(asset: Asset, target: string, alias: string, account: string, existingTabID?: string) {
    const descriptor: SSHDescriptor = {
      profile,
      organization,
      target,
      alias,
      assetID: asset.id,
      assetName: asset.name,
      account,
    }
    const tabID = existingTabID ?? newSSHTabID()
    if (!existingTabID) dispatchTabs({ type: 'open-ssh', id: tabID, descriptor })
    await beginSSHConnection(tabID, descriptor, false)
    void syncProfileAuth(profile).catch((reason) => setError(errorMessage(reason)))
    setPendingConnection(null)
    setQuickOpen(false)
  }

  async function reconnectTab(tab: SSHTab) {
    const current = workspaceRef.current.tabs.find((item): item is SSHTab => item.id === tab.id && item.kind === 'ssh')
    if (!current || current.connectionStatus === 'active' || current.connectionStatus === 'connecting' || current.connectionStatus === 'reconnecting') return
    await run(async () => {
      await beginSSHConnection(current.id, current.descriptor, true)
    })
  }

  async function beginSSHConnection(tabID: string, descriptor: SSHDescriptor, reconnecting: boolean) {
    const attempt = Symbol(tabID)
    connectionAttempts.current.set(tabID, attempt)
    dispatchTabs({ type: 'begin-connection', tabID, reconnecting })
    clearSessionDirectory(tabID)
    setSessionOutput((current) => ({ ...current, [tabID]: '' }))
    try {
      const session = await backend.startSSHSession({
        profile: descriptor.profile,
        organization: descriptor.organization,
        target: descriptor.target,
        account: descriptor.account,
        columns: 120,
        rows: 34,
      })
      const tabStillExists = workspaceRef.current.tabs.some((item) => item.id === tabID && item.kind === 'ssh')
      if (connectionAttempts.current.get(tabID) !== attempt || !tabStillExists) {
        pendingSessionStates.current.delete(session.id)
        await backend.closeSSHSession(session.id)
        clearSessionLatency(session.id)
        return
      }
      dispatchTabs({ type: 'attach-session', tabID, sessionID: session.id })
      const pendingState = pendingSessionStates.current.get(session.id)
      pendingSessionStates.current.delete(session.id)
      if (pendingState) {
        applySessionState(pendingState, tabID)
      } else {
        applySessionState(session, tabID)
      }
      connectionAttempts.current.delete(tabID)
    } catch (reason) {
      const isCurrent = connectionAttempts.current.get(tabID) === attempt
      connectionAttempts.current.delete(tabID)
      const tabStillExists = workspaceRef.current.tabs.some((item) => item.id === tabID && item.kind === 'ssh')
      if (!isCurrent || !tabStillExists) return
      dispatchTabs({ type: 'connection-error', tabID, error: errorMessage(reason) })
      appendDisconnectedPrompt(tabID)
      throw reason
    }
  }

  async function changeAliasAccount(alias: Alias, account: string) {
    await run(async () => {
      await backend.setAliasAccount({ profile, name: alias.name, account })
      setAssets((current) => ({
        ...current,
        results: current.results.map((asset) => ({
          ...asset,
          aliases: asset.aliases.map((item) => item.name === alias.name ? { ...item, account } : item),
        })),
      }))
      setDetails((current) => Object.fromEntries(Object.entries(current).map(([id, detail]) => [id, {
        ...detail,
        aliases: detail.aliases.map((item) => item.name === alias.name ? { ...item, account } : item),
      }])))
    })
  }

  function adjustProfileAliasCount(delta: number) {
    setBootstrap((current) => current ? {
      ...current,
      profiles: current.profiles.map((item) => item.name === profile ? { ...item, aliasCount: Math.max(0, item.aliasCount + delta) } : item),
    } : current)
  }

  async function createAliasForAsset(asset: Asset, name: string, account: string) {
    await run(async () => {
      const created = await backend.createAlias({ profile, asset: asset.id, name, account })
      setAssets((current) => ({
        ...current,
        aliasCount: current.aliasCount + 1,
        results: current.results.map((item) => item.id === asset.id ? { ...item, aliases: [...item.aliases, created].sort((left, right) => left.name.localeCompare(right.name)) } : item),
      }))
      setDetails((current) => {
        const detail = current[asset.id]
        return detail ? { ...current, [asset.id]: { ...detail, aliases: [...detail.aliases, created].sort((left, right) => left.name.localeCompare(right.name)) } } : current
      })
      adjustProfileAliasCount(1)
      setAliasAsset(null)
    })
  }

  async function deleteAlias(alias: Alias) {
    if (!window.confirm(`确定删除 Alias “${alias.name}”吗？`)) return
    await run(async () => {
      await backend.deleteAlias(profile, alias.name)
      setAssets((current) => ({
        ...current,
        aliasCount: Math.max(0, current.aliasCount - 1),
        results: current.results.map((asset) => ({ ...asset, aliases: asset.aliases.filter((item) => item.name !== alias.name) })),
      }))
      setDetails((current) => Object.fromEntries(Object.entries(current).map(([id, detail]) => [id, { ...detail, aliases: detail.aliases.filter((item) => item.name !== alias.name) }])))
      adjustProfileAliasCount(-1)
    })
  }

  function requestCloseTab(tab: AppTab) {
    if (tab.kind === 'ssh' && preferences?.confirmCloseActiveSession && (tab.connectionStatus === 'active' || tab.connectionStatus === 'connecting' || tab.connectionStatus === 'reconnecting')) {
      setPendingDisconnect(tab)
      return
    }
    void closeTab(tab)
  }

  async function disconnectTab(tab: SSHTab) {
    const current = workspaceRef.current.tabs.find((item): item is SSHTab => item.id === tab.id && item.kind === 'ssh')
    const sessionID = current?.sessionID
    if (!current || current.connectionStatus !== 'active' || !sessionID) return
    await run(async () => {
      await backend.closeSSHSession(sessionID)
    })
  }

  async function closeTab(tab: AppTab) {
    await run(async () => {
      connectionAttempts.current.delete(tab.id)
      const currentTab = workspaceRef.current.tabs.find((item) => item.id === tab.id)
      if (!currentTab) {
        setPendingDisconnect(null)
        return
      }
      if (currentTab.kind === 'ssh' && currentTab.sessionID) await backend.closeSSHSession(currentTab.sessionID)
      if (currentTab.kind === 'ssh' && currentTab.sessionID) clearSessionLatency(currentTab.sessionID)
      dispatchTabs({ type: 'close', id: currentTab.id })
      if (currentTab.kind === 'ssh') setSessionOutput((current) => {
        const next = { ...current }
        delete next[currentTab.id]
        return next
      })
      if (currentTab.kind === 'ssh') clearSessionDirectory(currentTab.id)
      setPendingDisconnect(null)
    })
  }

  async function deleteProfile(item: ProfileSummary) {
    await run(async () => {
      await backend.deleteProfile(item.name)
      const removedTabs = workspaceRef.current.tabs.filter((tab): tab is SSHTab => tab.kind === 'ssh' && tab.descriptor.profile === item.name)
      removedTabs.forEach((tab) => connectionAttempts.current.delete(tab.id))
      const removedTabIDs = new Set(removedTabs.map((tab) => tab.id))
      dispatchTabs({ type: 'drop-profile', profile: item.name })
      setSessionOutput((current) => Object.fromEntries(Object.entries(current).filter(([id]) => !removedTabIDs.has(id))))
      setSessionDirectories((current) => Object.fromEntries(Object.entries(current).filter(([id]) => !removedTabIDs.has(id))))
      const removedSessionIDs = new Set(removedTabs.flatMap((tab) => tab.sessionID ? [tab.sessionID] : []))
      setSessionLatencies((current) => Object.fromEntries(Object.entries(current).filter(([id]) => !removedSessionIDs.has(id))))
      setPendingProfileDeletion(null)
      setDetails({})
      setSelectedAssetID('')
      await reloadBootstrap()
    })
  }

  async function logoutProfile(item: ProfileSummary) {
    setError('')
    await backend.logout(item.name)
    if (item.name === profile) {
      setAssets({ count: 0, offset: 0, limit: pageSize, aliasCount: 0, results: [] })
      setDetails({})
      setSelectedAssetID('')
      setLastSynced(null)
    }
    await reloadBootstrap()
    setPendingProfileLogout(null)
  }

  async function savePreferences(next: Preferences) {
    if (!bootstrap) return
    const previous = bootstrap.preferences
    setBootstrap({ ...bootstrap, preferences: next })
    try {
      await backend.savePreferences(next)
    } catch (reason) {
      setBootstrap({ ...bootstrap, preferences: previous })
      setError(errorMessage(reason))
    }
  }

  async function quitApplication() {
    await workspaceSaveQueue.current.catch(() => undefined)
    window.runtime?.Quit?.()
  }

  if (!bootstrap) {
    return <main className="loading-shell"><div className="brand-mark"><AppLogo /></div><h1>JumpAccess</h1><p>{error || '正在连接桌面服务…'}</p></main>
  }

  return (
    <main className={`app-shell ${navigator.platform.toLowerCase().includes('mac') ? 'mac' : 'windows'}`}>
      <TitleBar
        activeTabID={workspace.activeTabID}
        auth={currentAuth}
        onActivate={(id) => dispatchTabs({ type: 'activate', id })}
        onClose={requestCloseTab}
        onOpenQuick={() => setQuickOpen(true)}
        onOpenSingleton={openSingleton}
        onMove={(id, toIndex) => dispatchTabs({ type: 'move', id, toIndex })}
        onQuit={() => void quitApplication()}
        profile={profile}
        showTabCloseButtons={bootstrap.preferences.showTabCloseButtons}
        tabs={workspace.tabs}
      />
      <section className="workspace">
        {activeTab?.kind === 'assets' ? <header className="asset-context-bar">
          <div className="context-switchers">
            <ContextSelect ariaLabel="当前 Profile" icon={<Server />} label="Profile" onChange={(value) => void run(async () => { await backend.useProfile(value); await reloadBootstrap(value); setOffset(0); setDetails({}) })} options={bootstrap.profiles.map((item) => ({ value: item.name, label: item.name }))} value={profile} />
            <div className="context-divider" />
            <ContextSelect ariaLabel="当前 Organization" label="Organization" onChange={(value) => void run(async () => { await backend.setOrganization(profile, value); setOrganization(value); setOffset(0); setDetails({}); await reloadBootstrap(profile) })} options={organizations.map((item) => ({ value: item.id, label: item.name }))} value={organization} />
          </div>
        </header> : null}

        {error ? <div className="error-banner" role="alert"><ShieldAlert /><span>{error}</span><button aria-label="关闭错误提示" onClick={() => setError('')}><X /></button></div> : null}

        {!activeTab ? <StartPage onAction={(action) => action === 'quick' ? setQuickOpen(true) : openSingleton(action)} /> : null}

        {activeTab?.kind === 'assets' ? (
          <div className="content">
            <section className="asset-pane">
              <PageHeading eyebrow="资源发现" title="资产" description="浏览当前 Organization 中有权访问的资产，并直接建立 SSH 会话。"><div className="refresh-controls"><span className="last-refreshed"><Clock3 />最近同步 {formatSyncTime(lastSynced)}</span><button className="button secondary" disabled={refreshing || !organization} onClick={() => setRefreshKey((value) => value + 1)} type="button"><RefreshCcw className={refreshing ? 'spin' : ''} />{refreshing ? '同步中…' : '立即同步'}</button></div></PageHeading>
              {!profile ? <EmptyState title="尚未创建 Profile" action="添加 Profile" onAction={() => { openSingleton('profiles'); setProfileDialog(true) }} /> : <>
                <div className="asset-toolbar"><label className="search-box"><Search /><input ref={searchRef} role="searchbox" aria-label="搜索资产或 Alias" value={search} onChange={(event) => setSearch(event.target.value)} placeholder="搜索名称、地址、Asset ID 或 Alias" /><kbd>/</kbd></label><AliasFilterMenu onChange={setAliasFilter} value={aliasFilter} /></div>
                <div className="asset-table-card"><table><thead><tr><th>资产 ({assets.count})</th><th>类型</th><th>Alias ({assets.aliasCount})</th><th aria-label="操作" /></tr></thead><tbody>{filteredAssets.map((asset) => <AssetRow asset={asset} detail={details[asset.id]} key={asset.id} onBind={(alias, account) => void changeAliasAccount(alias, account)} onConnect={() => void connectAsset(asset)} onConnectAlias={(alias) => void connectAlias(asset, alias)} onCreateAlias={() => { setSelectedAssetID(asset.id); setAliasAsset(asset) }} onDeleteAlias={(alias) => void deleteAlias(alias)} onEnsureDetail={() => void run(async () => { await ensureDetail(asset) })} onSelect={() => setSelectedAssetID(asset.id)} selected={asset.id === selectedAsset?.id} />)}</tbody></table>{filteredAssets.length === 0 ? <div className="table-empty"><Search /><strong>没有符合条件的资产</strong><span>请调整搜索、筛选或 Organization。</span></div> : null}{assets.count > pageSize ? <div className="table-footer"><span>{offset + 1}–{Math.min(offset + assets.results.length, assets.count)} / {assets.count}</span><div><button className="button secondary small" disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - pageSize))}>上一页</button><button className="button secondary small" disabled={offset + assets.results.length >= assets.count} onClick={() => setOffset(offset + pageSize)}>下一页</button></div></div> : null}</div>
              </>}
            </section>
            {selectedAsset ? <AssetDetailPane asset={selectedAsset} detail={selectedDetail} onConnect={() => void connectAsset(selectedAsset)} onCopy={(value) => void navigator.clipboard?.writeText(value)} onCreateAlias={() => setAliasAsset(selectedAsset)} /> : <aside className="detail-pane empty-detail"><Server /><span>选择一项资产查看详情</span></aside>}
          </div>
        ) : null}

        {activeTab?.kind === 'ssh' ? <SSHView backend={backend} currentDirectory={sessionDirectories[activeTab.id] ?? ''} latency={activeTab.sessionID ? sessionLatencies[activeTab.sessionID] : undefined} onCurrentDirectoryChange={(directory) => setSessionDirectories((current) => current[activeTab.id] === directory ? current : { ...current, [activeTab.id]: directory })} onDisconnect={() => void disconnectTab(activeTab)} onReconnect={() => void reconnectTab(activeTab)} output={sessionOutput[activeTab.id] ?? disconnectedMessage} preferences={bootstrap.preferences} tab={activeTab} /> : null}

        {activeTab?.kind === 'profiles' ? <section className="full-pane"><PageHeading eyebrow="连接上下文" title="Profile" description="管理 JumpServer 站点、认证状态和默认 Organization。"><button className="button primary" onClick={() => setProfileDialog(true)}><Plus />添加 Profile</button></PageHeading><div className="profile-grid">{bootstrap.profiles.map((item) => <article className={item.name === profile ? 'profile-card current' : 'profile-card'} key={item.name}><div className="profile-card-top"><div className="profile-icon"><Layers3 /></div>{item.name === profile ? <span className="badge">当前</span> : <span className="badge outline">备用</span>}</div><h2>{item.name}</h2><dl><div><dt>Organization</dt><dd>{organizations.find((org) => org.id === item.organization)?.name || item.organization || '未设置'}</dd></div><div><dt>认证</dt><dd className={item.auth.loggedIn ? 'auth-ok' : 'auth-warn'}>{item.auth.loggedIn ? <><span className="status-dot" />已认证</> : <><ShieldAlert />需要登录</>}</dd></div><div><dt>Server URL</dt><dd className="profile-server-url" title={item.url}><span>{item.url}</span><button aria-label={`复制 ${item.name} Server URL`} className="profile-url-copy" onClick={() => void navigator.clipboard?.writeText(item.url)} title="复制 Server URL" type="button"><Copy /></button></dd></div></dl><div className="profile-card-actions">{item.name !== profile ? <button className="button secondary small" onClick={() => void run(async () => { await backend.useProfile(item.name); await reloadBootstrap(item.name) })}>设为当前</button> : null}{item.auth.loggedIn ? <><button className="button ghost small" onClick={() => void run(async () => { await backend.refreshAuth(item.name); await reloadBootstrap(item.name) })}><RefreshCcw />刷新认证</button><button className="button ghost small danger" onClick={() => setPendingProfileLogout(item)}><LogOut />退出</button></> : <button className="button primary small" onClick={() => void run(async () => setLoginAttempt(await backend.startLogin(item.name)))}><LogIn />登录</button>}<button aria-label={`编辑 ${item.name} Profile`} className="button ghost small" onClick={() => setEditingProfile(item)}><Pencil />编辑</button><button aria-label={`删除 ${item.name} Profile`} className="button ghost small danger" onClick={() => setPendingProfileDeletion(item)}><Trash2 />删除</button></div></article>)}{bootstrap.profiles.length === 0 ? <EmptyState title="尚未创建 Profile" action="添加 Profile" onAction={() => setProfileDialog(true)} /> : null}</div></section> : null}

        {activeTab?.kind === 'settings' ? <SettingsView fontFamilies={terminalFontFamilies} onLicense={() => void run(async () => { setLicenseText(await backend.licenseText()); setLicenseOpen(true) })} onOpenConfig={() => void run(backend.openConfig)} onSave={(next) => void savePreferences(next)} preferences={bootstrap.preferences} version={bootstrap.version} /> : null}
      </section>

      {aliasAsset ? <AliasDialog asset={aliasAsset} detail={details[aliasAsset.id]} onCancel={() => setAliasAsset(null)} onEnsure={() => ensureDetail(aliasAsset)} onSave={(name, account) => void createAliasForAsset(aliasAsset, name, account)} /> : null}
      {pendingConnection ? <AccountDialog asset={pendingConnection.asset} accounts={details[pendingConnection.asset.id]?.accounts ?? []} onCancel={() => setPendingConnection(null)} onChoose={(account) => void run(() => startConnection(pendingConnection.asset, pendingConnection.target, pendingConnection.alias, account.id || account.username))} /> : null}
      {quickOpen ? <QuickConnectDialog assets={displayedQuickResults} onCancel={() => { setQuickOpen(false); setQuickQuery('') }} onConnectAsset={(asset) => void connectAsset(asset)} onConnectAlias={(asset, alias) => void connectAlias(asset, alias)} query={quickQuery} setQuery={setQuickQuery} /> : null}
      {profileDialog ? <ProfileDialog onCancel={() => setProfileDialog(false)} onSave={addProfile} /> : null}
      {editingProfile ? <EditProfileDialog profile={editingProfile} onCancel={() => setEditingProfile(null)} onSave={(url) => updateProfileURL(editingProfile, url)} /> : null}
      {loginAttempt ? <LoginDialog attempt={loginAttempt} onCancel={() => void run(async () => { await backend.cancelLogin(loginAttempt.id); setLoginAttempt(null) })} onComplete={(callback) => void run(async () => { await backend.completeLogin(loginAttempt.id, callback); setLoginAttempt(null); await reloadBootstrap(); setDetails({}); setRefreshKey((value) => value + 1) })} /> : null}
      {licenseOpen ? <Modal title="开源许可证" description="JumpAccess 及随附第三方组件的许可证信息。" onClose={() => setLicenseOpen(false)}><pre className="license-text">{licenseText}</pre><div className="dialog-actions"><button className="button primary" onClick={() => setLicenseOpen(false)}>关闭</button></div></Modal> : null}
      {hostKeyPrompt ? <HostKeyDialog prompt={hostKeyPrompt} onDecision={(accepted) => void run(async () => { await backend.resolveSSHHostKey(hostKeyPrompt.id, accepted); setHostKeyPrompt(null) })} /> : null}
      {pendingDisconnect ? <DisconnectSessionDialog tab={pendingDisconnect} onCancel={() => setPendingDisconnect(null)} onConfirm={() => void closeTab(pendingDisconnect)} /> : null}
      {pendingProfileLogout ? <LogoutProfileDialog profile={pendingProfileLogout} onCancel={() => setPendingProfileLogout(null)} onConfirm={() => logoutProfile(pendingProfileLogout)} /> : null}
      {pendingProfileDeletion ? <DeleteProfileDialog profile={pendingProfileDeletion} onCancel={() => setPendingProfileDeletion(null)} onConfirm={() => void deleteProfile(pendingProfileDeletion)} /> : null}
    </main>
  )
}

function tabIcon(tab: AppTab) {
  if (tab.kind === 'assets') return <Boxes />
  if (tab.kind === 'profiles') return <Layers3 />
  if (tab.kind === 'settings') return <Settings />
  return <TerminalSquare />
}

function TitleBar({ activeTabID, auth, onActivate, onClose, onMove, onOpenQuick, onOpenSingleton, onQuit, profile, showTabCloseButtons, tabs }: {
  activeTabID: string
  auth: ReturnType<typeof authPresentation>
  onActivate: (id: string) => void
  onClose: (tab: AppTab) => void
  onMove: (id: string, toIndex: number) => void
  onOpenQuick: () => void
  onOpenSingleton: (kind: SingletonTabKind) => void
  onQuit: () => void
  profile: string
  showTabCloseButtons: boolean
  tabs: AppTab[]
}) {
  const mac = navigator.platform.toLowerCase().includes('mac')
  const runtime = window.runtime
  const [draggedTabID, setDraggedTabID] = useState('')
  const [dropTargetID, setDropTargetID] = useState('')
  const activateFromKeyboard = (event: ReactKeyboardEvent<HTMLButtonElement>, index: number) => {
    let nextIndex = index
    if (event.key === 'ArrowLeft') nextIndex = (index - 1 + tabs.length) % tabs.length
    else if (event.key === 'ArrowRight') nextIndex = (index + 1) % tabs.length
    else if (event.key === 'Home') nextIndex = 0
    else if (event.key === 'End') nextIndex = tabs.length - 1
    else return
    event.preventDefault()
    onActivate(tabs[nextIndex].id)
    const tabButtons = event.currentTarget.closest('[role="tablist"]')?.querySelectorAll<HTMLButtonElement>('[role="tab"]')
    tabButtons?.[nextIndex]?.focus()
  }
  return <header className="titlebar">
    <div className="titlebar-brand" title="JumpAccess"><AppLogo /></div>
    <div aria-label="工作区 Tab" className="tab-strip" role="tablist">
      {tabs.map((tab, index) => <div
        className={[
          'workspace-tab',
          tab.id === activeTabID ? 'active' : '',
          tab.id === draggedTabID ? 'dragging' : '',
          tab.id === dropTargetID ? 'drag-target' : '',
        ].filter(Boolean).join(' ')}
        draggable
        key={tab.id}
        onAuxClick={(event) => {
          if (event.button !== 1) return
          event.preventDefault()
          onClose(tab)
        }}
        onMouseDown={(event) => {
          if (event.button === 1) event.preventDefault()
        }}
        onDragStart={(event) => {
          setDraggedTabID(tab.id)
          event.dataTransfer.effectAllowed = 'move'
          event.dataTransfer.setData('text/plain', tab.id)
        }}
        onDragOver={(event) => {
          const sourceID = draggedTabID || event.dataTransfer.getData('text/plain')
          if (!sourceID || sourceID === tab.id) return
          event.preventDefault()
          event.dataTransfer.dropEffect = 'move'
          setDropTargetID(tab.id)
        }}
        onDrop={(event) => {
          event.preventDefault()
          const sourceID = draggedTabID || event.dataTransfer.getData('text/plain')
          if (sourceID && sourceID !== tab.id) onMove(sourceID, index)
          setDraggedTabID('')
          setDropTargetID('')
        }}
        onDragEnd={() => {
          setDraggedTabID('')
          setDropTargetID('')
        }}
      >
        <button
          aria-selected={tab.id === activeTabID}
          className="tab-activate"
          onClick={() => onActivate(tab.id)}
          onKeyDown={(event) => activateFromKeyboard(event, index)}
          role="tab"
          tabIndex={tab.id === activeTabID ? 0 : -1}
          title={tab.kind === 'ssh' ? tabTooltip(tab) : tabTitle(tab)}
          type="button"
        >
          {tabIcon(tab)}<span className="tab-primary">{tabTitle(tab)}</span>
          {tab.kind === 'ssh' && tab.descriptor.alias && tab.descriptor.assetName ? <small>{tab.descriptor.assetName}</small> : null}
        </button>
        {showTabCloseButtons ? <button aria-label={`关闭 ${tabTitle(tab)} Tab`} className="tab-close" onClick={() => onClose(tab)} title="关闭 Tab" type="button"><X /></button> : null}
        {tab.id !== activeTabID && tabs[index + 1] && tabs[index + 1].id !== activeTabID
          ? <span aria-hidden="true" className="tab-separator" />
          : null}
      </div>)}
    </div>
    <button aria-label="新建连接" className="titlebar-button new-tab-button" onClick={onOpenQuick} title="新建连接 (Ctrl/Cmd+K)" type="button"><Plus /></button>
    <div className="titlebar-drag-region" onDoubleClick={() => runtime?.WindowToggleMaximise?.()} />
    <nav aria-label="顶部快捷操作" className="titlebar-actions">
      <button aria-label="打开资产" className="titlebar-button" onClick={() => onOpenSingleton('assets')} title="资产" type="button"><Boxes /></button>
      <button
        aria-label={`打开 Profile，认证状态：${profile || '未选择 Profile'}，${auth.title}`}
        className="titlebar-button profile-auth-button"
        onClick={() => onOpenSingleton('profiles')}
        title={`Profile · ${profile || '未选择 Profile'} · ${auth.title} · ${auth.description}`}
        type="button"
      ><span className="profile-status-icon"><Layers3 /><span aria-hidden="true" className={`auth-indicator ${auth.offline ? 'offline' : ''}`.trim()} /></span></button>
      <button aria-label="打开设置" className="titlebar-button" onClick={() => onOpenSingleton('settings')} title="设置" type="button"><Settings /></button>
    </nav>
    {!mac ? <div aria-label="窗口控制" className="window-controls">
      <button aria-label="最小化" onClick={() => runtime?.WindowMinimise?.()} type="button"><Minus /></button>
      <button aria-label="最大化或还原" onClick={() => runtime?.WindowToggleMaximise?.()} type="button"><Square /></button>
      <button aria-label="关闭窗口" className="window-close" onClick={onQuit} type="button"><X /></button>
    </div> : null}
  </header>
}

function StartPage({ onAction }: { onAction: (action: SingletonTabKind | 'quick') => void }) {
  return <section className="start-page">
    <AppLogo labelled className="start-page-logo" />
    <h1>开始使用 JumpAccess</h1>
    <p>打开工作区，或者直接建立一个 SSH 连接。</p>
    <div className="start-actions">
      <button className="start-action primary" onClick={() => onAction('quick')} type="button"><TerminalSquare /><span><strong>新建连接</strong><small>搜索 Asset 或 Alias</small></span></button>
      <button className="start-action" onClick={() => onAction('assets')} type="button"><Boxes /><span><strong>查看资产</strong><small>浏览当前 Organization</small></span></button>
      <button className="start-action" onClick={() => onAction('profiles')} type="button"><Layers3 /><span><strong>查看 Profile</strong><small>管理站点与认证</small></span></button>
      <button className="start-action" onClick={() => onAction('settings')} type="button"><Settings /><span><strong>打开设置</strong><small>调整终端与外观</small></span></button>
    </div>
  </section>
}

function SSHView({ backend, currentDirectory, latency, onCurrentDirectoryChange, onDisconnect, onReconnect, output, preferences, tab }: {
  backend: Backend
  currentDirectory: string
  latency?: SessionLatency
  onCurrentDirectoryChange: (directory: string) => void
  onDisconnect: () => void
  onReconnect: () => void
  output: string
  preferences: Preferences
  tab: SSHTab
}) {
  const descriptor = tab.descriptor
  const status: SessionState['status'] = tab.connectionStatus === 'active'
    ? 'active'
    : tab.connectionStatus === 'failed' ? 'failed'
      : tab.connectionStatus === 'disconnected' ? 'closed' : 'connecting'
  const session: SessionState = {
    id: tab.sessionID ?? '',
    status,
    title: tabTitle(tab),
    profile: descriptor.profile,
    organization: descriptor.organization,
    asset: descriptor.assetName,
    target: descriptor.target,
    alias: descriptor.alias,
    assetId: descriptor.assetID,
    assetName: descriptor.assetName,
    account: descriptor.account,
    error: tab.error ?? '',
  }
  const assetName = descriptor.assetName || descriptor.target
  const statusLabel = status === 'active' ? '已连接'
    : status === 'connecting' ? '连接中'
      : status === 'failed' ? '连接失败' : '未连接'
  const latencyAvailable = status === 'active' && latency?.available
  const showLatency = status === 'active' || status === 'connecting'
  const latencyText = latencyAvailable ? `${latency.milliseconds} ms` : '— ms'
  const latencyClass = status === 'closed' || status === 'failed'
    ? 'offline'
    : !latencyAvailable
      ? 'latency-pending'
      : latency.milliseconds < 100
        ? 'latency-good'
        : latency.milliseconds <= 200 ? 'latency-warning' : 'latency-slow'
  const latencyTitle = latencyAvailable
    ? `${statusLabel} · 到 JumpServer SSH 网关的往返延迟 ${latency.milliseconds} ms`
    : status === 'active' ? `${statusLabel} · 正在检测 JumpServer SSH 网关延迟` : statusLabel
  return <section className="terminal-panel tab-terminal">
    <div className="terminal-toolbar">
      <div className="terminal-toolbar-info">
        <strong className="terminal-toolbar-name">{descriptor.alias || assetName}</strong>
        {descriptor.alias && assetName ? <small className="terminal-toolbar-meta">{assetName}</small> : null}
        {descriptor.assetID ? <small className="terminal-toolbar-meta" title={descriptor.assetID}>{descriptor.assetID}</small> : null}
        <span className={`terminal-connection-metric${showLatency ? '' : ' latency-hidden'}`} title={latencyTitle}>
          <span aria-label={`连接状态：${statusLabel}`} className={`status-dot terminal-connection-status ${latencyClass}`} role="img" />
          {showLatency ? <small className="terminal-latency-value">{latencyText}</small> : null}
        </span>
      </div>
      <div className="terminal-toolbar-actions">
        <button aria-label="复制当前路径" className="icon-button" disabled={!currentDirectory} onClick={() => void navigator.clipboard?.writeText(currentDirectory)} title={currentDirectory || '当前目录不可用'} type="button"><Copy /></button>
        <span aria-hidden="true" className="terminal-action-separator" />
        <button aria-label={`断开 ${tabTitle(tab)} SSH 连接`} className="icon-button danger" disabled={status !== 'active' || !tab.sessionID} onClick={onDisconnect} title="断开连接" type="button"><Unplug /></button>
      </div>
    </div>
    <div className="terminal-screen"><Suspense fallback={<div className="terminal-loading">正在加载终端…</div>}><TerminalPane backend={backend} onCurrentDirectoryChange={onCurrentDirectoryChange} onReconnect={onReconnect} output={output} preferences={preferences} session={session} /></Suspense></div>
    <div className="terminal-statusbar"><span>SSH</span><span>xterm-256color</span><span>{tab.connectionStatus}</span></div>
  </section>
}

function PageHeading({ children, description, eyebrow, title }: { children?: ReactNode; description?: string; eyebrow: string; title: string }) {
  return <div className="page-heading"><div><div className="eyebrow"><Activity />{eyebrow}</div><h1>{title}</h1>{description ? <p>{description}</p> : null}</div>{children}</div>
}

function EmptyState({ action, onAction, title }: { action: string; onAction: () => void; title: string }) {
  return <div className="empty-state"><Server /><h2>{title}</h2><button className="button primary" onClick={onAction}>{action}</button></div>
}

function isAssetRowControl(target: EventTarget | null) {
  return target instanceof Element && target.closest('button, select, input, textarea, a, [role="menuitem"]') !== null
}

function AssetRow({ asset, detail, onBind, onConnect, onConnectAlias, onCreateAlias, onDeleteAlias, onEnsureDetail, onSelect, selected }: {
  asset: Asset
  detail?: AssetDetail
  onBind: (alias: Alias, account: string) => void
  onConnect: () => void
  onConnectAlias: (alias: Alias) => void
  onCreateAlias: () => void
  onDeleteAlias: (alias: Alias) => void
  onEnsureDetail: () => void
  onSelect: () => void
  selected: boolean
}) {
  return <tr className={selected ? 'asset-row selected' : 'asset-row'} data-testid={`asset-row-${asset.id}`} onClick={(event) => { if (!isAssetRowControl(event.target)) onSelect() }}><td><div className="asset-identity"><div className="server-glyph"><Server /></div><div><strong>{asset.name}</strong><span>{asset.address}</span></div></div></td><td><span className="type-label">{asset.type || asset.category || 'Asset'}</span></td><td><div className="inline-alias-stack">{asset.aliases.map((alias) => {
    const knownAccounts = detail?.accounts ?? []
    const currentKnown = knownAccounts.some((account) => account.id === alias.account || account.username === alias.account)
    return <div className="inline-alias-item" key={alias.name}><span className="inline-alias-name"><Tags />{alias.name}</span><div className="inline-alias-actions"><select aria-label={`${alias.name} 默认账号`} onFocus={onEnsureDetail} value={alias.account} onChange={(event) => onBind(alias, event.target.value)}><option value="">连接时询问</option>{alias.account && !currentKnown ? <option value={alias.account}>已绑定账号</option> : null}{knownAccounts.map((account) => <option key={account.id || account.username} value={account.id || account.username}>{accountLabel(account)}</option>)}</select><button className="icon-button" aria-label={`使用 ${alias.name} 连接`} title="连接 SSH" onClick={() => onConnectAlias(alias)}><TerminalSquare /></button><button className="icon-button danger" aria-label={`删除 ${alias.name}`} title="删除 Alias" onClick={() => onDeleteAlias(alias)}><Trash2 /></button></div></div>
  })}{asset.aliases.length === 0 ? <button className="inline-add-alias" aria-label="创建 Alias" onClick={onCreateAlias}><Plus />创建 Alias</button> : null}</div></td><td><AssetRowActions asset={asset} onConnect={onConnect} onCreateAlias={onCreateAlias} /></td></tr>
}

function AssetRowActions({ asset, onConnect, onCreateAlias }: { asset: Asset; onConnect: () => void; onCreateAlias: () => void }) {
  const [open, setOpen] = useState(false)
  const root = useDismissiblePopover(open, () => setOpen(false))
  const act = (action: () => void) => {
    setOpen(false)
    action()
  }
  return <div className="row-actions" ref={root}><button aria-expanded={open} aria-haspopup="menu" aria-label={`${asset.name} 更多操作`} className="icon-button" onClick={() => setOpen((current) => !current)} type="button"><MoreHorizontal /></button>{open ? <div className="popover right" role="menu"><button aria-label={`从操作菜单连接 ${asset.name}`} onClick={() => act(onConnect)} role="menuitem"><TerminalSquare />连接 SSH</button>{asset.aliases.length === 0 ? <button onClick={() => act(onCreateAlias)} role="menuitem"><Plus />创建 Alias</button> : null}<button onClick={() => act(() => void navigator.clipboard?.writeText(asset.address))} role="menuitem"><Copy />复制地址</button><button onClick={() => act(() => void navigator.clipboard?.writeText(asset.id))} role="menuitem"><Copy />复制 Asset ID</button></div> : null}</div>
}

function AssetDetailPane({ asset, detail, onConnect, onCopy, onCreateAlias }: { asset: Asset; detail?: AssetDetail; onConnect: () => void; onCopy: (value: string) => void; onCreateAlias: () => void }) {
  return <aside className="detail-pane"><div className="detail-overline">资产详情</div><div className="detail-title"><div className="detail-icon"><Server /></div><div><h2>{asset.name}</h2><p>{asset.type || asset.category}</p></div></div><dl className="asset-metadata"><div><dt>地址</dt><dd>{asset.address}<button aria-label="复制地址" onClick={() => onCopy(asset.address)}><Copy /></button></dd></div><div><dt>协议</dt><dd>{detail?.protocols.map((protocol) => <span className="badge outline" key={protocol.name}>{protocol.name.toUpperCase()} : {protocol.port}</span>) ?? '加载中…'}</dd></div><div><dt>Asset ID</dt><dd className="mono asset-id-value"><span className="asset-id-text" title={asset.id}>{asset.id}</span><button aria-label="复制 Asset ID" onClick={() => onCopy(asset.id)}><Copy /></button></dd></div></dl><div className="detail-section-heading"><div><h3>可用账号</h3><span>{detail ? `${detail.accounts.length} 个` : '加载中…'}</span></div><ShieldCheck /></div><div className="account-list">{detail?.accounts.map((account) => <div className="account-card" key={account.id || account.username}><span className="account-icon"><KeyRound /></span><span><strong>{accountLabel(account)}</strong><small>{account.name && account.name !== account.username ? account.name : 'JumpServer 授权账号'}</small></span></div>)}</div><div className="connect-actions"><button className="button primary large" aria-label={`连接 ${asset.name}`} onClick={onConnect}><TerminalSquare />连接 SSH</button>{asset.aliases.length === 0 ? <button className="button secondary large icon-only" aria-label="为资产创建 Alias" onClick={onCreateAlias}><Tags /></button> : null}</div></aside>
}

function TerminalFontInput({ families, onChange, value }: { families: string[]; onChange: (value: string) => void; value: string }) {
  const input = useRef<HTMLInputElement>(null)
  const [activeIndex, setActiveIndex] = useState(-1)
  const [editing, setEditing] = useState(false)
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const root = useDismissiblePopover(open, () => {
    setActiveIndex(-1)
    setEditing(false)
    setOpen(false)
    setQuery('')
  })
  const options = useMemo(() => {
    const unique = new Map<string, string>()
    for (const family of ['monospace', ...families, value]) {
      const trimmed = family.trim()
      const key = trimmed.toLocaleLowerCase()
      if (trimmed && !unique.has(key)) unique.set(key, trimmed)
    }
    return [...unique.values()].sort((left, right) => {
      if (left === 'monospace') return -1
      if (right === 'monospace') return 1
      return left.localeCompare(right)
    })
  }, [families, value])
  const normalizedQuery = query.trim()
  const visibleOptions = editing && normalizedQuery
    ? options.filter((family) => family.toLocaleLowerCase().includes(normalizedQuery.toLocaleLowerCase()))
    : options
  const exactOption = options.find((family) => family.toLocaleLowerCase() === normalizedQuery.toLocaleLowerCase())
  const typedValue = exactOption ?? normalizedQuery
  const customValue = editing && normalizedQuery && !exactOption
    ? normalizedQuery
    : ''
  const selectableValues = customValue ? [...visibleOptions, customValue] : visibleOptions

  useEffect(() => {
    setActiveIndex(-1)
    setEditing(false)
    setQuery('')
  }, [value])

  const openAll = () => {
    setActiveIndex(-1)
    setEditing(false)
    setOpen(true)
    setQuery('')
    input.current?.focus()
    input.current?.select()
  }

  const close = () => {
    setActiveIndex(-1)
    setEditing(false)
    setOpen(false)
    setQuery('')
  }

  const choose = (next: string) => {
    close()
    if (next !== value) onChange(next)
  }

  const moveActive = (direction: 1 | -1) => {
    if (!open) openAll()
    if (selectableValues.length === 0) return
    setActiveIndex((current) => {
      if (current < 0) return direction > 0 ? 0 : selectableValues.length - 1
      return (current + direction + selectableValues.length) % selectableValues.length
    })
  }

  return <div className="setting-field terminal-font-field" ref={root}>
    <label htmlFor="terminal-font-family">字体</label>
    <div className="terminal-font-control">
      <input
        aria-activedescendant={activeIndex >= 0 ? `terminal-font-option-${activeIndex}` : undefined}
        aria-autocomplete="list"
        aria-controls="terminal-font-options"
        aria-describedby="terminal-font-help"
        aria-expanded={open}
        aria-haspopup="listbox"
        autoComplete="off"
        id="terminal-font-family"
        onBlur={() => {
          if (editing && typedValue) choose(typedValue)
          else close()
        }}
        onChange={(event) => {
          setActiveIndex(-1)
          setEditing(true)
          setOpen(true)
          setQuery(event.target.value)
        }}
        onClick={() => {
          if (!open) openAll()
          else if (!editing) input.current?.select()
        }}
        onFocus={() => {
          if (!open) openAll()
        }}
        onKeyDown={(event) => {
          if (event.key === 'ArrowDown') {
            event.preventDefault()
            moveActive(1)
          } else if (event.key === 'ArrowUp') {
            event.preventDefault()
            moveActive(-1)
          } else if (event.key === 'Enter') {
            event.preventDefault()
            const next = activeIndex >= 0 ? selectableValues[activeIndex] : typedValue
            if (next) choose(next)
            else close()
          } else if (event.key === 'Escape') {
            event.preventDefault()
            close()
          }
        }}
        ref={input}
        role="combobox"
        spellCheck={false}
        value={editing ? query : value}
      />
      <button
        aria-controls="terminal-font-options"
        aria-expanded={open}
        aria-label={open ? '收起字体列表' : '展开字体列表'}
        className="terminal-font-toggle"
        onClick={() => open ? close() : openAll()}
        onMouseDown={(event) => event.preventDefault()}
        tabIndex={-1}
        title={open ? '收起字体列表' : '展开字体列表'}
        type="button"
      ><ChevronDown /></button>
      {open ? <div aria-label="系统等宽字体" className="terminal-font-options" id="terminal-font-options" role="listbox">
      {visibleOptions.map((family, index) => {
        const selected = family.toLocaleLowerCase() === value.toLocaleLowerCase()
        return <button
          aria-selected={selected}
          className={index === activeIndex ? 'active' : ''}
          id={`terminal-font-option-${index}`}
          key={family}
          onClick={() => choose(family)}
          onMouseDown={(event) => event.preventDefault()}
          onMouseEnter={() => setActiveIndex(index)}
          role="option"
          tabIndex={-1}
          type="button"
        ><span>{family}</span>{family === 'monospace' ? <small>系统默认</small> : null}{selected ? <span aria-hidden="true" className="selected-mark">✓</span> : null}</button>
      })}
      {customValue ? <button
        aria-selected="false"
        className={`terminal-font-custom ${activeIndex === visibleOptions.length ? 'active' : ''}`}
        id={`terminal-font-option-${visibleOptions.length}`}
        onClick={() => choose(customValue)}
        onMouseDown={(event) => event.preventDefault()}
        onMouseEnter={() => setActiveIndex(visibleOptions.length)}
        role="option"
        tabIndex={-1}
        type="button"
      >使用 <strong>“{customValue}”</strong></button> : null}
      {visibleOptions.length === 0 && !customValue ? <span className="terminal-font-empty">没有匹配的字体</span> : null}
      </div> : null}
    </div>
    <small className="setting-help" id="terminal-font-help">输入名称可过滤系统等宽字体，也可直接填写未列出的字体。</small>
  </div>
}

const settingsNavigation = [
  { id: 'appearance', label: '外观', icon: Palette },
  { id: 'terminal', label: '终端', icon: TerminalSquare },
  { id: 'tabs', label: 'Tab 行为', icon: PanelTopClose },
  { id: 'about', label: '关于 JumpAccess', icon: AppLogo },
] as const

type SettingsSectionID = typeof settingsNavigation[number]['id']

function SettingsView({ fontFamilies, onLicense, onOpenConfig, onSave, preferences, version }: { fontFamilies: string[]; onLicense: () => void; onOpenConfig: () => void; onSave: (value: Preferences) => void; preferences: Preferences; version: string }) {
  const [activeSection, setActiveSection] = useState<SettingsSectionID>('appearance')
  const scrollRef = useRef<HTMLDivElement>(null)
  const update = (patch: Partial<Preferences>) => onSave({ ...preferences, ...patch })

  function scrollToSection(sectionID: SettingsSectionID) {
    setActiveSection(sectionID)
    scrollRef.current?.querySelector<HTMLElement>(`#settings-${sectionID}`)?.scrollIntoView({ behavior: 'smooth', block: 'start' })
  }

  function syncActiveSection() {
    const scrollContainer = scrollRef.current
    if (!scrollContainer) return
    if (scrollContainer.scrollHeight > scrollContainer.clientHeight
      && scrollContainer.scrollTop + scrollContainer.clientHeight >= scrollContainer.scrollHeight - 8) {
      setActiveSection(settingsNavigation.at(-1)!.id)
      return
    }

    const threshold = scrollContainer.scrollTop + Math.min(64, Math.max(24, scrollContainer.clientHeight * .12))
    let nextSection: SettingsSectionID = settingsNavigation[0].id
    for (const { id } of settingsNavigation) {
      const section = scrollContainer.querySelector<HTMLElement>(`#settings-${id}`)
      if (section && section.offsetTop <= threshold) nextSection = id
    }
    setActiveSection(nextSection)
  }

  return <section className="full-pane settings-page">
    <PageHeading eyebrow="桌面偏好" title="设置">
      <button className="button secondary" onClick={onOpenConfig}><FileCode2 />打开 config.toml</button>
    </PageHeading>
    <div className="settings-layout">
      <nav aria-label="设置导航" className="settings-nav">
        {settingsNavigation.map(({ icon: Icon, id, label }) => <button
          aria-controls={`settings-${id}`}
          aria-current={activeSection === id ? 'location' : undefined}
          key={id}
          onClick={() => scrollToSection(id)}
          type="button"
        ><Icon /><span>{label}</span></button>)}
      </nav>
      <div className="settings-scroll" data-testid="settings-scroll" onScroll={syncActiveSection} ref={scrollRef}>
        <div className="settings-stack">
          <section className="settings-card" id="settings-appearance">
            <div className="settings-card-title"><Palette /><div><h2>外观</h2><p>整套界面统一跟随所选主题。</p></div></div>
            <div className="segmented-control" aria-label="界面主题">
              {([['light', '浅色'], ['dark', '深色'], ['system', '跟随系统']] as [ThemeMode, string][]).map(([mode, label]) => <button aria-pressed={preferences.theme === mode} className={preferences.theme === mode ? 'selected' : ''} key={mode} onClick={() => update({ theme: mode })}>{label}</button>)}
            </div>
          </section>
          <section className="settings-card" id="settings-terminal">
            <div className="settings-card-title"><TerminalSquare /><div><h2>终端</h2><p>应用于新建及重新打开的终端视图。</p></div></div>
            <TerminalFontInput families={fontFamilies} onChange={(terminalFontFamily) => update({ terminalFontFamily })} value={preferences.terminalFontFamily} />
            <label>字号<select value={preferences.terminalFontSize} onChange={(event) => update({ terminalFontSize: Number(event.target.value) })}>{[12, 13, 14, 16, 18].map((size) => <option key={size}>{size}</option>)}</select></label>
          </section>
          <section className="settings-card" id="settings-tabs">
            <div className="settings-card-title"><PanelTopClose /><div><h2>Tab 行为</h2><p>控制工作区 Tab 的关闭入口和确认方式。</p></div></div>
            <div className="setting-row"><span><strong>显示 Tab 关闭按钮</strong><small>隐藏后仍可使用鼠标中键关闭 Tab。</small></span><button aria-label="显示 Tab 关闭按钮" role="switch" aria-checked={preferences.showTabCloseButtons} className={preferences.showTabCloseButtons ? 'switch on' : 'switch'} onClick={() => update({ showTabCloseButtons: !preferences.showTabCloseButtons })}><span /></button></div>
            <div className="setting-row"><span><strong>关闭活动会话前确认</strong><small>避免误关正在运行的 SSH 终端。</small></span><button aria-label="关闭活动会话前确认" role="switch" aria-checked={preferences.confirmCloseActiveSession} className={preferences.confirmCloseActiveSession ? 'switch on' : 'switch'} onClick={() => update({ confirmCloseActiveSession: !preferences.confirmCloseActiveSession })}><span /></button></div>
          </section>
          <section className="settings-card about-settings-card" id="settings-about">
            <div className="settings-card-title about-settings-inline"><AppLogo labelled className="about-app-logo" /><div><h2>关于 JumpAccess</h2><p>Desktop · {version}</p></div><button className="button secondary small" onClick={onLicense}>查看许可证</button></div>
          </section>
        </div>
      </div>
    </div>
  </section>
}

function Modal({ children, description, onClose, title }: { children: ReactNode; description?: string; onClose: () => void; title: string }) {
  const titleID = `dialog-${title.replace(/\s/g, '-')}`
  return <div className="modal-backdrop" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose() }}><section className="modal" role="dialog" aria-modal="true" aria-labelledby={titleID}><button className="modal-close icon-button" aria-label="关闭" onClick={onClose}><X /></button><header><h2 id={titleID}>{title}</h2>{description ? <p>{description}</p> : null}</header>{children}</section></div>
}

function DisconnectSessionDialog({ onCancel, onConfirm, tab }: { onCancel: () => void; onConfirm: () => void; tab: SSHTab }) {
  return <Modal title="关闭 SSH Tab" description="连接会立即终止，该 Tab 也会从工作区移除。" onClose={onCancel}><div className="disconnect-session-summary"><span><Unplug /></span><div><strong>{tabTitle(tab)}</strong><small>{tab.descriptor.account || '未指定账号'}</small></div></div><div className="dialog-actions"><button className="button secondary" onClick={onCancel}>取消</button><button className="button destructive-confirm" onClick={onConfirm}><Unplug />关闭 Tab</button></div></Modal>
}

function LogoutProfileDialog({ onCancel, onConfirm, profile }: { onCancel: () => void; onConfirm: () => Promise<void>; profile: ProfileSummary }) {
  const [submitError, setSubmitError] = useState('')
  const [submitting, setSubmitting] = useState(false)

  async function confirm() {
    setSubmitError('')
    setSubmitting(true)
    try {
      await onConfirm()
    } catch (reason) {
      setSubmitError(errorMessage(reason))
      setSubmitting(false)
    }
  }

  return <Modal title="退出登录" description="退出后，该 Profile 需要重新完成认证才能访问 JumpServer。" onClose={onCancel}>{submitError ? <div className="dialog-error" role="alert"><ShieldAlert /><span>{submitError}</span></div> : null}<div className="disconnect-session-summary"><span><LogOut /></span><div><strong>{profile.name}</strong><small>{profile.url}</small></div></div><p className="delete-profile-warning">将撤销并清除该 Profile 的本地 OAuth 登录状态；Profile 配置、Organization 和 Alias 会保留，现有 SSH 会话不会断开。</p><div className="dialog-actions"><button className="button secondary" disabled={submitting} onClick={onCancel}>取消</button><button aria-label={`确认退出 ${profile.name}`} className="button destructive-confirm" disabled={submitting} onClick={() => void confirm()}><LogOut />{submitting ? '退出中…' : '确认退出'}</button></div></Modal>
}

function DeleteProfileDialog({ onCancel, onConfirm, profile }: { onCancel: () => void; onConfirm: () => void; profile: ProfileSummary }) {
  return <Modal title="删除 Profile" description="此操作无法撤销。" onClose={onCancel}><div className="delete-profile-summary"><span><Trash2 /></span><div><strong>{profile.name}</strong><small>{profile.url}</small></div></div><p className="delete-profile-warning">将永久删除该 Profile 的 Server URL、Organization、全部 Alias 和本地 OAuth 凭据；如果存在活动 SSH 会话，也会一并断开。JumpServer 上的资产和账号不会被删除。</p><div className="dialog-actions"><button className="button secondary" onClick={onCancel}>取消</button><button aria-label={`删除 ${profile.name} Profile`} className="button destructive-confirm" onClick={onConfirm}><Trash2 />确认删除</button></div></Modal>
}

function AliasDialog({ asset, detail, onCancel, onEnsure, onSave }: { asset: Asset; detail?: AssetDetail; onCancel: () => void; onEnsure: () => Promise<AssetDetail>; onSave: (name: string, account: string) => void }) {
  const [name, setName] = useState('')
  const [account, setAccount] = useState('')
  useEffect(() => { if (!detail) void onEnsure() }, [detail, onEnsure])
  return <Modal title="创建 Alias" description="Alias 保存在当前 Profile，CLI 与 GUI 可共同使用。" onClose={onCancel}><form onSubmit={(event) => { event.preventDefault(); if (name.trim()) onSave(name.trim(), account) }}><div className="dialog-fields"><label>Alias 名称<input autoFocus value={name} onChange={(event) => setName(event.target.value)} placeholder="例如 production-web" /></label><label>Asset<input disabled value={`${asset.name} · ${asset.id}`} /></label><label>默认账号<select value={account} onChange={(event) => setAccount(event.target.value)}><option value="">连接时询问</option>{detail?.accounts.map((item) => <option key={item.id || item.username} value={item.id || item.username}>{accountLabel(item)}</option>)}</select></label></div><div className="dialog-actions"><button className="button secondary" type="button" onClick={onCancel}>取消</button><button className="button primary" disabled={!name.trim()} type="submit">保存 Alias</button></div></form></Modal>
}

function AccountDialog({ accounts, asset, onCancel, onChoose }: { accounts: Account[]; asset: Asset; onCancel: () => void; onChoose: (account: Account) => void }) {
  return <Modal title="选择连接账号" description={`请选择本次连接 ${asset.name} 使用的 Account。`} onClose={onCancel}><div className="prompt-account-list">{accounts.map((account) => <button key={account.id || account.username} onClick={() => onChoose(account)}><span className="account-icon"><KeyRound /></span><span><strong>{accountLabel(account)}</strong><small>{account.name || 'JumpServer 授权账号'}</small></span></button>)}</div><div className="dialog-actions"><button className="button secondary" onClick={onCancel}>取消</button></div></Modal>
}

function QuickConnectDialog({ assets, onCancel, onConnectAlias, onConnectAsset, query, setQuery }: { assets: Asset[]; onCancel: () => void; onConnectAlias: (asset: Asset, alias: Alias) => void; onConnectAsset: (asset: Asset) => void; query: string; setQuery: (value: string) => void }) {
  return <Modal title="快速连接" description="搜索当前 Profile 中的 Asset 或 Alias。" onClose={onCancel}><label className="quick-search"><Search /><input autoFocus value={query} onChange={(event) => setQuery(event.target.value)} placeholder="名称、地址、Asset ID 或 Alias" /></label><div className="quick-results">{assets.flatMap((asset) => asset.aliases.map((alias) => <button key={`alias-${alias.name}`} onClick={() => onConnectAlias(asset, alias)}><span className="quick-icon"><Tags /></span><span><strong>{alias.name}</strong><small>{asset.name} · {asset.address}</small></span><em>{alias.account || '连接时询问'}</em></button>))}{assets.map((asset) => <button key={asset.id} onClick={() => onConnectAsset(asset)}><span className="quick-icon"><Server /></span><span><strong>{asset.name}</strong><small>{asset.address} · {asset.type}</small></span><em>Asset</em></button>)}{assets.length === 0 ? <div className="quick-empty"><Search />没有匹配结果</div> : null}</div></Modal>
}

function ProfileDialog({ onCancel, onSave }: { onCancel: () => void; onSave: (name: string, url: string) => Promise<void> }) {
  const [name, setName] = useState('')
  const [url, setURL] = useState('')
  const [submitError, setSubmitError] = useState('')
  const [submitting, setSubmitting] = useState(false)

  async function submit() {
    setSubmitError('')
    setSubmitting(true)
    try {
      await onSave(name.trim(), url.trim())
    } catch (reason) {
      setSubmitError(errorMessage(reason))
      setSubmitting(false)
    }
  }

  function changeName(value: string) {
    setName(value)
    setSubmitError('')
  }

  function changeURL(value: string) {
    setURL(value)
    setSubmitError('')
  }

  return <Modal title="添加 Profile" description="为一个 JumpServer 站点创建本地连接上下文。" onClose={onCancel}><form onSubmit={(event) => { event.preventDefault(); if (name.trim() && url.trim() && !submitting) void submit() }}>{submitError ? <div className="dialog-error" role="alert"><ShieldAlert /><span>{submitError}</span></div> : null}<div className="dialog-fields"><label>名称<input autoFocus value={name} onChange={(event) => changeName(event.target.value)} placeholder="例如 office" /></label><label>JumpServer URL<input type="url" value={url} onChange={(event) => changeURL(event.target.value)} placeholder="https://jump.example.com" /></label></div><div className="dialog-actions"><button className="button secondary" disabled={submitting} type="button" onClick={onCancel}>取消</button><button className="button primary" disabled={!name.trim() || !url.trim() || submitting} type="submit">{submitting ? '添加中…' : '添加并登录'}</button></div></form></Modal>
}

function EditProfileDialog({ onCancel, onSave, profile }: { onCancel: () => void; onSave: (url: string) => Promise<void>; profile: ProfileSummary }) {
  const [url, setURL] = useState(profile.url)
  const [submitError, setSubmitError] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const changed = url.trim() !== profile.url

  async function submit() {
    setSubmitError('')
    setSubmitting(true)
    try {
      await onSave(url.trim())
    } catch (reason) {
      setSubmitError(errorMessage(reason))
      setSubmitting(false)
    }
  }

  return <Modal title="编辑 Profile" description={`修改 ${profile.name} 的 JumpServer 地址。`} onClose={onCancel}><form onSubmit={(event) => { event.preventDefault(); if (url.trim() && changed && !submitting) void submit() }}>{submitError ? <div className="dialog-error" role="alert"><ShieldAlert /><span>{submitError}</span></div> : null}<div className="dialog-fields"><label>名称<input disabled value={profile.name} /></label><label>JumpServer URL<input autoFocus type="url" value={url} onChange={(event) => { setURL(event.target.value); setSubmitError('') }} /></label></div><p className="edit-profile-note"><ShieldAlert />修改 URL 会保留现有 Organization 和 Alias，但会清除旧的 OAuth 凭据，之后需要重新登录。</p><div className="dialog-actions"><button className="button secondary" disabled={submitting} type="button" onClick={onCancel}>取消</button><button className="button primary" disabled={!url.trim() || !changed || submitting} type="submit">{submitting ? '保存中…' : '保存'}</button></div></form></Modal>
}

function LoginDialog({ attempt, onCancel, onComplete }: { attempt: LoginAttempt; onCancel: () => void; onComplete: (callback: string) => void }) {
  const [callback, setCallback] = useState('')
  return <Modal title="完成浏览器登录" description={`浏览器已打开。完成 ${attempt.profile} 的授权后，粘贴 jms:// 回调链接，或浏览器地址栏中的完整确认页 URL（http:// 或 https://）。`} onClose={onCancel}><form onSubmit={(event) => { event.preventDefault(); if (callback.trim()) onComplete(callback.trim()) }}><div className="dialog-fields"><label>回调链接或确认页 URL<textarea autoFocus value={callback} onChange={(event) => setCallback(event.target.value)} placeholder={'jms://auth/callback?code=…&state=…\n或 https://jump.example.com/core/redirect/confirm/?next=…'} /></label></div><div className="dialog-actions"><button className="button secondary" type="button" onClick={onCancel}>取消</button><button className="button primary" disabled={!callback.trim()} type="submit">完成登录</button></div></form></Modal>
}

function HostKeyDialog({ onDecision, prompt }: { onDecision: (accepted: boolean) => void; prompt: HostKeyPrompt }) {
  return <Modal title="确认新的 SSH Gateway" description="这是首次连接此 Gateway。请通过可信渠道核对指纹。" onClose={() => onDecision(false)}><div className="warning-icon"><ShieldAlert /></div><div className="fingerprint-card"><span>{prompt.host}</span><code>{prompt.fingerprint}</code></div><div className="dialog-actions"><button className="button secondary" onClick={() => onDecision(false)}>拒绝</button><button className="button primary" onClick={() => onDecision(true)}><ShieldCheck />信任并连接</button></div></Modal>
}
