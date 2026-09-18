import { defaultTerminalBackground } from '../model/terminalBackground'
import { DemoShell } from './demoShell'
import type {
  Alias,
  Asset,
  AssetDetail,
  Backend,
  BootstrapState,
  HostKeyPrompt,
  Preferences,
  SessionLatency,
  SessionOutput,
  SessionState,
  SFTPState,
  SFTPEntry,
  SFTPTransfer,
  SFTPConflictChoice,
} from './backend'

export type DemoScenario = 'ready' | 'empty' | 'expired'

// 每次创建均有独立的内存状态，不调用桌面绑定或正式服务。
export function createPreviewBackend(options: { scenario?: DemoScenario } = {}): Backend {
  const assets: Asset[] = [
    { id: '7f3c91bd', name: 'prod-web-01', address: '10.24.8.31', type: 'Linux', category: 'Host', aliases: [
      { name: 'web', asset: '7f3c91bd', account: 'account-deploy', organization: 'org-dev' },
      { name: 'ops', asset: '7f3c91bd', account: 'account-ops', organization: 'org-dev' },
      { name: 'web-any', asset: '7f3c91bd', account: '', organization: 'org-dev' },
    ] },
    { id: 'd92a2c14', name: 'db-primary-01', address: '10.24.12.7', type: 'Linux', category: 'Database', aliases: [
      { name: 'prod-db', asset: 'd92a2c14', account: 'account-dba', organization: 'org-dev' },
    ] },
    { id: '219e0b42', name: 'cache-redis-03', address: '10.24.16.43', type: 'Linux', category: 'Database', aliases: [] },
    { id: '5e1a08c7', name: 'ci-builder-02', address: '10.18.3.22', type: 'Linux', category: 'Host', aliases: [
      { name: 'builder', asset: '5e1a08c7', account: 'account-builder', organization: 'org-dev' },
      { name: 'ci-ops', asset: '5e1a08c7', account: 'account-ops', organization: 'org-dev' },
    ] },
    { id: 'e2b71f03', name: 'bastion-dr-01', address: '172.18.4.10', type: 'Linux', category: 'Host', aliases: [] },
  ]

  const details = new Map<string, AssetDetail>(assets.map((asset) => [asset.id, {
    ...asset,
    accounts: asset.id === 'd92a2c14'
      ? [{ id: 'account-dba', name: 'dba', alias: '', username: 'dba' }, { id: 'account-readonly', name: 'readonly', alias: '', username: 'readonly' }]
      : [{ id: 'account-deploy', name: 'deploy', alias: '', username: 'deploy' }, { id: 'account-ops', name: 'ops', alias: '', username: 'ops' }],
    protocols: [{ name: 'ssh', port: 22 }, { name: 'sftp', port: 22 }],
  }]))

  let state: BootstrapState = {
    version: options.scenario ? 'demo' : 'dev',
    currentProfile: 'office',
    currentOrganization: 'org-dev',
    profiles: [
      { name: 'office', url: 'https://jump.example.com', organization: 'org-dev', aliasCount: 6, auth: { loggedIn: true, expired: false, refreshAvailable: true, expiresAt: new Date(Date.now() + 56 * 60_000).toISOString() } },
      { name: 'staging', url: 'https://staging-jump.example.com', organization: 'org-platform', aliasCount: 0, auth: { loggedIn: false, expired: false, refreshAvailable: false, expiresAt: '' } },
    ],
    preferences: { terminalBackground: { ...defaultTerminalBackground }, version: 13, theme: 'light', terminalFontFamily: 'monospace', terminalFontSize: 12, terminalLineHeight: 1, terminalCursorStyle: 'block', terminalCursorBlink: true, terminalScrollbarVisibility: 'active', terminalShowStatusBar: true, terminalColorScheme: 'nord', terminalRightClickAction: 'paste', terminalWarnOnMultiLinePaste: true, terminalCopyOnEnter: true, downloadMode: 'ask', downloadDirectory: '', confirmCloseActiveSession: true, showTabCloseButtons: true, newTabPosition: 'end' },
    workspace: { activeTabId: 'system:assets', tabs: [{ id: 'system:assets', type: 'assets' }] },
  }

  let sessions: SessionState[] = []
  const shells = new Map<string, DemoShell>()
  const loginAttempts = new Map<string, string>()
  const stateHandlers = new Set<(event: SessionState) => void>()
  const outputHandlers = new Set<(event: SessionOutput) => void>()
  const latencyHandlers = new Set<(event: SessionLatency) => void>()
  const hostKeyHandlers = new Set<(event: HostKeyPrompt) => void>()
  const sftpSessions = new Map<string, SFTPState>()
  const sftpStateHandlers = new Set<(event: SFTPState) => void>()
  const sftpTransferHandlers = new Set<(event: SFTPTransfer) => void>()
  const sftpDirectories = new Map<string, SFTPEntry[]>()
  const sftpTransfers = new Map<string, SFTPTransfer>()
  const transferTimers = new Map<string, number>()
  const batchChoices = new Map<string, SFTPConflictChoice>()
  let previewSequence = 0

  function sftpSession(id: string): SFTPState {
    const session = sftpSessions.get(id)
    if (!session || session.status !== 'active') throw new Error('SFTP 连接已断开。')
    return session
  }

  function directoryKey(session: SFTPState, path: string): string {
    return `${session.assetId}:${path}`
  }

  function parentPath(path: string): string { return path.slice(0, path.lastIndexOf('/')) || '/' }
  function entryName(path: string): string { return path.replace(/\\/g, '/').split('/').pop() || '' }
  function childPath(parent: string, name: string): string { return `${parent === '/' ? '' : parent}/${name}` }
  function directoryEntries(session: SFTPState, path: string): SFTPEntry[] {
    const entries = sftpDirectories.get(directoryKey(session, path))
    if (!entries) throw new Error('目录不存在或不可访问。')
    return entries
  }
  function previewEntry(path: string, type: SFTPEntry['type'], size = 0): SFTPEntry {
    return { name: entryName(path), path, type, size, modifiedAt: new Date().toISOString(), permissions: type === 'directory' ? 'drwxr-xr-x' : '-rw-r--r--' }
  }

  // 浏览器预览只在内存中演示文件操作，不读取或写入本机文件。
  const previewLocalEntries = new Map<string, SFTPEntry>([
    previewEntry('/preview/uploads/notes.txt', 'file', 2_000),
    previewEntry('/preview/uploads/release.zip', 'file', 640_000),
    previewEntry('/preview/uploads/release', 'directory'),
    previewEntry('/preview/uploads/release/README.md', 'file', 3_200),
  ].map((entry) => [entry.path, entry]))

  function emitTransfer(transfer: SFTPTransfer) {
    sftpTransfers.set(transfer.id, transfer)
    sftpTransferHandlers.forEach((handler) => handler(clone(transfer)))
  }

  function sourceEntry(transfer: SFTPTransfer, path = transfer.source): SFTPEntry | undefined {
    if (transfer.direction === 'upload') return previewLocalEntries.get(path)
    return directoryEntries(sftpSession(transfer.sessionId), parentPath(path)).find((entry) => entry.path === path)
  }

  function sourceChildren(transfer: SFTPTransfer, path: string): SFTPEntry[] {
    return transfer.direction === 'upload' ? [...previewLocalEntries.values()].filter((entry) => parentPath(entry.path) === path) : directoryEntries(sftpSession(transfer.sessionId), path)
  }

  function destinationExists(transfer: SFTPTransfer, path = transfer.destination): boolean {
    return transfer.direction === 'download' ? previewLocalEntries.has(path) : directoryEntries(sftpSession(transfer.sessionId), parentPath(path)).some((entry) => entry.path === path)
  }

  function sourceSize(transfer: SFTPTransfer, entry: SFTPEntry): number {
    return entry.type === 'directory' ? sourceChildren(transfer, entry.path).reduce((sum, child) => sum + sourceSize(transfer, child), 0) : entry.size
  }

  function copyPreviewEntry(transfer: SFTPTransfer, source: SFTPEntry, destination: string) {
    const copied = { ...source, path: destination, name: entryName(destination) }
    if (transfer.direction === 'download') previewLocalEntries.set(destination, copied)
    else {
      const session = sftpSession(transfer.sessionId)
      const entries = directoryEntries(session, parentPath(destination)).filter((entry) => entry.path !== destination)
      entries.push(copied)
      sftpDirectories.set(directoryKey(session, parentPath(destination)), entries)
      if (source.type === 'directory') sftpDirectories.set(directoryKey(session, destination), [])
    }
    if (source.type === 'directory') for (const child of sourceChildren(transfer, source.path)) copyPreviewEntry(transfer, child, childPath(destination, child.name))
  }

  function scheduleTransfer(transfer: SFTPTransfer, action: () => void, milliseconds: number) {
    window.clearTimeout(transferTimers.get(transfer.id))
    transferTimers.set(transfer.id, window.setTimeout(() => { transferTimers.delete(transfer.id); action() }, milliseconds))
  }

  function runTransfer(transfer: SFTPTransfer, choice = batchChoices.get(transfer.batchId)) {
    try {
      sftpSession(transfer.sessionId)
      const source = sourceEntry(transfer)
      if (!source) throw new Error('演示源文件不存在。')
      transfer.total = sourceSize(transfer, source)
      if (destinationExists(transfer)) {
        if (!choice) { transfer.status = 'conflict'; transfer.conflict = { source: transfer.source, destination: transfer.destination }; emitTransfer(transfer); return }
        if (choice === 'skip') { transfer.status = 'skipped'; delete transfer.conflict; emitTransfer(transfer); return }
        if (choice === 'keep-both') {
          const original = transfer.destination
          const name = entryName(original)
          const dot = source.type === 'file' ? name.lastIndexOf('.') : -1
          const base = dot > 0 ? name.slice(0, dot) : name
          const extension = dot > 0 ? name.slice(dot) : ''
          let suffix = 1
          do { transfer.destination = childPath(parentPath(original), `${base} (${suffix++})${extension}`) } while (destinationExists(transfer))
        }
      }
      delete transfer.conflict
      transfer.status = 'running'
      emitTransfer(transfer)
      const tick = () => {
        if (transfer.status !== 'running') return
        try {
          sftpSession(transfer.sessionId)
          transfer.transferred = Math.min(transfer.total, transfer.transferred + Math.max(1, Math.ceil(transfer.total / 4)))
          if (transfer.transferred === transfer.total) { copyPreviewEntry(transfer, source, transfer.destination); transfer.status = 'completed' }
          emitTransfer(transfer)
          if (transfer.status === 'running') scheduleTransfer(transfer, tick, 300)
        } catch (reason) { transfer.status = 'failed'; transfer.error = reason instanceof Error ? reason.message : String(reason); emitTransfer(transfer) }
      }
      scheduleTransfer(transfer, tick, 300)
    } catch (reason) { transfer.status = 'failed'; transfer.error = reason instanceof Error ? reason.message : String(reason); emitTransfer(transfer) }
  }

  function transferByID(id: string): SFTPTransfer {
    const transfer = sftpTransfers.get(id)
    if (!transfer) throw new Error('传输任务不存在。')
    return transfer
  }

  function previewHome(session: SFTPState): string {
    const account = details.get(session.assetId ?? '')?.accounts.find((item) => item.id === session.account || item.username === session.account)
    return `/home/${account?.username || 'deploy'}`
  }

  function prepareSFTPFiles(session: SFTPState) {
    const home = previewHome(session)
    const samples: Record<string, Array<[string, 'file' | 'directory', number]>> = {
      '/': [['home', 'directory', 0], ['srv', 'directory', 0]],
      '/home': [[home.split('/').pop()!, 'directory', 0]],
      [home]: [['projects', 'directory', 0], ['notes.txt', 'file', 1200], ['.profile', 'file', 850]],
      [`${home}/projects`]: [],
      '/srv': [['webapp', 'directory', 0]],
      '/srv/webapp': [['logs', 'directory', 0], ['app.yaml', 'file', 1840], ['README.md', 'file', 3200]],
      '/srv/webapp/logs': [['access.log', 'file', 12500]],
    }
    for (const [path, entries] of Object.entries(samples)) {
      const key = directoryKey(session, path)
      if (!sftpDirectories.has(key)) sftpDirectories.set(key, entries.map(([name, type, size]) => ({ name, path: `${path === '/' ? '' : path}/${name}`, type, size, modifiedAt: '2026-09-04T02:24:00Z', permissions: type === 'directory' ? 'drwxr-xr-x' : '-rw-r--r--' })))
    }
  }

  const delay = <T>(value: T, milliseconds = 90) => new Promise<T>((resolve) => window.setTimeout(() => resolve(value), milliseconds))
  const clone = <T>(value: T): T => structuredClone(value)

  function aliasCount(): number {
    return assets.reduce((total, asset) => total + asset.aliases.length, 0)
  }

  if (options.scenario === 'empty') {
    state.profiles = []
    state.currentProfile = ''
    state.currentOrganization = ''
  }
  if (options.scenario === 'expired') {
    state.profiles[0].auth = { loggedIn: true, expired: true, refreshAvailable: false, expiresAt: new Date(Date.now() - 3600_000).toISOString() }
  }

  return {
    bootstrap: () => delay(clone(state)),
    listOrganizations: () => delay([{ id: 'org-dev', name: '研发中心' }, { id: 'org-platform', name: '工程效能' }, { id: 'org-dr', name: '灾备中心' }]),
    async listAssets(request) {
      const query = request.search.toLowerCase()
      const matched = assets.filter((asset) => !query || [asset.id, asset.name, asset.address, ...asset.aliases.map((alias) => alias.name)].some((value) => value.toLowerCase().includes(query)))
      return delay({ count: matched.length, offset: request.offset, limit: request.limit, aliasCount: aliasCount(), results: clone(matched.slice(request.offset, request.offset + request.limit)) })
    },
    getAsset: ({ asset }) => delay(clone(details.get(asset) ?? details.get(assets.find((item) => item.name === asset)?.id ?? '')!)),
    async quickSearch(request) {
      const page = await this.listAssets({ profile: request.profile, organization: request.organization, search: request.query, offset: 0, limit: request.limit })
      return page.results
    },
    addProfile: async (name, siteURL) => { state.profiles.push({ name, url: siteURL, organization: '', aliasCount: 0, auth: { loggedIn: false, expired: false, refreshAvailable: false, expiresAt: '' } }) },
    updateProfileURL: async (name, siteURL) => {
      const profile = state.profiles.find((item) => item.name === name)
      if (!profile) throw new Error(`profile ${JSON.stringify(name)} does not exist`)
      profile.url = siteURL.replace(/\/+$/, '')
      profile.auth = { loggedIn: false, expired: false, refreshAvailable: false, expiresAt: '' }
    },
    deleteProfile: async (name) => {
      state.profiles = state.profiles.filter((item) => item.name !== name)
      for (const session of sessions) if (session.profile === name) shells.delete(session.id)
      for (const [id, profile] of loginAttempts) if (profile === name) loginAttempts.delete(id)
      sessions = sessions.filter((session) => session.profile !== name)
      if (state.currentProfile === name) {
        state.currentProfile = [...state.profiles].sort((left, right) => left.name.localeCompare(right.name))[0]?.name ?? ''
        state.currentOrganization = state.profiles.find((item) => item.name === state.currentProfile)?.organization ?? ''
      }
    },
    useProfile: async (name) => {
      const profile = state.profiles.find(item => item.name === name)
      if (!profile) throw new Error('Profile 不存在。')
      state.currentProfile = name
      state.currentOrganization = profile.organization
    },
    setOrganization: async (profile, organization) => { const item = state.profiles.find((value) => value.name === profile); if (item) item.organization = organization; state.currentOrganization = organization },
    async createAlias(request) {
      const asset = assets.find((item) => item.id === request.asset)!
      const alias: Alias = { name: request.name, asset: asset.id, account: request.account, organization: state.currentOrganization }
      asset.aliases.push(alias)
      details.set(asset.id, { ...details.get(asset.id)!, aliases: asset.aliases })
      state.profiles.find((item) => item.name === request.profile)!.aliasCount = aliasCount()
      return clone(alias)
    },
    async deleteAlias(profile, name) {
      for (const asset of assets) {
        asset.aliases = asset.aliases.filter((alias) => alias.name !== name)
        details.set(asset.id, { ...details.get(asset.id)!, aliases: asset.aliases })
      }
      const owner = state.profiles.find(item => item.name === profile)
      if (owner) owner.aliasCount = aliasCount()
    },
    async renameAlias(request) {
      let renamed: Alias | undefined
      for (const asset of assets) {
        asset.aliases = asset.aliases.map((alias) => {
          if (alias.name !== request.currentName) return alias
          renamed = { ...alias, name: request.newName }
          return renamed
        })
        const detail = details.get(asset.id)
        if (detail) details.set(asset.id, { ...detail, aliases: asset.aliases })
      }
      if (!renamed) throw new Error(`Alias ${request.currentName} 不存在`)
      return clone(renamed)
    },
    async setAliasAccount(request) {
      for (const asset of assets) {
        asset.aliases = asset.aliases.map((alias) => alias.name === request.name ? { ...alias, account: request.account } : alias)
        details.set(asset.id, { ...details.get(asset.id)!, aliases: asset.aliases })
      }
    },
    async minimizeWindow() {},
    async ensureWindowVisible() {},
    async savePreferences(preferences: Preferences) { state = { ...state, preferences: clone(preferences) } },
    async saveWorkspace(workspace) { state = { ...state, workspace: clone(workspace) } },
    async getAuthStatus(profile) { return clone(state.profiles.find((item) => item.name === profile)!.auth) },
    async refreshAuth(profile) { return clone(state.profiles.find((item) => item.name === profile)!.auth) },
    async startLogin(profile) {
      if (!state.profiles.some(item => item.name === profile)) throw new Error('Profile 不存在。')
      const id = `login-${++previewSequence}`
      loginAttempts.set(id, profile)
      return { id, profile, expiresAt: new Date(Date.now() + 300_000).toISOString() }
    },
    async completeLogin(attemptID, _callbackURL) {
      const profile = state.profiles.find(item => item.name === loginAttempts.get(attemptID))
      if (!profile) throw new Error('演示登录已取消或不存在。')
      loginAttempts.delete(attemptID)
      profile.auth = { loggedIn: true, expired: false, refreshAvailable: true, expiresAt: new Date(Date.now() + 3600_000).toISOString() }
      return clone(profile.auth)
    },
    cancelLogin: async id => { loginAttempts.delete(id) },
    async logout(profile) { const item = state.profiles.find((value) => value.name === profile); if (item) item.auth.loggedIn = false },
    licenseText: () => delay('MIT License\n\nCopyright (c) 2026 Eric Ruan\n\nPermission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files, to deal in the Software without restriction.'),
    chooseTerminalBackground: async () => '',
    chooseDownloadFolder: async () => '',
    readTerminalBackground: async () => { throw new Error('浏览器预览无法读取本地路径，请在桌面应用选择图片。') },
    openConfig: async () => undefined,
    listMonospaceFonts: () => delay(['Cascadia Mono', 'JetBrains Mono', 'Menlo']),
    async startSSHSession(request) {
      const asset = assets.find((item) => item.id === request.target || item.aliases.some((alias) => alias.name === request.target))
      if (!asset) throw new Error('资产不存在。')
      const username = details.get(asset.id)?.accounts.find(item => item.id === request.account || item.username === request.account)?.username || 'deploy'
      const shell = new DemoShell(username, asset.name)
      const session: SessionState = { id: `session-${++previewSequence}`, status: 'connecting', title: asset.name, profile: request.profile, organization: request.organization, asset: asset.id, assetId: asset.id, assetName: asset.name, account: request.account, error: '' }
      shells.set(session.id, shell)
      sessions = [...sessions, session]
      window.setTimeout(() => {
        if (!shells.has(session.id)) return
        const active = { ...session, status: 'active' as const }
        sessions = sessions.map((item) => item.id === active.id ? active : item)
        stateHandlers.forEach((handler) => handler(active))
        latencyHandlers.forEach((handler) => handler({ id: active.id, milliseconds: 42, available: true }))
        outputHandlers.forEach((handler) => handler({ id: active.id, data: `Connecting through JumpServer gateway…\r\nConnected to ${asset.name}\r\n\r\nJumpAccess Demo — simulated SSH session\r\nType help to see available sample commands.\r\n\r\n${shell.prompt()}` }))
      }, 500)
      return clone(session)
    },
    listSSHSessions: () => delay(clone(sessions)),
    async writeSSHSession(id, data) {
      const shell = shells.get(id)
      if (!shell || !sessions.some(item => item.id === id && item.status === 'active')) throw new Error('SSH 连接已断开。')
      const output = shell.write(data)
      outputHandlers.forEach(handler => handler({ id, data: output }))
    },
    resizeSSHSession: async () => undefined,
    async closeSSHSession(id) {
      const session = sessions.find(item => item.id === id)
      shells.delete(id)
      sessions = sessions.filter((session) => session.id !== id)
      if (session) stateHandlers.forEach(handler => handler({ ...session, status: 'closed' }))
    },
    resolveSSHHostKey: async () => undefined,
    onSessionState(handler) { stateHandlers.add(handler); return () => stateHandlers.delete(handler) },
    onSessionOutput(handler) { outputHandlers.add(handler); return () => outputHandlers.delete(handler) },
    onSessionLatency(handler) { latencyHandlers.add(handler); return () => latencyHandlers.delete(handler) },
    onHostKeyPrompt(handler) { hostKeyHandlers.add(handler); return () => hostKeyHandlers.delete(handler) },
    async startSFTPSession(request) {
      const source = request.sourceSSHSessionId ? sessions.find((session) => session.id === request.sourceSSHSessionId) : undefined
      const target = source?.assetId || source?.asset || request.target
      const asset = assets.find((item) => item.id === target || item.aliases.some((alias) => alias.name === target))
      if (!asset) throw new Error('资产不存在。')
      const session: SFTPState = { id: `sftp-preview-${++previewSequence}`, status: 'active', title: asset.aliases.find((alias) => alias.name === request.target)?.name || asset.name, profile: source?.profile || request.profile, organization: source?.organization || request.organization, target: asset.id, asset: asset.id, assetId: asset.id, assetName: asset.name, alias: asset.aliases.find((alias) => alias.name === request.target)?.name || '', account: source?.account || request.account, directory: '', error: '' }
      prepareSFTPFiles(session)
      session.directory = request.directory && sftpDirectories.has(directoryKey(session, request.directory)) ? request.directory : previewHome(session)
      sftpSessions.set(session.id, session)
      sftpStateHandlers.forEach((handler) => handler(clone(session)))
      return clone(session)
    },
    async listSFTPSessions() { return clone([...sftpSessions.values()]) },
    async closeSFTPSession(id) {
      const session = sftpSessions.get(id)
      if (session) { session.status = 'closed'; sftpStateHandlers.forEach((handler) => handler(clone(session))) }
      for (const transfer of sftpTransfers.values()) if (transfer.sessionId === id) await this.cancelSFTPTransfer(transfer.id)
    },
    async readSFTPDirectory(id, path) {
      const session = sftpSession(id)
      const entries = sftpDirectories.get(directoryKey(session, path))
      if (!entries) throw new Error('目录不存在或不可访问。')
      return clone({ path, entries })
    },
    async homeSFTPDirectory(id) { return this.readSFTPDirectory(id, previewHome(sftpSession(id))) },
    async makeSFTPDirectory(id, path) {
      const session = sftpSession(id)
      const parent = directoryEntries(session, parentPath(path))
      if (!entryName(path) || parent.some((entry) => entry.path === path)) throw new Error('文件或目录已存在。')
      parent.push(previewEntry(path, 'directory'))
      sftpDirectories.set(directoryKey(session, path), [])
    },
    async renameSFTPEntry(id, path, newName) {
      const session = sftpSession(id)
      const entries = directoryEntries(session, parentPath(path))
      const entry = entries.find((item) => item.path === path)
      if (!entry) throw new Error('文件不存在。')
      if (!newName || /[/\\]/.test(newName) || ['.', '..'].includes(newName)) throw new Error('名称无效。')
      if (entries.some((item) => item.name === newName)) throw new Error('文件或目录已存在。')
      const destination = childPath(parentPath(path), newName)
      entry.name = newName
      entry.path = destination
      const sourceKey = directoryKey(session, path)
      for (const [key, children] of [...sftpDirectories]) {
        if (key !== sourceKey && !key.startsWith(`${sourceKey}/`)) continue
        sftpDirectories.delete(key)
        sftpDirectories.set(`${directoryKey(session, destination)}${key.slice(sourceKey.length)}`, children.map((child) => ({ ...child, path: `${destination}${child.path.slice(path.length)}` })))
      }
    },
    async removeSFTPEntries(id, paths) {
      const session = sftpSession(id)
      for (const path of paths) {
        if (path === '/') throw new Error('无法删除根目录。')
        const entries = directoryEntries(session, parentPath(path))
        sftpDirectories.set(directoryKey(session, parentPath(path)), entries.filter((entry) => entry.path !== path))
        const key = directoryKey(session, path)
        for (const candidate of sftpDirectories.keys()) if (candidate === key || candidate.startsWith(`${key}/`)) sftpDirectories.delete(candidate)
      }
    },
    async chooseSFTPUploadFiles() { return ['/preview/uploads/notes.txt', '/preview/uploads/release.zip'] },
    async chooseSFTPUploadDirectory() { return '/preview/uploads/release' },
    async chooseSFTPDownloadDirectory() { return '/preview/downloads' },
    async startSFTPTransfer(request) {
      sftpSession(request.sessionId)
      const batchId = `preview-batch-${++previewSequence}`
      const transfers = request.sources.map((source): SFTPTransfer => ({ id: `preview-transfer-${++previewSequence}`, batchId, sessionId: request.sessionId, direction: request.direction, name: entryName(source), source, destination: childPath(request.destination, entryName(source)), status: 'queued', transferred: 0, total: 0, error: '' }))
      for (const transfer of transfers) { emitTransfer(transfer); scheduleTransfer(transfer, () => runTransfer(transfer), 150) }
      return clone(transfers)
    },
    async listSFTPTransfers(id) { return clone([...sftpTransfers.values()].filter((transfer) => transfer.sessionId === id)) },
    async cancelSFTPTransfer(id) {
      const transfer = transferByID(id)
      if (!['queued', 'running', 'conflict'].includes(transfer.status)) return
      window.clearTimeout(transferTimers.get(id)); transferTimers.delete(id)
      transfer.status = 'cancelled'; delete transfer.conflict; emitTransfer(transfer)
    },
    async retrySFTPTransfer(id) {
      const transfer = transferByID(id)
      sftpSession(transfer.sessionId)
      if (!['failed', 'cancelled'].includes(transfer.status)) throw new Error('该任务无需重试。')
      transfer.status = 'queued'; transfer.transferred = 0; transfer.error = ''; delete transfer.conflict
      emitTransfer(transfer); scheduleTransfer(transfer, () => runTransfer(transfer), 150)
      return clone(transfer)
    },
    async resolveSFTPConflict(id, choice, applyToBatch) {
      const transfer = transferByID(id)
      if (transfer.status !== 'conflict') throw new Error('此任务没有等待处理的冲突。')
      if (applyToBatch) batchChoices.set(transfer.batchId, choice)
      for (const pending of sftpTransfers.values()) if (pending.status === 'conflict' && (pending.id === id || (applyToBatch && pending.batchId === transfer.batchId))) runTransfer(pending, choice)
    },
    async clearCompletedSFTPTransfers(id) { for (const [key, transfer] of sftpTransfers) if (transfer.sessionId === id && ['completed', 'skipped', 'cancelled'].includes(transfer.status)) sftpTransfers.delete(key) },
    onSFTPState(handler) { sftpStateHandlers.add(handler); return () => sftpStateHandlers.delete(handler) },
    onSFTPTransfer(handler) { sftpTransferHandlers.add(handler); return () => sftpTransferHandlers.delete(handler) },
    onQuitRequested: () => () => undefined,
    confirmQuit: async () => undefined,
  }
}

export const previewBackend = createPreviewBackend()
