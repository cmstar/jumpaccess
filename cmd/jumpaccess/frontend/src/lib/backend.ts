export type ThemeMode = 'system' | 'light' | 'dark'

export interface Preferences {
  version: number
  theme: ThemeMode
  terminalFontFamily: string
  terminalFontSize: number
  confirmCloseActiveSession: boolean
  showTabCloseButtons: boolean
}

export type WorkspaceTabType = 'assets' | 'profiles' | 'settings' | 'ssh'

export interface WorkspaceTab {
  id: string
  type: WorkspaceTabType
  profile?: string
  organization?: string
  target?: string
  account?: string
  assetId?: string
  assetName?: string
  alias?: string
}

export interface Workspace {
  activeTabId: string
  tabs: WorkspaceTab[] | null
}

export interface AuthStatus {
  loggedIn: boolean
  expired: boolean
  refreshAvailable: boolean
  expiresAt: string
}

export interface ProfileSummary {
  name: string
  url: string
  organization: string
  aliasCount: number
  auth: AuthStatus
}

export interface BootstrapState {
  version: string
  currentProfile: string
  currentOrganization: string
  profiles: ProfileSummary[]
  preferences: Preferences
  workspace: Workspace
}

export interface Organization {
  id: string
  name: string
}

export interface Alias {
  name: string
  asset: string
  account: string
  organization: string
}

export interface Asset {
  id: string
  name: string
  address: string
  type: string
  category: string
  aliases: Alias[]
}

export interface Account {
  id: string
  name: string
  alias: string
  username: string
}

export interface Protocol {
  name: string
  port: number
}

export interface AssetDetail extends Asset {
  accounts: Account[]
  protocols: Protocol[]
}

export interface AssetPage {
  count: number
  offset: number
  limit: number
  aliasCount: number
  results: Asset[]
}

export interface SessionState {
  id: string
  status: 'connecting' | 'active' | 'closed' | 'failed'
  title: string
  profile: string
  organization: string
  asset: string
  target?: string
  alias?: string
  assetId?: string
  assetName?: string
  account: string
  error: string
}

export interface SessionOutput {
  id: string
  data: string
}

export interface SessionLatency {
  id: string
  milliseconds: number
  available: boolean
}

export interface HostKeyPrompt {
  id: string
  host: string
  fingerprint: string
}

export interface LoginAttempt {
  id: string
  profile: string
  expiresAt: string
}

export interface Backend {
  bootstrap(): Promise<BootstrapState>
  listOrganizations(profile: string): Promise<Organization[]>
  listAssets(request: { profile: string; organization: string; search: string; offset: number; limit: number }): Promise<AssetPage>
  getAsset(request: { profile: string; organization: string; asset: string }): Promise<AssetDetail>
  quickSearch(request: { profile: string; organization: string; query: string; limit: number }): Promise<Asset[]>
  addProfile(name: string, siteURL: string): Promise<void>
  updateProfileURL(name: string, siteURL: string): Promise<void>
  deleteProfile(name: string): Promise<void>
  useProfile(name: string): Promise<void>
  setOrganization(profile: string, organization: string): Promise<void>
  createAlias(request: { profile: string; asset: string; name: string; account: string }): Promise<Alias>
  deleteAlias(profile: string, name: string): Promise<void>
  setAliasAccount(request: { profile: string; name: string; account: string }): Promise<void>
  savePreferences(preferences: Preferences): Promise<void>
  saveWorkspace(workspace: Workspace): Promise<void>
  getAuthStatus(profile: string): Promise<AuthStatus>
  refreshAuth(profile: string): Promise<AuthStatus>
  startLogin(profile: string): Promise<LoginAttempt>
  completeLogin(attemptID: string, callbackURL: string): Promise<AuthStatus>
  cancelLogin(attemptID: string): Promise<void>
  logout(profile: string): Promise<void>
  licenseText(): Promise<string>
  openConfig(): Promise<void>
  listMonospaceFonts(): Promise<string[]>
  startSSHSession(request: { profile: string; organization: string; target: string; account: string; columns: number; rows: number }): Promise<SessionState>
  listSSHSessions(): Promise<SessionState[]>
  writeSSHSession(id: string, data: string): Promise<void>
  resizeSSHSession(id: string, columns: number, rows: number): Promise<void>
  closeSSHSession(id: string): Promise<void>
  resolveSSHHostKey(id: string, accepted: boolean): Promise<void>
  onSessionState(handler: (event: SessionState) => void): () => void
  onSessionOutput(handler: (event: SessionOutput) => void): () => void
  onSessionLatency(handler: (event: SessionLatency) => void): () => void
  onHostKeyPrompt(handler: (event: HostKeyPrompt) => void): () => void
}

type GoPreferences = {
  Version: number
  Appearance: { Theme: ThemeMode; TerminalFontFamily: string; TerminalFontSize: number }
  Behavior: { ConfirmCloseActiveSession: boolean; ShowTabCloseButtons: boolean }
}

type DesktopBinding = {
  Bootstrap(): Promise<Omit<BootstrapState, 'preferences'> & { preferences: GoPreferences }>
  ListOrganizations(profile: string): Promise<Organization[]>
  ListAssets(request: Parameters<Backend['listAssets']>[0]): Promise<AssetPage>
  GetAsset(request: Parameters<Backend['getAsset']>[0]): Promise<AssetDetail>
  QuickSearch(request: Parameters<Backend['quickSearch']>[0]): Promise<Asset[]>
  AddProfile(name: string, siteURL: string): Promise<void>
  UpdateProfileURL(name: string, siteURL: string): Promise<void>
  DeleteProfile(name: string): Promise<void>
  UseProfile(name: string): Promise<void>
  SetOrganization(profile: string, organization: string): Promise<void>
  CreateAlias(request: Parameters<Backend['createAlias']>[0]): Promise<Alias>
  DeleteAlias(profile: string, name: string): Promise<void>
  SetAliasAccount(request: Parameters<Backend['setAliasAccount']>[0]): Promise<void>
  SavePreferences(preferences: GoPreferences): Promise<void>
  SaveWorkspace(workspace: Workspace): Promise<void>
  GetAuthStatus(profile: string): Promise<AuthStatus>
  RefreshAuth(profile: string): Promise<AuthStatus>
  StartLogin(profile: string): Promise<LoginAttempt>
  CompleteLogin(attemptID: string, callbackURL: string): Promise<AuthStatus>
  CancelLogin(attemptID: string): Promise<void>
  Logout(profile: string): Promise<void>
  LicenseText(): Promise<string>
  OpenConfig(): Promise<void>
  ListMonospaceFonts(): Promise<string[]>
  StartSSHSession(request: Parameters<Backend['startSSHSession']>[0]): Promise<SessionState>
  ListSSHSessions(): Promise<SessionState[]>
  WriteSSHSession(id: string, data: string): Promise<void>
  ResizeSSHSession(id: string, columns: number, rows: number): Promise<void>
  CloseSSHSession(id: string): Promise<void>
  ResolveSSHHostKey(id: string, accepted: boolean): Promise<void>
}

declare global {
  interface Window {
    go?: { main?: { desktopApp?: DesktopBinding } }
    runtime?: {
      EventsOnMultiple(eventName: string, callback: (event: unknown) => void, maxCallbacks: number): () => void
      Quit?(): void
      WindowMinimise?(): void
      WindowToggleMaximise?(): void
    }
  }
}

function binding(): DesktopBinding {
  const value = window.go?.main?.desktopApp
  if (!value) throw new Error('Wails 后端尚未连接，请从 JumpAccess 桌面应用打开此界面。')
  return value
}

function subscribe<T>(eventName: string, handler: (event: T) => void): () => void {
  if (!window.runtime) return () => undefined
  return window.runtime.EventsOnMultiple(eventName, (event) => handler(event as T), -1)
}

function toPreferences(value: GoPreferences): Preferences {
  return {
    version: value.Version,
    theme: value.Appearance.Theme,
    terminalFontFamily: value.Appearance.TerminalFontFamily,
    terminalFontSize: value.Appearance.TerminalFontSize,
    confirmCloseActiveSession: value.Behavior.ConfirmCloseActiveSession,
    showTabCloseButtons: value.Behavior.ShowTabCloseButtons,
  }
}

function fromPreferences(value: Preferences): GoPreferences {
  return {
    Version: value.version,
    Appearance: {
      Theme: value.theme,
      TerminalFontFamily: value.terminalFontFamily,
      TerminalFontSize: value.terminalFontSize,
    },
    Behavior: {
      ConfirmCloseActiveSession: value.confirmCloseActiveSession,
      ShowTabCloseButtons: value.showTabCloseButtons,
    },
  }
}

export const wailsBackend: Backend = {
  async bootstrap() {
    const state = await binding().Bootstrap()
    return { ...state, preferences: toPreferences(state.preferences) }
  },
  listOrganizations: (profile) => binding().ListOrganizations(profile),
  listAssets: (request) => binding().ListAssets(request),
  getAsset: (request) => binding().GetAsset(request),
  quickSearch: (request) => binding().QuickSearch(request),
  addProfile: (name, siteURL) => binding().AddProfile(name, siteURL),
  updateProfileURL: (name, siteURL) => binding().UpdateProfileURL(name, siteURL),
  deleteProfile: (name) => binding().DeleteProfile(name),
  useProfile: (name) => binding().UseProfile(name),
  setOrganization: (profile, organization) => binding().SetOrganization(profile, organization),
  createAlias: (request) => binding().CreateAlias(request),
  deleteAlias: (profile, name) => binding().DeleteAlias(profile, name),
  setAliasAccount: (request) => binding().SetAliasAccount(request),
  savePreferences: (preferences) => binding().SavePreferences(fromPreferences(preferences)),
  saveWorkspace: (workspace) => binding().SaveWorkspace(workspace),
  getAuthStatus: (profile) => binding().GetAuthStatus(profile),
  refreshAuth: (profile) => binding().RefreshAuth(profile),
  startLogin: (profile) => binding().StartLogin(profile),
  completeLogin: (attemptID, callbackURL) => binding().CompleteLogin(attemptID, callbackURL),
  cancelLogin: (attemptID) => binding().CancelLogin(attemptID),
  logout: (profile) => binding().Logout(profile),
  licenseText: () => binding().LicenseText(),
  openConfig: () => binding().OpenConfig(),
  listMonospaceFonts: () => binding().ListMonospaceFonts(),
  startSSHSession: (request) => binding().StartSSHSession(request),
  listSSHSessions: () => binding().ListSSHSessions(),
  writeSSHSession: (id, data) => binding().WriteSSHSession(id, data),
  resizeSSHSession: (id, columns, rows) => binding().ResizeSSHSession(id, columns, rows),
  closeSSHSession: (id) => binding().CloseSSHSession(id),
  resolveSSHHostKey: (id, accepted) => binding().ResolveSSHHostKey(id, accepted),
  onSessionState: (handler) => subscribe('ssh:state', handler),
  onSessionOutput: (handler) => subscribe('ssh:output', handler),
  onSessionLatency: (handler) => subscribe('ssh:latency', handler),
  onHostKeyPrompt: (handler) => subscribe('ssh:host-key', handler),
}
