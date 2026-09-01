import type {
  Alias,
  Asset,
  AssetDetail,
  Backend,
  BootstrapState,
  HostKeyPrompt,
  Preferences,
  SessionOutput,
  SessionState,
} from './backend'

const assets: Asset[] = [
  { id: '7f3c91bd', name: 'prod-web-01', address: '10.24.8.31', type: 'Linux', category: 'Host', aliases: [
    { name: 'production-web', asset: '7f3c91bd', account: 'account-deploy', organization: 'org-dev' },
    { name: 'production-ops', asset: '7f3c91bd', account: 'account-ops', organization: 'org-dev' },
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
  protocols: [{ name: 'ssh', port: 22 }],
}]))

let state: BootstrapState = {
  version: '0.1.0-dev',
  currentProfile: 'production',
  currentOrganization: 'org-dev',
  profiles: [
    { name: 'production', url: 'https://jump.example.com', organization: 'org-dev', aliasCount: 6, auth: { loggedIn: true, expired: false, refreshAvailable: true, expiresAt: new Date(Date.now() + 56 * 60_000).toISOString() } },
    { name: 'staging', url: 'https://staging-jump.example.com', organization: 'org-platform', aliasCount: 0, auth: { loggedIn: false, expired: false, refreshAvailable: false, expiresAt: '' } },
  ],
  preferences: { version: 1, theme: 'light', terminalFontFamily: 'monospace', terminalFontSize: 12, confirmCloseActiveSession: true },
  workspace: { activeTabId: 'system:assets', tabs: [{ id: 'system:assets', type: 'assets' }] },
}

let sessions: SessionState[] = []
const stateHandlers = new Set<(event: SessionState) => void>()
const outputHandlers = new Set<(event: SessionOutput) => void>()
const hostKeyHandlers = new Set<(event: HostKeyPrompt) => void>()

const delay = <T>(value: T, milliseconds = 90) => new Promise<T>((resolve) => window.setTimeout(() => resolve(value), milliseconds))
const clone = <T>(value: T): T => structuredClone(value)

function aliasCount(): number {
  return assets.reduce((total, asset) => total + asset.aliases.length, 0)
}

export const previewBackend: Backend = {
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
    sessions = sessions.filter((session) => session.profile !== name)
    if (state.currentProfile === name) {
      state.currentProfile = [...state.profiles].sort((left, right) => left.name.localeCompare(right.name))[0]?.name ?? ''
      state.currentOrganization = state.profiles.find((item) => item.name === state.currentProfile)?.organization ?? ''
    }
  },
  useProfile: async (name) => { state.currentProfile = name },
  setOrganization: async (profile, organization) => { const item = state.profiles.find((value) => value.name === profile); if (item) item.organization = organization; state.currentOrganization = organization },
  async createAlias(request) {
    const asset = assets.find((item) => item.id === request.asset)!
    const alias: Alias = { name: request.name, asset: asset.id, account: request.account, organization: state.currentOrganization }
    asset.aliases.push(alias)
    details.set(asset.id, { ...details.get(asset.id)!, aliases: asset.aliases })
    state.profiles.find((item) => item.name === request.profile)!.aliasCount = aliasCount()
    return clone(alias)
  },
  async deleteAlias(_profile, name) { for (const asset of assets) asset.aliases = asset.aliases.filter((alias) => alias.name !== name) },
  async setAliasAccount(request) { for (const asset of assets) asset.aliases = asset.aliases.map((alias) => alias.name === request.name ? { ...alias, account: request.account } : alias) },
  async savePreferences(preferences: Preferences) { state = { ...state, preferences: clone(preferences) } },
  async saveWorkspace(workspace) { state = { ...state, workspace: clone(workspace) } },
  async getAuthStatus(profile) { return clone(state.profiles.find((item) => item.name === profile)!.auth) },
  async refreshAuth(profile) { return clone(state.profiles.find((item) => item.name === profile)!.auth) },
  async startLogin(profile) { return { id: `login-${Date.now()}`, profile, expiresAt: new Date(Date.now() + 300_000).toISOString() } },
  async completeLogin(_attemptID, _callbackURL) { const auth = { loggedIn: true, expired: false, refreshAvailable: true, expiresAt: new Date(Date.now() + 3600_000).toISOString() }; return auth },
  cancelLogin: async () => undefined,
  async logout(profile) { const item = state.profiles.find((value) => value.name === profile); if (item) item.auth.loggedIn = false },
  licenseText: () => delay('MIT License\n\nCopyright (c) 2026 Eric Ruan\n\nPermission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files, to deal in the Software without restriction.'),
  openConfig: async () => undefined,
  listMonospaceFonts: () => delay(['Cascadia Mono', 'JetBrains Mono', 'Menlo']),
  async startSSHSession(request) {
    const asset = assets.find((item) => item.id === request.target || item.aliases.some((alias) => alias.name === request.target))
    const session: SessionState = { id: `session-${Date.now()}`, status: 'connecting', title: request.target, profile: request.profile, organization: request.organization, asset: asset?.id ?? request.target, account: request.account, error: '' }
    sessions = [...sessions, session]
    window.setTimeout(() => {
      const active = { ...session, status: 'active' as const }
      const remoteName = asset?.name ?? 'asset'
      const remoteDirectory = `/home/${request.account || 'user'}`
      sessions = sessions.map((item) => item.id === active.id ? active : item)
      stateHandlers.forEach((handler) => handler(active))
      outputHandlers.forEach((handler) => handler({ id: active.id, data: `Connecting through JumpServer gateway…\r\nConnected to ${asset?.name ?? request.target}\r\n\r\n\x1b]7;file://${remoteName}${encodeURI(remoteDirectory)}\x1b\\${request.account}@${remoteName}:~$ ` }))
    }, 500)
    return clone(session)
  },
  listSSHSessions: () => delay(clone(sessions)),
  async writeSSHSession(id, data) { outputHandlers.forEach((handler) => handler({ id, data })) },
  resizeSSHSession: async () => undefined,
  async closeSSHSession(id) { sessions = sessions.filter((session) => session.id !== id) },
  resolveSSHHostKey: async () => undefined,
  onSessionState(handler) { stateHandlers.add(handler); return () => stateHandlers.delete(handler) },
  onSessionOutput(handler) { outputHandlers.add(handler); return () => outputHandlers.delete(handler) },
  onHostKeyPrompt(handler) { hostKeyHandlers.add(handler); return () => hostKeyHandlers.delete(handler) },
}
