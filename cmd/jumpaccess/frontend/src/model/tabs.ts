export type SingletonTabKind = 'assets' | 'profiles' | 'settings'

export interface SingletonTab {
  id: `system:${SingletonTabKind}`
  kind: SingletonTabKind
}

export type SSHConnectionStatus = 'disconnected' | 'connecting' | 'active' | 'reconnecting' | 'failed'

export interface SSHDescriptor {
  profile: string
  organization: string
  target: string
  alias: string
  assetID: string
  assetName: string
  account: string
}

export interface SSHTab {
  id: string
  kind: 'ssh'
  descriptor: SSHDescriptor
  connectionStatus: SSHConnectionStatus
  sessionID?: string
  error?: string
}

export type AppTab = SingletonTab | SSHTab

export interface TabWorkspace {
  tabs: AppTab[]
  activeTabID: string
}

export type TabAction = {
  type: 'open-singleton'
  kind: SingletonTabKind
} | {
  type: 'open-ssh'
  id: string
  descriptor: SSHDescriptor
} | {
  type: 'close'
  id: string
} | {
  type: 'move'
  id: string
  toIndex: number
} | {
  type: 'hydrate'
  workspace: TabWorkspace
} | {
  type: 'attach-session'
  tabID: string
  sessionID: string
} | {
  type: 'session-state'
  sessionID: string
  status: 'active' | 'closed' | 'failed'
  error: string
} | {
  type: 'drop-profile'
  profile: string
} | {
  type: 'activate'
  id: string
} | {
  type: 'begin-connection'
  tabID: string
  reconnecting: boolean
} | {
  type: 'connection-error'
  tabID: string
  error: string
}

export const emptyTabWorkspace: TabWorkspace = { tabs: [], activeTabID: '' }

export function reduceTabs(state: TabWorkspace, action: TabAction): TabWorkspace {
  if (action.type === 'begin-connection') {
    return {
      ...state,
      tabs: state.tabs.map((tab) => tab.id === action.tabID && tab.kind === 'ssh'
        ? { ...tab, connectionStatus: action.reconnecting ? 'reconnecting' : 'connecting', error: undefined }
        : tab),
    }
  }
  if (action.type === 'connection-error') {
    return {
      ...state,
      tabs: state.tabs.map((tab) => tab.id === action.tabID && tab.kind === 'ssh'
        ? { ...tab, connectionStatus: 'failed', error: action.error, sessionID: undefined }
        : tab),
    }
  }
  if (action.type === 'activate') {
    return state.tabs.some((tab) => tab.id === action.id) ? { ...state, activeTabID: action.id } : state
  }
  if (action.type === 'drop-profile') {
    const belongsToProfile = (tab: AppTab) => tab.kind === 'ssh' && tab.descriptor.profile === action.profile
    const tabs = state.tabs.filter((tab) => !belongsToProfile(tab))
    const activeIndex = state.tabs.findIndex((tab) => tab.id === state.activeTabID)
    if (activeIndex < 0 || !belongsToProfile(state.tabs[activeIndex])) return { ...state, tabs }
    const right = state.tabs.slice(activeIndex + 1).find((tab) => !belongsToProfile(tab))
    const left = state.tabs.slice(0, activeIndex).reverse().find((tab) => !belongsToProfile(tab))
    return { tabs, activeTabID: right?.id ?? left?.id ?? '' }
  }
  if (action.type === 'session-state') {
    return {
      ...state,
      tabs: state.tabs.map((tab) => {
        if (tab.kind !== 'ssh' || tab.sessionID !== action.sessionID) return tab
        if (action.status === 'active') return { ...tab, connectionStatus: 'active', error: undefined }
        const { error: _error, sessionID: _sessionID, ...disconnected } = tab
        if (action.status === 'failed') {
          return { ...disconnected, connectionStatus: 'failed', error: action.error }
        }
        return { ...disconnected, connectionStatus: 'disconnected' }
      }),
    }
  }
  if (action.type === 'attach-session') {
    return {
      ...state,
      tabs: state.tabs.map((tab) => tab.id === action.tabID && tab.kind === 'ssh'
        ? { ...tab, connectionStatus: 'connecting', sessionID: action.sessionID }
        : tab),
    }
  }
  if (action.type === 'hydrate') {
    const singletonKinds = new Set<SingletonTabKind>()
    const tabs: AppTab[] = []
    for (const tab of action.workspace.tabs) {
      if (tab.kind === 'ssh') {
        tabs.push({
          id: tab.id,
          kind: 'ssh',
          descriptor: tab.descriptor,
          connectionStatus: 'disconnected',
        })
      } else if (!singletonKinds.has(tab.kind)) {
        singletonKinds.add(tab.kind)
        tabs.push({ id: `system:${tab.kind}` as SingletonTab['id'], kind: tab.kind })
      }
    }
    const activeTabID = tabs.some((tab) => tab.id === action.workspace.activeTabID)
      ? action.workspace.activeTabID
      : (tabs[0]?.id ?? '')
    return { tabs, activeTabID }
  }
  if (action.type === 'move') {
    const fromIndex = state.tabs.findIndex((tab) => tab.id === action.id)
    if (fromIndex < 0) return state
    const tabs = [...state.tabs]
    const [tab] = tabs.splice(fromIndex, 1)
    tabs.splice(Math.max(0, Math.min(action.toIndex, tabs.length)), 0, tab)
    return { ...state, tabs }
  }
  if (action.type === 'close') {
    const closedIndex = state.tabs.findIndex((tab) => tab.id === action.id)
    if (closedIndex < 0) return state
    const tabs = state.tabs.filter((tab) => tab.id !== action.id)
    return {
      tabs,
      activeTabID: state.activeTabID === action.id
        ? (tabs[Math.min(closedIndex, tabs.length - 1)]?.id ?? '')
        : state.activeTabID,
    }
  }
  if (action.type === 'open-ssh') {
    const tab: SSHTab = {
      id: action.id,
      kind: 'ssh',
      descriptor: action.descriptor,
      connectionStatus: 'disconnected',
    }
    return { tabs: [...state.tabs, tab], activeTabID: tab.id }
  }
  const id = `system:${action.kind}` as const
  if (state.tabs.some((tab) => tab.id === id)) return { ...state, activeTabID: id }
  return { tabs: [...state.tabs, { id, kind: action.kind }], activeTabID: id }
}
