import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, test, vi } from 'vitest'

import App from './App'
import type { AssetDetail, AssetPage, Backend, BootstrapState, HostKeyPrompt, SessionLatency, SessionOutput, SessionState } from './lib/backend'

const { terminalKeyHandlers, terminalOscHandlers, terminalState, terminalWrites } = vi.hoisted(() => ({
  terminalKeyHandlers: [] as Array<(event: KeyboardEvent) => boolean>,
  terminalOscHandlers: new Map<number, (data: string) => boolean | Promise<boolean>>(),
  terminalState: {
    pasted: [] as string[],
    selection: '',
    selectionHandler: undefined as (() => void) | undefined,
  },
  terminalWrites: [] as string[],
}))

vi.mock('@xterm/xterm', () => ({
  Terminal: class {
    cols = 120
    rows = 34
    parser = {
      registerOscHandler: (ident: number, handler: (data: string) => boolean | Promise<boolean>) => {
        terminalOscHandlers.set(ident, handler)
        return { dispose: () => terminalOscHandlers.delete(ident) }
      },
    }
    loadAddon() {}
    open() {}
    write(value: string) { terminalWrites.push(value) }
    reset() {}
    focus() {}
    getSelection() { return terminalState.selection }
    hasSelection() { return terminalState.selection.length > 0 }
    paste(value: string) { terminalState.pasted.push(value) }
    dispose() {}
    onData() { return { dispose() {} } }
    onResize() { return { dispose() {} } }
    onSelectionChange(handler: () => void) { terminalState.selectionHandler = handler; return { dispose() {} } }
    attachCustomKeyEventHandler(handler: (event: KeyboardEvent) => boolean) { terminalKeyHandlers.push(handler) }
  },
}))

vi.mock('@xterm/addon-fit', () => ({
  FitAddon: class {
    fit() {}
  },
}))

const bootstrapState: BootstrapState = {
  version: '0.1.0',
  currentProfile: 'production',
  currentOrganization: 'org-1',
  profiles: [{
    name: 'production',
    url: 'https://jump.example.test',
    organization: 'org-1',
    aliasCount: 2,
    auth: { loggedIn: true, expired: false, refreshAvailable: true, expiresAt: '2026-08-29T12:00:00Z' },
  }],
  preferences: {
    version: 3,
    theme: 'light',
    terminalFontFamily: 'JetBrains Mono',
    terminalFontSize: 13,
    terminalRightClickAction: 'paste',
    confirmCloseActiveSession: true,
    showTabCloseButtons: true,
  },
  workspace: { activeTabId: 'system:assets', tabs: [{ id: 'system:assets', type: 'assets' }] },
}

const assetPage: AssetPage = {
  count: 1,
  offset: 0,
  limit: 25,
  aliasCount: 2,
  results: [{
    id: 'asset-1',
    name: 'prod-web-01',
    address: '10.0.0.1',
    type: 'Linux',
    category: 'Host',
    aliases: [
      { name: 'production-web', asset: 'asset-1', account: 'account-1', organization: 'org-1' },
      { name: 'web-any', asset: 'asset-1', account: '', organization: 'org-1' },
    ],
  }],
}

const assetDetail: AssetDetail = {
  ...assetPage.results[0],
  accounts: [
    { id: 'account-1', name: 'deploy', alias: '', username: 'deploy' },
    { id: 'account-2', name: 'ops', alias: '', username: 'ops' },
  ],
  protocols: [{ name: 'ssh', port: 22 }],
}

function makeBackend(overrides: Partial<Backend> = {}): Backend {
  const session: SessionState = {
    id: 'session-1', status: 'connecting', title: 'production-web', profile: 'production',
    organization: 'org-1', asset: 'asset-1', account: 'account-1', error: '',
  }
  return {
    bootstrap: vi.fn().mockResolvedValue(bootstrapState),
    listOrganizations: vi.fn().mockResolvedValue([{ id: 'org-1', name: '研发中心' }]),
    listAssets: vi.fn().mockResolvedValue(assetPage),
    getAsset: vi.fn().mockResolvedValue(assetDetail),
    quickSearch: vi.fn().mockResolvedValue(assetPage.results),
    addProfile: vi.fn().mockResolvedValue(undefined),
    updateProfileURL: vi.fn().mockResolvedValue(undefined),
    deleteProfile: vi.fn().mockResolvedValue(undefined),
    useProfile: vi.fn().mockResolvedValue(undefined),
    setOrganization: vi.fn().mockResolvedValue(undefined),
    createAlias: vi.fn().mockResolvedValue(assetPage.results[0].aliases[0]),
    deleteAlias: vi.fn().mockResolvedValue(undefined),
    renameAlias: vi.fn().mockResolvedValue(assetPage.results[0].aliases[0]),
    setAliasAccount: vi.fn().mockResolvedValue(undefined),
    minimizeWindow: vi.fn().mockResolvedValue(undefined),
    ensureWindowVisible: vi.fn().mockResolvedValue(undefined),
    savePreferences: vi.fn().mockResolvedValue(undefined),
    saveWorkspace: vi.fn().mockResolvedValue(undefined),
    getAuthStatus: vi.fn().mockResolvedValue(bootstrapState.profiles[0].auth),
    refreshAuth: vi.fn().mockResolvedValue(bootstrapState.profiles[0].auth),
    startLogin: vi.fn().mockResolvedValue({ id: 'login-1', profile: 'production', expiresAt: '2026-08-29T12:00:00Z' }),
    completeLogin: vi.fn().mockResolvedValue(bootstrapState.profiles[0].auth),
    cancelLogin: vi.fn().mockResolvedValue(undefined),
    logout: vi.fn().mockResolvedValue(undefined),
    licenseText: vi.fn().mockResolvedValue('MIT License'),
    openConfig: vi.fn().mockResolvedValue(undefined),
    listMonospaceFonts: vi.fn().mockResolvedValue([]),
    startSSHSession: vi.fn().mockResolvedValue(session),
    listSSHSessions: vi.fn().mockResolvedValue([]),
    writeSSHSession: vi.fn().mockResolvedValue(undefined),
    resizeSSHSession: vi.fn().mockResolvedValue(undefined),
    closeSSHSession: vi.fn().mockResolvedValue(undefined),
    resolveSSHHostKey: vi.fn().mockResolvedValue(undefined),
    onSessionState: vi.fn().mockReturnValue(() => undefined),
    onSessionOutput: vi.fn().mockReturnValue(() => undefined),
    onSessionLatency: vi.fn().mockReturnValue(() => undefined),
    onHostKeyPrompt: vi.fn().mockReturnValue(() => undefined),
    ...overrides,
  }
}

test('恢复空工作区时显示起始页而不自动创建资产 Tab', async () => {
  const backend = makeBackend({
    bootstrap: vi.fn().mockResolvedValue({
      ...bootstrapState,
      workspace: { activeTabId: '', tabs: [] },
    }),
  })

  render(<App backend={backend} />)

  expect(await screen.findByRole('heading', { name: '开始使用 JumpAccess' })).toBeInTheDocument()
  expect(screen.queryByRole('heading', { name: '资产' })).not.toBeInTheDocument()
})

test('后端返回 null tabs 时按空工作区启动', async () => {
  const backend = makeBackend({
    bootstrap: vi.fn().mockResolvedValue({
      ...bootstrapState,
      workspace: { activeTabId: '', tabs: null } as unknown as BootstrapState['workspace'],
    }),
  })

  render(<App backend={backend} />)

  expect(await screen.findByRole('heading', { name: '开始使用 JumpAccess' })).toBeInTheDocument()
  expect(screen.queryByRole('alert')).not.toBeInTheDocument()
})

test('没有可用 Profile 时启动后自动打开 Profile 页面', async () => {
  const backend = makeBackend({
    bootstrap: vi.fn().mockResolvedValue({
      ...bootstrapState,
      currentProfile: '',
      currentOrganization: '',
      profiles: [],
    }),
  })

  render(<App backend={backend} />)

  expect(await screen.findByRole('heading', { name: 'Profile' })).toBeInTheDocument()
  expect(screen.getByRole('tab', { name: 'Profile' })).toHaveAttribute('aria-selected', 'true')
  expect(screen.getByRole('tab', { name: '资产' })).toHaveAttribute('aria-selected', 'false')
  const profileButton = screen.getByRole('button', { name: '打开 Profile，认证状态：未选择 Profile，需要登录' })
  expect(profileButton.querySelector('.profile-status-icon .auth-indicator')).toHaveClass('offline')
  expect(screen.queryByRole('button', { name: /^认证状态：/ })).not.toBeInTheDocument()
})

test('当前 Profile 未登录时启动后自动打开 Profile 页面', async () => {
  const listOrganizations = vi.fn().mockRejectedValue(new Error('login required for profile "production"; run jumpctl auth login'))
  const listAssets = vi.fn().mockRejectedValue(new Error('login required for profile "production"; run jumpctl auth login'))
  const backend = makeBackend({
    bootstrap: vi.fn().mockResolvedValue({
      ...bootstrapState,
      profiles: bootstrapState.profiles.map((item) => ({
        ...item,
        auth: { loggedIn: false, expired: false, refreshAvailable: false, expiresAt: '' },
      })),
    }),
    listOrganizations,
    listAssets,
  })

  render(<App backend={backend} />)

  expect(await screen.findByRole('heading', { name: 'Profile' })).toBeInTheDocument()
  expect(screen.getByRole('tab', { name: 'Profile' })).toHaveAttribute('aria-selected', 'true')
  expect(screen.getByRole('tab', { name: '资产' })).toHaveAttribute('aria-selected', 'false')
  expect(listOrganizations).not.toHaveBeenCalled()
  expect(listAssets).not.toHaveBeenCalled()
  expect(screen.queryByRole('alert')).not.toBeInTheDocument()
})

test('Tab 激活控件使用标准 tab 语义和 roving tabindex', async () => {
  const backend = makeBackend()
  const user = userEvent.setup()
  render(<App backend={backend} />)

  const assetsTab = await screen.findByRole('tab', { name: '资产' })
  expect(assetsTab.tagName).toBe('BUTTON')
  expect(assetsTab).toHaveAttribute('tabindex', '0')
  await user.click(screen.getByRole('button', { name: /^打开 Profile/ }))

  expect(screen.getByRole('tab', { name: '资产' })).toHaveAttribute('tabindex', '-1')
  expect(screen.getByRole('tab', { name: 'Profile' })).toHaveAttribute('tabindex', '0')
})

test('仅在相邻的未选中 Tab 之间显示分隔线', async () => {
  const backend = makeBackend({
    bootstrap: vi.fn().mockResolvedValue({
      ...bootstrapState,
      workspace: {
        activeTabId: 'system:assets',
        tabs: [
          { id: 'system:assets', type: 'assets' },
          { id: 'system:profiles', type: 'profiles' },
          { id: 'system:settings', type: 'settings' },
        ],
      },
    }),
  })
  const user = userEvent.setup()
  render(<App backend={backend} />)

  const tablist = await screen.findByRole('tablist', { name: '工作区 Tab' })
  expect(tablist.querySelectorAll('.tab-separator')).toHaveLength(1)
  expect(tablist.querySelector('.tab-separator')?.parentElement).toContainElement(screen.getByRole('tab', { name: 'Profile' }))

  await user.click(screen.getByRole('tab', { name: '设置' }))
  expect(tablist.querySelectorAll('.tab-separator')).toHaveLength(1)
  expect(tablist.querySelector('.tab-separator')?.parentElement).toContainElement(screen.getByRole('tab', { name: '资产' }))
})

test('鼠标中键关闭 Tab，右键不会误关闭', async () => {
  const backend = makeBackend()
  render(<App backend={backend} />)

  const assetsTab = await screen.findByRole('tab', { name: '资产' })
  fireEvent(assetsTab, new MouseEvent('auxclick', { bubbles: true, button: 2 }))
  expect(screen.getByRole('tab', { name: '资产' })).toBeInTheDocument()

  fireEvent(assetsTab, new MouseEvent('auxclick', { bubbles: true, button: 1 }))
  expect(await screen.findByRole('heading', { name: '开始使用 JumpAccess' })).toBeInTheDocument()
  expect(screen.queryByRole('tab', { name: '资产' })).not.toBeInTheDocument()
})

test('鼠标拖拽 Tab 会调整顺序并保存工作区', async () => {
  const backend = makeBackend()
  const user = userEvent.setup()
  render(<App backend={backend} />)

  await screen.findByRole('tab', { name: '资产' })
  await user.click(screen.getByRole('button', { name: /^打开 Profile/ }))
  await user.click(screen.getByRole('button', { name: '打开设置' }))
  await waitFor(() => expect(backend.saveWorkspace).toHaveBeenLastCalledWith(expect.objectContaining({
    tabs: [
      expect.objectContaining({ type: 'assets' }),
      expect.objectContaining({ type: 'profiles' }),
      expect.objectContaining({ type: 'settings' }),
    ],
  })))

  const values: Record<string, string> = {}
  const dataTransfer = {
    dropEffect: 'none',
    effectAllowed: 'none',
    getData: (type: string) => values[type] ?? '',
    setData: (type: string, value: string) => { values[type] = value },
  }
  const assetsTab = screen.getByRole('tab', { name: '资产' })
  const settingsTab = screen.getByRole('tab', { name: '设置' })
  fireEvent.dragStart(assetsTab, { dataTransfer })
  fireEvent.dragOver(settingsTab, { dataTransfer })
  fireEvent.drop(settingsTab, { dataTransfer })

  expect(screen.getAllByRole('tab').map((tab) => tab.textContent)).toEqual(['Profile', '设置', '资产'])
  await waitFor(() => expect(backend.saveWorkspace).toHaveBeenLastCalledWith(expect.objectContaining({
    tabs: [
      expect.objectContaining({ type: 'profiles' }),
      expect.objectContaining({ type: 'settings' }),
      expect.objectContaining({ type: 'assets' }),
    ],
  })))
})

test('Windows 关闭按钮等待工作区保存完成后再退出', async () => {
  let resolveSave: () => void = () => undefined
  const saveWorkspace = vi.fn().mockReturnValue(new Promise<void>((resolve) => { resolveSave = resolve }))
  const backend = makeBackend({ saveWorkspace })
  const quit = vi.fn()
  const previousRuntime = window.runtime
  window.runtime = {
    EventsOnMultiple: vi.fn().mockReturnValue(() => undefined),
    Quit: quit,
  }
  const user = userEvent.setup()

  try {
    render(<App backend={backend} />)
    await screen.findByRole('heading', { name: '资产' })
    await waitFor(() => expect(saveWorkspace).toHaveBeenCalled())
    await user.click(screen.getByRole('button', { name: '关闭窗口' }))

    expect(quit).not.toHaveBeenCalled()
    resolveSave()
    await waitFor(() => expect(quit).toHaveBeenCalledTimes(1))
  } finally {
    window.runtime = previousRuntime
  }
})

test('Windows 最小化交给后端记录当前显示器，恢复焦点时校正窗口可见性', async () => {
  const backend = makeBackend()
  const user = userEvent.setup()
  render(<App backend={backend} />)

  await screen.findByRole('heading', { name: '资产' })
  await user.click(screen.getByRole('button', { name: '最小化' }))
  expect(backend.minimizeWindow).toHaveBeenCalledTimes(1)

  fireEvent.focus(window)
  expect(backend.ensureWindowVisible).toHaveBeenCalledTimes(1)
})

test('恢复 SSH Tab 时保持断连并且不自动连接', async () => {
  terminalKeyHandlers.length = 0
  terminalWrites.length = 0
  const backend = makeBackend({
    bootstrap: vi.fn().mockResolvedValue({
      ...bootstrapState,
      workspace: {
        activeTabId: 'ssh-restored',
        tabs: [{
          id: 'ssh-restored', type: 'ssh', profile: 'production', organization: 'org-1',
          target: 'production-web', account: 'account-1', alias: 'production-web',
          assetId: 'asset-1', assetName: 'prod-web-01',
        }],
      },
    }),
  })

  render(<App backend={backend} />)

  const restored = await screen.findByRole('tab', { name: /production-web/ })
  expect(restored).toHaveTextContent('production-webprod-web-01')
  expect(restored).toHaveAttribute('title', expect.stringContaining('ID: asset-1'))
  expect(backend.startSSHSession).not.toHaveBeenCalled()
  await waitFor(() => expect(terminalWrites.join('')).toContain('Connection closed.\r\n\r\nPress Enter to reconnect ...'))
})

test('断连 SSH Tab 连按 Enter 只启动一次重连', async () => {
  terminalKeyHandlers.length = 0
  let resolveStart: (session: SessionState) => void = () => undefined
  const start = new Promise<SessionState>((resolve) => { resolveStart = resolve })
  const backend = makeBackend({
    bootstrap: vi.fn().mockResolvedValue({
      ...bootstrapState,
      workspace: {
        activeTabId: 'ssh-restored',
        tabs: [{
          id: 'ssh-restored', type: 'ssh', profile: 'production', organization: 'org-1',
          target: 'production-web', account: 'account-1', alias: 'production-web',
          assetId: 'asset-1', assetName: 'prod-web-01',
        }],
      },
    }),
    startSSHSession: vi.fn().mockReturnValue(start),
  })
  render(<App backend={backend} />)
  await screen.findByRole('tab', { name: /production-web/ })
  await waitFor(() => expect(terminalKeyHandlers.length).toBeGreaterThan(0))
  const enter = {
    type: 'keydown', key: 'Enter', ctrlKey: false, altKey: false, shiftKey: false, metaKey: false,
    preventDefault: vi.fn(),
  } as unknown as KeyboardEvent

  act(() => {
    terminalKeyHandlers.at(-1)?.(enter)
    terminalKeyHandlers.at(-1)?.(enter)
  })

  expect(backend.startSSHSession).toHaveBeenCalledTimes(1)
  resolveStart({
    id: 'reconnected-1', status: 'connecting', title: 'production-web', profile: 'production',
    organization: 'org-1', asset: 'asset-1', account: 'account-1', error: '',
  })
})

test('连接结果返回前关闭 Tab 会回收后续创建的 Session', async () => {
  let resolveStart: (session: SessionState) => void = () => undefined
  const start = new Promise<SessionState>((resolve) => { resolveStart = resolve })
  const backend = makeBackend({ startSSHSession: vi.fn().mockReturnValue(start) })
  const user = userEvent.setup()
  render(<App backend={backend} />)
  await screen.findByRole('heading', { name: '资产' })
  await user.click(await screen.findByRole('button', { name: '使用 production-web 连接' }))
  await user.click(await screen.findByRole('button', { name: '关闭 production-web Tab' }))
  expect(await screen.findByRole('dialog', { name: '关闭 SSH Tab' })).toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: '关闭 Tab' }))

  await act(async () => resolveStart({
    id: 'late-session', status: 'connecting', title: 'production-web', profile: 'production',
    organization: 'org-1', asset: 'asset-1', account: 'account-1', error: '',
  }))

  await waitFor(() => expect(backend.closeSSHSession).toHaveBeenCalledWith('late-session'))
  expect(screen.queryByRole('tab', { name: /production-web/ })).not.toBeInTheDocument()
})

test('顶部单例页不重复打开，且允许关闭最后一个 Tab', async () => {
  const backend = makeBackend()
  const user = userEvent.setup()
  render(<App backend={backend} />)
  await screen.findByRole('heading', { name: '资产' })

  await user.click(screen.getByRole('button', { name: /^打开 Profile/ }))
  await user.click(screen.getByRole('button', { name: /^打开 Profile/ }))
  expect(screen.getAllByRole('tab')).toHaveLength(2)
  await user.click(screen.getByRole('button', { name: '关闭 Profile Tab' }))
  await user.click(screen.getByRole('button', { name: '关闭 资产 Tab' }))

  expect(await screen.findByRole('heading', { name: '开始使用 JumpAccess' })).toBeInTheDocument()
  await waitFor(() => expect(backend.saveWorkspace).toHaveBeenLastCalledWith({ activeTabId: '', tabs: [] }))
})

test('macOS 保留原生 traffic lights，Profile 状态图标右侧不渲染自定义窗口按钮', async () => {
  const platform = vi.spyOn(window.navigator, 'platform', 'get').mockReturnValue('MacIntel')
  render(<App backend={makeBackend()} />)

  await screen.findByRole('heading', { name: '资产' })
  expect(screen.queryByLabelText('窗口控制')).not.toBeInTheDocument()
  expect(screen.getByRole('button', { name: '打开 Profile，认证状态：production，已认证' })).toBeInTheDocument()
  platform.mockRestore()
})

test('加载分页资产，搜索 Alias，并支持立即同步', async () => {
  const backend = makeBackend()
  const user = userEvent.setup()
  render(<App backend={backend} />)

  expect(await screen.findByRole('heading', { name: '资产' })).toBeInTheDocument()
  await waitFor(() => expect(backend.listAssets).toHaveBeenCalledWith({
    profile: 'production', organization: 'org-1', search: '', offset: 0, limit: 25,
  }))
  const search = screen.getByRole('searchbox', { name: '搜索资产或 Alias' })
  await user.type(search, 'production-web')
  await waitFor(() => expect(backend.listAssets).toHaveBeenLastCalledWith(expect.objectContaining({ search: 'production-web', offset: 0 })))

  const callsBeforeRefresh = vi.mocked(backend.listAssets).mock.calls.length
  await user.click(screen.getByRole('button', { name: '立即同步' }))
  await waitFor(() => expect(vi.mocked(backend.listAssets).mock.calls.length).toBeGreaterThan(callsBeforeRefresh))
  expect(screen.getByText(/最近同步/)).toBeInTheDocument()
})

test('资产详情不显示 Gateway 主机密钥提示', async () => {
  render(<App backend={makeBackend()} />)

  await screen.findByRole('heading', { name: '资产' })
  await screen.findByRole('heading', { name: 'prod-web-01' })
  expect(screen.queryByText('严格校验 Gateway 主机密钥')).not.toBeInTheDocument()
})

test('资产详情中的 Asset ID 使用保留开头的可收缩文本元素', async () => {
  render(<App backend={makeBackend()} />)

  const copyButton = await screen.findByRole('button', { name: '复制 Asset ID' })
  const value = copyButton.parentElement?.querySelector('.asset-id-text')

  expect(value).toHaveTextContent('asset-1')
  expect(value).toHaveAttribute('title', 'asset-1')
})

test('在资产行内纵向展示全部 Alias，并分别绑定账号和连接', async () => {
  const backend = makeBackend()
  const user = userEvent.setup()
  render(<App backend={backend} />)

  const row = await screen.findByTestId('asset-row-asset-1')
  expect(within(row).getByText('production-web')).toBeInTheDocument()
  expect(within(row).getByText('web-any')).toBeInTheDocument()
  expect(within(row).queryByRole('button', { name: '创建 Alias' })).not.toBeInTheDocument()
  expect(screen.queryByRole('button', { name: '为资产创建 Alias' })).not.toBeInTheDocument()

  const accountSelect = within(row).getByLabelText('production-web 默认账号')
  await waitFor(() => expect(accountSelect).toHaveDisplayValue('deploy'))
  await user.selectOptions(accountSelect, 'account-2')
  await waitFor(() => expect(backend.setAliasAccount).toHaveBeenCalledWith({
    profile: 'production', name: 'production-web', account: 'account-2',
  }))

  await user.click(within(row).getByRole('button', { name: '使用 production-web 连接' }))
  await waitFor(() => expect(backend.startSSHSession).toHaveBeenCalledWith(expect.objectContaining({
    profile: 'production', organization: 'org-1', target: 'production-web', account: 'account-2',
  })))
})

test('已有 Alias 的资产仍可从更多操作菜单继续创建', async () => {
  const backend = makeBackend()
  const user = userEvent.setup()
  render(<App backend={backend} />)

  const row = await screen.findByTestId('asset-row-asset-1')
  await user.click(within(row).getByRole('button', { name: 'prod-web-01 更多操作' }))
  const menu = screen.getByRole('menu')
  await user.click(within(menu).getByRole('menuitem', { name: '创建 Alias' }))

  expect(await screen.findByRole('dialog', { name: '创建 Alias' })).toBeInTheDocument()
})

test('删除 Alias 使用应用内确认对话框', async () => {
  const backend = makeBackend()
  const user = userEvent.setup()
  const systemConfirm = vi.spyOn(window, 'confirm').mockReturnValue(true)

  try {
    render(<App backend={backend} />)
    const row = await screen.findByTestId('asset-row-asset-1')

    await user.click(within(row).getByRole('button', { name: '删除 production-web' }))
    expect(systemConfirm).not.toHaveBeenCalled()
    let dialog = await screen.findByRole('dialog', { name: '删除 Alias' })
    expect(within(dialog).getByText('production-web')).toBeInTheDocument()
    expect(backend.deleteAlias).not.toHaveBeenCalled()
    await user.click(within(dialog).getByRole('button', { name: '取消' }))
    expect(screen.queryByRole('dialog', { name: '删除 Alias' })).not.toBeInTheDocument()

    await user.click(within(row).getByRole('button', { name: '删除 production-web' }))
    dialog = await screen.findByRole('dialog', { name: '删除 Alias' })
    await user.click(within(dialog).getByRole('button', { name: '确认删除 production-web' }))
    await waitFor(() => expect(backend.deleteAlias).toHaveBeenCalledWith('production', 'production-web'))
    expect(within(row).queryByText('production-web')).not.toBeInTheDocument()
  } finally {
    systemConfirm.mockRestore()
  }
})

test('编辑 Alias 名称时保留资产和默认账号', async () => {
  const renamedAlias = { ...assetPage.results[0].aliases[0], name: 'primary-web' }
  const backend = makeBackend({ renameAlias: vi.fn().mockResolvedValue(renamedAlias) })
  const user = userEvent.setup()
  render(<App backend={backend} />)

  const row = await screen.findByTestId('asset-row-asset-1')
  await user.click(within(row).getByRole('button', { name: '编辑 production-web' }))
  const dialog = await screen.findByRole('dialog', { name: '编辑 Alias' })
  const nameInput = within(dialog).getByLabelText('Alias 名称')
  expect(nameInput).toHaveValue('production-web')
  await user.clear(nameInput)
  await user.type(nameInput, 'primary-web')
  await user.click(within(dialog).getByRole('button', { name: '保存 Alias' }))

  await waitFor(() => expect(backend.renameAlias).toHaveBeenCalledWith({
    profile: 'production', currentName: 'production-web', newName: 'primary-web',
  }))
  expect(within(row).getByText('primary-web')).toBeInTheDocument()
  expect(within(row).getByLabelText('primary-web 默认账号')).toHaveDisplayValue('deploy')
  expect(within(row).getByText('web-any')).toBeInTheDocument()
})

test('点击资产行的 Alias 区域会打开详情，但操作控件不会切换资产', async () => {
  const secondAsset = { id: 'asset-2', name: 'new-server', address: '10.0.0.2', type: 'Linux', category: 'Host', aliases: [] }
  const secondDetail: AssetDetail = { ...secondAsset, accounts: assetDetail.accounts, protocols: assetDetail.protocols }
  const backend = makeBackend({
    listAssets: vi.fn().mockResolvedValue({ ...assetPage, count: 2, results: [...assetPage.results, secondAsset] }),
    getAsset: vi.fn(({ asset }) => Promise.resolve(asset === 'asset-2' ? secondDetail : assetDetail)),
  })
  const user = userEvent.setup()
  render(<App backend={backend} />)

  const firstRow = await screen.findByTestId('asset-row-asset-1')
  const secondRow = await screen.findByTestId('asset-row-asset-2')
  await user.click(within(secondRow).getAllByRole('cell')[2])
  expect(await screen.findByRole('heading', { name: 'new-server' })).toBeInTheDocument()

  await user.click(within(firstRow).getByText('production-web'))
  expect(await screen.findByRole('heading', { name: 'prod-web-01' })).toBeInTheDocument()

  await user.click(within(secondRow).getAllByRole('cell')[2])
  await user.click(within(firstRow).getByRole('button', { name: 'prod-web-01 更多操作' }))
  expect(screen.getByRole('heading', { name: 'new-server' })).toBeInTheDocument()
})

test('从资产连接且存在多个账号时要求明确选择', async () => {
  const backend = makeBackend()
  const user = userEvent.setup()
  render(<App backend={backend} />)

  await screen.findByTestId('asset-row-asset-1')
  await user.click(screen.getByRole('button', { name: '连接 prod-web-01' }))
  expect(await screen.findByRole('dialog', { name: '选择连接账号' })).toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: /ops/ }))
  await waitFor(() => expect(backend.startSSHSession).toHaveBeenCalledWith(expect.objectContaining({
    target: 'asset-1', account: 'account-2',
  })))
})

test('设置主题会持久化 GUI 独有偏好，许可证位于关于栏', async () => {
  const backend = makeBackend()
  const user = userEvent.setup()
  render(<App backend={backend} />)

  await screen.findByRole('heading', { name: '资产' })
  await user.click(screen.getByRole('button', { name: '打开设置' }))
  const aboutSection = screen.getByRole('heading', { name: '关于 JumpAccess' }).closest('section')
  expect(aboutSection).not.toBeNull()
  expect(within(aboutSection!).getByRole('img', { name: 'JumpAccess 应用图标' })).toHaveAttribute(
    'src',
    expect.stringContaining('appicon.svg'),
  )
  await user.click(screen.getByRole('button', { name: '深色' }))
  await waitFor(() => expect(backend.savePreferences).toHaveBeenCalledWith(expect.objectContaining({ theme: 'dark' })))
  expect(document.documentElement).toHaveClass('dark')

  await user.click(screen.getByRole('button', { name: '查看许可证' }))
  expect(await screen.findByRole('dialog', { name: '开源许可证' })).toHaveTextContent('MIT License')
})

test('设置页使用左侧导航和右侧单列滚动面板', async () => {
  const backend = makeBackend()
  const user = userEvent.setup()
  const originalScrollIntoView = HTMLElement.prototype.scrollIntoView
  const scrollIntoView = vi.fn()
  Object.defineProperty(HTMLElement.prototype, 'scrollIntoView', {
    configurable: true,
    value: scrollIntoView,
  })

  try {
    render(<App backend={backend} />)

    await screen.findByRole('heading', { name: '资产' })
    await user.click(screen.getByRole('button', { name: '打开设置' }))

    const navigation = screen.getByRole('navigation', { name: '设置导航' })
    const navigationLabels = ['外观', '终端', 'Tab 行为', '关于 JumpAccess']
    expect(within(navigation).getAllByRole('button').map((button) => button.textContent)).toEqual(navigationLabels)

    const scrollContainer = screen.getByTestId('settings-scroll')
    const sectionIDs = ['settings-appearance', 'settings-terminal', 'settings-tabs', 'settings-about']
    expect(Array.from(scrollContainer.querySelectorAll(':scope > .settings-stack > section')).map((section) => section.id)).toEqual(sectionIDs)

    await user.click(within(navigation).getByRole('button', { name: '终端' }))
    expect(scrollIntoView).toHaveBeenCalledWith({ behavior: 'smooth', block: 'start' })
    expect(within(navigation).getByRole('button', { name: '终端' })).toHaveAttribute('aria-current', 'location')
  } finally {
    if (originalScrollIntoView) {
      Object.defineProperty(HTMLElement.prototype, 'scrollIntoView', {
        configurable: true,
        value: originalScrollIntoView,
      })
    } else {
      Reflect.deleteProperty(HTMLElement.prototype, 'scrollIntoView')
    }
  }
})

test('设置页滚动时同步选中对应的导航项', async () => {
  const backend = makeBackend()
  const user = userEvent.setup()
  render(<App backend={backend} />)

  await screen.findByRole('heading', { name: '资产' })
  await user.click(screen.getByRole('button', { name: '打开设置' }))

  const scrollContainer = screen.getByTestId('settings-scroll')
  const sectionOffsets: Record<string, number> = {
    'settings-appearance': 0,
    'settings-terminal': 220,
    'settings-tabs': 520,
    'settings-about': 760,
  }
  for (const [id, offsetTop] of Object.entries(sectionOffsets)) {
    Object.defineProperty(document.getElementById(id), 'offsetTop', { configurable: true, value: offsetTop })
  }
  Object.defineProperty(scrollContainer, 'scrollTop', { configurable: true, value: 500 })

  fireEvent.scroll(scrollContainer)

  const navigation = screen.getByRole('navigation', { name: '设置导航' })
  expect(within(navigation).getByRole('button', { name: 'Tab 行为' })).toHaveAttribute('aria-current', 'location')
  expect(within(navigation).getByRole('button', { name: '外观' })).not.toHaveAttribute('aria-current')
})

test('Tab 行为设置可隐藏关闭按钮且保留鼠标中键关闭', async () => {
  const backend = makeBackend()
  const user = userEvent.setup()
  render(<App backend={backend} />)

  await screen.findByRole('heading', { name: '资产' })
  expect(screen.getByRole('button', { name: '关闭 资产 Tab' })).toBeVisible()
  await user.click(screen.getByRole('button', { name: '打开设置' }))

  const section = screen.getByRole('heading', { name: 'Tab 行为' }).closest('section')
  expect(section).not.toBeNull()
  expect(section!.querySelector('.lucide-panel-top-close')).toBeInTheDocument()
  await user.click(within(section!).getByRole('switch', { name: '显示 Tab 关闭按钮' }))

  await waitFor(() => expect(backend.savePreferences).toHaveBeenCalledWith(expect.objectContaining({
    showTabCloseButtons: false,
  })))
  expect(screen.queryByRole('button', { name: '关闭 资产 Tab' })).not.toBeInTheDocument()
  expect(screen.queryByRole('button', { name: '关闭 设置 Tab' })).not.toBeInTheDocument()

  const assetsTab = screen.getByRole('tab', { name: '资产' }).closest('.workspace-tab')
  expect(assetsTab).not.toBeNull()
  fireEvent(assetsTab!, new MouseEvent('auxclick', { bubbles: true, button: 1 }))
  expect(screen.queryByRole('tab', { name: '资产' })).not.toBeInTheDocument()
})

test('终端交互设置可把鼠标右键切换为上下文菜单', async () => {
  const backend = makeBackend()
  const user = userEvent.setup()
  render(<App backend={backend} />)

  await screen.findByRole('heading', { name: '资产' })
  await user.click(screen.getByRole('button', { name: '打开设置' }))
  expect(screen.getByRole('heading', { name: '字体与配色' })).toBeVisible()
  expect(screen.getByRole('heading', { name: '交互' })).toBeVisible()

  await user.selectOptions(screen.getByLabelText('鼠标右键'), 'context_menu')

  await waitFor(() => expect(backend.savePreferences).toHaveBeenCalledWith(expect.objectContaining({
    terminalRightClickAction: 'context_menu',
  })))
})

test('设置页列出系统等宽字体并允许输入过滤后保存', async () => {
  const listMonospaceFonts = vi.fn().mockResolvedValue(['Menlo', 'JetBrains Mono', 'Fira Code'])
  const backend = makeBackend({ listMonospaceFonts })
  const user = userEvent.setup()
  render(<App backend={backend} />)

  await screen.findByRole('heading', { name: '资产' })
  await user.click(screen.getByRole('button', { name: '打开设置' }))
  await waitFor(() => expect(listMonospaceFonts).toHaveBeenCalledTimes(1))

  const fontInput = screen.getByLabelText('字体')
  expect(fontInput).toHaveRole('combobox')
  await user.click(screen.getByRole('button', { name: '展开字体列表' }))
  const listbox = screen.getByRole('listbox', { name: '系统等宽字体' })
  expect(within(listbox).getAllByRole('option')).toHaveLength(4)
  expect(within(listbox).getByRole('option', { name: /monospace.*系统默认/ })).toBeVisible()
  expect(within(listbox).getByRole('option', { name: 'Fira Code' })).toBeVisible()
  expect(within(listbox).getByRole('option', { name: 'Menlo' })).toBeVisible()
  expect(within(listbox).getByRole('option', { name: 'JetBrains Mono' })).toHaveAttribute('aria-selected', 'true')

  await user.click(fontInput)
  await user.clear(fontInput)
  await user.type(fontInput, 'Fira')
  expect(within(listbox).getByRole('option', { name: 'Fira Code' })).toBeVisible()
  expect(within(listbox).queryByRole('option', { name: 'Menlo' })).not.toBeInTheDocument()
  expect(backend.savePreferences).not.toHaveBeenCalled()
  await user.click(within(listbox).getByRole('option', { name: 'Fira Code' }))
  await waitFor(() => expect(backend.savePreferences).toHaveBeenCalledWith(expect.objectContaining({
    terminalFontFamily: 'Fira Code',
  })))
})

test('系统字体枚举失败时保留 monospace 和手工字体输入', async () => {
  const backend = makeBackend({ listMonospaceFonts: vi.fn().mockRejectedValue(new Error('unsupported')) })
  const user = userEvent.setup()
  render(<App backend={backend} />)

  await screen.findByRole('heading', { name: '资产' })
  await user.click(screen.getByRole('button', { name: '打开设置' }))

  const fontInput = screen.getByLabelText('字体')
  await user.click(screen.getByRole('button', { name: '展开字体列表' }))
  const listbox = await screen.findByRole('listbox', { name: '系统等宽字体' })
  expect(within(listbox).getByRole('option', { name: /monospace.*系统默认/ })).toBeVisible()

  await user.click(fontInput)
  await user.clear(fontInput)
  await user.type(fontInput, 'Custom Mono')
  await user.click(within(listbox).getByRole('option', { name: '使用 “Custom Mono”' }))
  await waitFor(() => expect(backend.savePreferences).toHaveBeenCalledWith(expect.objectContaining({
    terminalFontFamily: 'Custom Mono',
  })))
})

test('字体 Combobox 支持键盘选择和 Escape 撤销过滤', async () => {
  const backend = makeBackend({ listMonospaceFonts: vi.fn().mockResolvedValue(['Menlo']) })
  const user = userEvent.setup()
  render(<App backend={backend} />)

  await screen.findByRole('heading', { name: '资产' })
  await user.click(screen.getByRole('button', { name: '打开设置' }))
  const fontInput = screen.getByLabelText('字体')

  await user.click(fontInput)
  await user.keyboard('{ArrowDown}{Enter}')
  await waitFor(() => expect(backend.savePreferences).toHaveBeenCalledWith(expect.objectContaining({
    terminalFontFamily: 'monospace',
  })))

  await user.click(fontInput)
  await user.clear(fontInput)
  await user.type(fontInput, 'Men')
  await user.keyboard('{Escape}')
  expect(fontInput).toHaveValue('monospace')
  expect(backend.savePreferences).toHaveBeenCalledTimes(1)
})

test('路由 SSH 状态、输出和主机密钥确认事件', async () => {
  let stateHandler: (event: SessionState) => void = () => undefined
  let outputHandler: (event: SessionOutput) => void = () => undefined
  let hostKeyHandler: (event: HostKeyPrompt) => void = () => undefined
  const backend = makeBackend({
    onSessionState: vi.fn((handler) => { stateHandler = handler; return () => undefined }),
    onSessionOutput: vi.fn((handler) => { outputHandler = handler; return () => undefined }),
    onHostKeyPrompt: vi.fn((handler) => { hostKeyHandler = handler; return () => undefined }),
  })
  const user = userEvent.setup()
  terminalWrites.length = 0
  render(<App backend={backend} />)
  await screen.findByRole('heading', { name: '资产' })

  await user.click(await screen.findByRole('button', { name: '使用 production-web 连接' }))
  await waitFor(() => expect(backend.startSSHSession).toHaveBeenCalled())
  act(() => stateHandler({
    id: 'session-1', status: 'active', title: 'production-web', profile: 'production',
    organization: 'org-1', asset: 'asset-1', account: 'account-1', error: '',
  }))
  act(() => outputHandler({ id: 'session-1', data: 'hello from ssh\r\n' }))
  await waitFor(() => expect(terminalWrites).toContain('hello from ssh\r\n'))

  act(() => hostKeyHandler({ id: 'host-key-1', host: 'gateway.example.test:22', fingerprint: 'SHA256:abc123' }))
  expect(await screen.findByRole('dialog', { name: '确认新的 SSH Gateway' })).toHaveTextContent('SHA256:abc123')
  await user.click(screen.getByRole('button', { name: '拒绝' }))
  await waitFor(() => expect(backend.resolveSSHHostKey).toHaveBeenCalledWith('host-key-1', false))
})

test('远端断开后保留 SSH Tab 并追加 Enter 重连提示', async () => {
  let stateHandler: (event: SessionState) => void = () => undefined
  const backend = makeBackend({
    onSessionState: vi.fn((handler) => { stateHandler = handler; return () => undefined }),
  })
  const user = userEvent.setup()
  terminalWrites.length = 0
  render(<App backend={backend} />)
  await screen.findByRole('heading', { name: '资产' })
  await user.click(await screen.findByRole('button', { name: '使用 production-web 连接' }))
  await screen.findByRole('tab', { name: /production-web/ })

  act(() => stateHandler({
    id: 'session-1', status: 'active', title: 'production-web', profile: 'production',
    organization: 'org-1', asset: 'asset-1', account: 'account-1', error: '',
  }))
  act(() => stateHandler({
    id: 'session-1', status: 'closed', title: 'production-web', profile: 'production',
    organization: 'org-1', asset: 'asset-1', account: 'account-1', error: '',
  }))

  expect(screen.getByRole('tab', { name: /production-web/ })).toBeInTheDocument()
  await waitFor(() => expect(terminalWrites.join('')).toContain('Connection closed.\r\n\r\nPress Enter to reconnect ...'))
  await waitFor(() => expect(backend.closeSSHSession).toHaveBeenCalledWith('session-1'))
})

test('StartSSHSession 返回前到达的失败事件不会丢失', async () => {
  let stateHandler: (event: SessionState) => void = () => undefined
  terminalWrites.length = 0
  const earlyFailure: SessionState = {
    id: 'session-1', status: 'failed', title: 'production-web', profile: 'production',
    organization: 'org-1', asset: 'asset-1', account: 'account-1', error: 'connection refused',
  }
  const backend = makeBackend({
    onSessionState: vi.fn((handler) => { stateHandler = handler; return () => undefined }),
    startSSHSession: vi.fn(async () => {
      stateHandler(earlyFailure)
      return { ...earlyFailure, status: 'connecting' as const, error: '' }
    }),
  })
  const user = userEvent.setup()
  render(<App backend={backend} />)
  await screen.findByRole('heading', { name: '资产' })

  await user.click(await screen.findByRole('button', { name: '使用 production-web 连接' }))

  await waitFor(() => expect(terminalWrites.join('')).toContain('Connection closed.\r\n\r\nPress Enter to reconnect ...'))
  expect(screen.getByText('failed')).toBeInTheDocument()
})

test('远端断开与关闭确认并发时仍能关闭 SSH Tab', async () => {
  let stateHandler: (event: SessionState) => void = () => undefined
  const closeSSHSession = vi.fn()
    .mockResolvedValueOnce(undefined)
    .mockRejectedValue(new Error('session not found'))
  const backend = makeBackend({
    closeSSHSession,
    onSessionState: vi.fn((handler) => { stateHandler = handler; return () => undefined }),
  })
  const user = userEvent.setup()
  render(<App backend={backend} />)
  await screen.findByRole('heading', { name: '资产' })

  await user.click(await screen.findByRole('button', { name: '使用 production-web 连接' }))
  act(() => stateHandler({
    id: 'session-1', status: 'active', title: 'production-web', profile: 'production',
    organization: 'org-1', asset: 'asset-1', account: 'account-1', error: '',
  }))
  await user.click(await screen.findByRole('button', { name: '关闭 production-web Tab' }))
  const dialog = await screen.findByRole('dialog', { name: '关闭 SSH Tab' })

  act(() => stateHandler({
    id: 'session-1', status: 'closed', title: 'production-web', profile: 'production',
    organization: 'org-1', asset: 'asset-1', account: 'account-1', error: '',
  }))
  await waitFor(() => expect(closeSSHSession).toHaveBeenCalledTimes(1))
  await user.click(within(dialog).getByRole('button', { name: '关闭 Tab' }))

  await waitFor(() => expect(screen.queryByRole('tab', { name: /production-web/ })).not.toBeInTheDocument())
  expect(closeSSHSession).toHaveBeenCalledTimes(1)
})

test('顶部上下文选择器和资产菜单点击外部后关闭', async () => {
  const backend = makeBackend()
  const user = userEvent.setup()
  render(<App backend={backend} />)
  const heading = await screen.findByRole('heading', { name: '资产' })

  expect(screen.queryByRole('combobox', { name: '当前 Organization' })).not.toBeInTheDocument()
  await user.click(await screen.findByRole('button', { name: '当前 Organization：研发中心' }))
  expect(screen.getByRole('listbox', { name: '当前 Organization' })).toBeInTheDocument()
  await user.click(heading)
  expect(screen.queryByRole('listbox', { name: '当前 Organization' })).not.toBeInTheDocument()

  await user.click(screen.getByRole('button', { name: '筛选' }))
  expect(screen.getByRole('group', { name: '当前页 Alias 筛选' })).toBeInTheDocument()
  await user.click(heading)
  expect(screen.queryByRole('group', { name: '当前页 Alias 筛选' })).not.toBeInTheDocument()

  await user.click(screen.getByLabelText('prod-web-01 更多操作'))
  expect(screen.getByRole('menuitem', { name: '从操作菜单连接 prod-web-01' })).toBeInTheDocument()
  await user.click(heading)
  expect(screen.queryByRole('menuitem', { name: '从操作菜单连接 prod-web-01' })).not.toBeInTheDocument()
})

test('Profile 和 Organization 上下文只在资产界面显示', async () => {
  const user = userEvent.setup()
  render(<App backend={makeBackend()} />)

  await screen.findByRole('heading', { name: '资产' })
  expect(screen.getByRole('button', { name: '当前 Profile：production' })).toBeInTheDocument()
  expect(await screen.findByRole('button', { name: '当前 Organization：研发中心' })).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: /快速连接/ })).not.toBeInTheDocument()

  await user.click(screen.getByRole('button', { name: /^打开 Profile/ }))
  expect(screen.queryByRole('button', { name: '当前 Profile：production' })).not.toBeInTheDocument()
  expect(screen.queryByRole('button', { name: '当前 Organization：研发中心' })).not.toBeInTheDocument()

  await user.click(screen.getByRole('button', { name: '打开设置' }))
  expect(screen.queryByRole('button', { name: '当前 Profile：production' })).not.toBeInTheDocument()
  expect(screen.queryByRole('button', { name: '当前 Organization：研发中心' })).not.toBeInTheDocument()
})

test('Tab 栏新建连接和全局快捷键直接打开快速连接', async () => {
  const backend = makeBackend()
  const user = userEvent.setup()
  render(<App backend={backend} />)

  await screen.findByRole('heading', { name: '资产' })
  await user.click(screen.getByRole('button', { name: '新建连接' }))
  expect(await screen.findByRole('dialog', { name: '快速连接' })).toBeInTheDocument()
  expect(screen.getByRole('heading', { name: '资产' })).toBeInTheDocument()

  await user.click(within(screen.getByRole('dialog', { name: '快速连接' })).getByRole('button', { name: '关闭' }))
  await user.click(screen.getByRole('button', { name: '打开设置' }))
  await user.keyboard('{Control>}k{/Control}')
  expect(await screen.findByRole('dialog', { name: '快速连接' })).toBeInTheDocument()
})

test('快速连接复用资产页已有数据并在本地筛选', async () => {
  const backend = makeBackend()
  const user = userEvent.setup()
  render(<App backend={backend} />)

  await screen.findByTestId('asset-row-asset-1')
  await user.click(screen.getByRole('button', { name: '新建连接' }))
  const dialog = await screen.findByRole('dialog', { name: '快速连接' })

  expect(await within(dialog).findByText('production-web')).toBeInTheDocument()
  expect(backend.quickSearch).not.toHaveBeenCalled()

  await user.type(within(dialog).getByPlaceholderText('名称、地址、Asset ID 或 Alias'), 'missing')
  expect(within(dialog).getByText('没有匹配结果')).toBeInTheDocument()
  await act(async () => { await new Promise((resolve) => window.setTimeout(resolve, 220)) })
  expect(backend.quickSearch).not.toHaveBeenCalled()
})

test('资产页没有数据时快速连接才请求 API', async () => {
  const emptyPage: AssetPage = { ...assetPage, count: 0, aliasCount: 0, results: [] }
  const backend = makeBackend({
    listAssets: vi.fn().mockResolvedValue(emptyPage),
    quickSearch: vi.fn().mockResolvedValue(assetPage.results),
  })
  const user = userEvent.setup()
  render(<App backend={backend} />)

  await waitFor(() => expect(backend.listAssets).toHaveBeenCalled())
  await user.click(screen.getByRole('button', { name: '新建连接' }))
  const dialog = await screen.findByRole('dialog', { name: '快速连接' })

  expect(await within(dialog).findByText('production-web')).toBeInTheDocument()
  expect(backend.quickSearch).toHaveBeenCalledWith({
    profile: 'production', organization: 'org-1', query: '', limit: 20,
  })
})

test('创建 Alias 后局部更新并保留已有账号显示缓存', async () => {
  const secondAsset = { id: 'asset-2', name: 'new-server', address: '10.0.0.2', type: 'Linux', category: 'Host', aliases: [] }
  const pageWithEmptyAsset: AssetPage = { ...assetPage, count: 2, results: [...assetPage.results, secondAsset] }
  const secondDetail: AssetDetail = { ...secondAsset, accounts: assetDetail.accounts, protocols: assetDetail.protocols }
  const createdAlias = { name: 'new-alias', asset: 'asset-2', account: 'account-2', organization: 'org-1' }
  const backend = makeBackend({
    listAssets: vi.fn().mockResolvedValue(pageWithEmptyAsset),
    getAsset: vi.fn(({ asset }) => Promise.resolve(asset === 'asset-2' ? secondDetail : assetDetail)),
    createAlias: vi.fn().mockResolvedValue(createdAlias),
  })
  const user = userEvent.setup()
  render(<App backend={backend} />)

  const firstAccount = await screen.findByLabelText('production-web 默认账号')
  await waitFor(() => expect(firstAccount).toHaveDisplayValue('deploy'))
  const callsBeforeCreate = vi.mocked(backend.listAssets).mock.calls.length
  await user.click(within(screen.getByTestId('asset-row-asset-2')).getByLabelText('创建 Alias'))
  const dialog = await screen.findByRole('dialog', { name: '创建 Alias' })
  await user.type(within(dialog).getByLabelText('Alias 名称'), 'new-alias')
  await waitFor(() => expect(within(dialog).getByLabelText('默认账号')).toHaveDisplayValue('连接时询问'))
  await user.selectOptions(within(dialog).getByLabelText('默认账号'), 'account-2')
  await user.click(within(dialog).getByRole('button', { name: '保存 Alias' }))

  expect(await screen.findByText('new-alias')).toBeInTheDocument()
  expect(screen.getByLabelText('production-web 默认账号')).toHaveDisplayValue('deploy')
  expect(backend.listAssets).toHaveBeenCalledTimes(callsBeforeCreate)
})

test('资产请求自动续期后同步 Profile 图标状态并仅在悬停提示显示到期时间', async () => {
  const expiredState: BootstrapState = {
    ...bootstrapState,
    profiles: bootstrapState.profiles.map((item) => ({
      ...item,
      auth: { ...item.auth, expired: true, expiresAt: new Date(Date.now() - 60_000).toISOString() },
    })),
  }
  const refreshedAuth = {
    loggedIn: true,
    expired: false,
    refreshAvailable: true,
    expiresAt: new Date(Date.now() + 60 * 60_000).toISOString(),
  }
  const backend = makeBackend({
    bootstrap: vi.fn().mockResolvedValue(expiredState),
    getAuthStatus: vi.fn().mockResolvedValue(refreshedAuth),
  })
  render(<App backend={backend} />)

  await screen.findByRole('heading', { name: '资产' })
  await waitFor(() => expect(backend.getAuthStatus).toHaveBeenCalledWith('production'))
  const profileButton = screen.getByRole('button', { name: '打开 Profile，认证状态：production，已认证' })
  expect(profileButton).toHaveClass('profile-auth-button')
  expect(profileButton.querySelectorAll('svg')).toHaveLength(1)
  expect(profileButton.querySelector('.profile-status-icon .auth-indicator')).toBeInTheDocument()
  expect(profileButton).toHaveAttribute('title', expect.stringMatching(/Profile · production · 已认证 · \d+ 分钟后到期/))
  expect(screen.queryByRole('button', { name: /^认证状态：/ })).not.toBeInTheDocument()
  expect(within(screen.getByRole('navigation', { name: '顶部快捷操作' })).getAllByRole('button')).toHaveLength(3)
  expect(screen.queryByText(/分钟后到期/)).not.toBeInTheDocument()
})

test('SSH 标题栏显示 Alias、原始资产名、ID 和状态灯，并按 OSC 7 启用路径复制', async () => {
  let stateHandler: (event: SessionState) => void = () => undefined
  let latencyHandler: (event: SessionLatency) => void = () => undefined
  const activeSession: SessionState = {
    id: 'live-1', status: 'active', title: 'production-web', profile: 'production',
    organization: 'org-1', asset: 'asset-1', account: 'account-1', error: '',
  }
  const backend = makeBackend({
    startSSHSession: vi.fn().mockResolvedValue(activeSession),
    onSessionState: vi.fn((handler) => { stateHandler = handler; return () => undefined }),
    onSessionLatency: vi.fn((handler) => { latencyHandler = handler; return () => undefined }),
  })
  const user = userEvent.setup()
  const writeClipboard = vi.spyOn(navigator.clipboard, 'writeText')
  const readClipboard = vi.spyOn(navigator.clipboard, 'readText').mockResolvedValue('pwd')
  terminalState.pasted = []
  terminalState.selection = ''
  terminalState.selectionHandler = undefined
  terminalOscHandlers.clear()
  render(<App backend={backend} />)

  await screen.findByRole('heading', { name: '资产' })
  await user.click(await screen.findByRole('button', { name: '使用 production-web 连接' }))
  const disconnect = await screen.findByRole('button', { name: '断开 production-web SSH 连接' })
  const toolbar = disconnect.closest<HTMLElement>('.terminal-toolbar')!
  expect(Array.from(toolbar.querySelectorAll('.terminal-toolbar-name, .terminal-toolbar-meta')).map((item) => item.textContent)).toEqual([
    'production-web', 'prod-web-01', 'asset-1',
  ])
  expect(within(toolbar).getByRole('img', { name: '连接状态：已连接' })).toHaveClass('latency-pending')
  expect(within(toolbar).getByText('— ms')).toBeInTheDocument()
  expect(toolbar).not.toHaveTextContent('active')

  for (const [milliseconds, indicatorClass] of [[99, 'latency-good'], [100, 'latency-warning'], [200, 'latency-warning'], [201, 'latency-slow']] as const) {
    act(() => latencyHandler({ id: 'live-1', milliseconds, available: true }))
    expect(within(toolbar).getByRole('img', { name: '连接状态：已连接' })).toHaveClass(indicatorClass)
    expect(within(toolbar).getByText(`${milliseconds} ms`)).toBeInTheDocument()
  }

  const actions = toolbar.querySelector<HTMLElement>('.terminal-toolbar-actions')!
  const copySelection = within(actions).getByRole('button', { name: '复制选中文本' })
  const pasteClipboard = within(actions).getByRole('button', { name: '粘贴剪贴板文本' })
  const copyDirectory = within(actions).getByRole('button', { name: '复制当前工作目录' })
  expect(within(actions).getAllByRole('button')).toEqual([copySelection, pasteClipboard, copyDirectory, disconnect])
  expect(copySelection).toHaveAttribute('title', '复制选中文本 (Ctrl + Insert)')
  expect(pasteClipboard).toHaveAttribute('title', '粘贴剪贴板文本 (Shift + Insert)')
  expect(copySelection).toBeDisabled()
  await waitFor(() => expect(pasteClipboard).toBeEnabled())
  expect(copyDirectory).toBeDisabled()
  expect(copyDirectory.nextElementSibling).toHaveClass('terminal-action-separator')
  expect(copyDirectory.nextElementSibling?.nextElementSibling).toBe(disconnect)

  terminalState.selection = 'selected output'
  act(() => terminalState.selectionHandler?.())
  expect(copySelection).toBeEnabled()
  await user.click(copySelection)
  expect(writeClipboard).toHaveBeenCalledWith('selected output')
  await user.click(pasteClipboard)
  expect(readClipboard).toHaveBeenCalled()
  expect(terminalState.pasted).toEqual(['pwd'])

  await act(async () => terminalOscHandlers.get(7)?.('file://prod-web-01/srv/releases/current%20build'))
  expect(copyDirectory).toBeEnabled()
  await user.click(copyDirectory)
  expect(writeClipboard).toHaveBeenCalledWith('/srv/releases/current build')

  act(() => stateHandler({ ...activeSession, status: 'closed' }))
  expect(copyDirectory).toBeDisabled()
  expect(pasteClipboard).toBeDisabled()
  expect(disconnect).toBeDisabled()
  expect(within(toolbar).getByRole('img', { name: '连接状态：未连接' })).toHaveClass('offline')
  expect(within(toolbar).queryByText('— ms')).not.toBeInTheDocument()
})

test('标题栏断开按钮只断开活动 Session 并保留 SSH Tab', async () => {
  const activeSession: SessionState = {
    id: 'live-1', status: 'active', title: 'production-web', profile: 'production',
    organization: 'org-1', asset: 'asset-1', account: 'account-1', error: '',
  }
  const backend = makeBackend({ startSSHSession: vi.fn().mockResolvedValue(activeSession) })
  const user = userEvent.setup()
  render(<App backend={backend} />)

  await screen.findByRole('heading', { name: '资产' })
  await user.click(await screen.findByRole('button', { name: '使用 production-web 连接' }))
  await user.click(await screen.findByRole('button', { name: '断开 production-web SSH 连接' }))

  await waitFor(() => expect(backend.closeSSHSession).toHaveBeenCalledWith('live-1'))
  expect(screen.getByRole('tab', { name: /production-web/ })).toBeInTheDocument()
  expect(screen.queryByRole('dialog', { name: '关闭 SSH Tab' })).not.toBeInTheDocument()
})

test('连接不可用时标题栏显示红灯并禁用断开和路径复制', async () => {
  const backend = makeBackend({
    bootstrap: vi.fn().mockResolvedValue({
      ...bootstrapState,
      workspace: {
        activeTabId: 'ssh-restored',
        tabs: [{
          id: 'ssh-restored', type: 'ssh', profile: 'production', organization: 'org-1',
          target: 'production-web', account: 'account-1', alias: 'production-web',
          assetId: 'asset-1', assetName: 'prod-web-01',
        }],
      },
    }),
  })
  render(<App backend={backend} />)

  const disconnect = await screen.findByRole('button', { name: '断开 production-web SSH 连接' })
  const toolbar = disconnect.closest<HTMLElement>('.terminal-toolbar')!
  const disconnectedIndicator = within(toolbar).getByRole('img', { name: '连接状态：未连接' })
  expect(disconnectedIndicator).toHaveClass('offline')
  expect(disconnectedIndicator.closest('.terminal-connection-metric')).toHaveClass('latency-hidden')
  expect(within(toolbar).queryByText(/ms$/)).not.toBeInTheDocument()
  expect(within(toolbar).getByRole('button', { name: '复制当前工作目录' })).toBeDisabled()
  expect(disconnect).toBeDisabled()
})

test('使用应用内确认框关闭活动 SSH Tab', async () => {
  const activeSession: SessionState = {
    id: 'live-1', status: 'active', title: 'production-web', profile: 'production',
    organization: 'org-1', asset: 'asset-1', account: 'account-1', error: '',
  }
  const backend = makeBackend({ startSSHSession: vi.fn().mockResolvedValue(activeSession) })
  const user = userEvent.setup()
  render(<App backend={backend} />)

  await screen.findByRole('heading', { name: '资产' })
  await user.click(await screen.findByRole('button', { name: '使用 production-web 连接' }))
  await user.click(await screen.findByRole('button', { name: '关闭 production-web Tab' }))
  expect(await screen.findByRole('dialog', { name: '关闭 SSH Tab' })).toBeInTheDocument()
  expect(backend.closeSSHSession).not.toHaveBeenCalled()

  await user.click(screen.getByRole('button', { name: '取消' }))
  expect(screen.queryByRole('dialog', { name: '关闭 SSH Tab' })).not.toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: '关闭 production-web Tab' }))
  await user.click(screen.getByRole('button', { name: '关闭 Tab' }))
  await waitFor(() => expect(backend.closeSSHSession).toHaveBeenCalledWith('live-1'))
})

test('浏览器登录弹窗说明支持原生回调和完整确认页 URL', async () => {
  const loggedOutState: BootstrapState = {
    ...bootstrapState,
    profiles: bootstrapState.profiles.map((item) => ({
      ...item,
      auth: { loggedIn: false, expired: false, refreshAvailable: false, expiresAt: '' },
    })),
  }
  const backend = makeBackend({
    bootstrap: vi.fn().mockResolvedValue(loggedOutState),
    getAuthStatus: vi.fn().mockResolvedValue(loggedOutState.profiles[0].auth),
  })
  const user = userEvent.setup()
  render(<App backend={backend} />)

  await screen.findByRole('heading', { name: 'Profile' })
  const profileButton = screen.getByRole('button', { name: '打开 Profile，认证状态：production，需要登录' })
  expect(profileButton).toHaveClass('profile-auth-button')
  expect(profileButton.querySelectorAll('svg')).toHaveLength(1)
  expect(profileButton.querySelector('.profile-status-icon .auth-indicator')).toHaveClass('offline')
  expect(screen.queryByRole('button', { name: /^认证状态：/ })).not.toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: '登录' }))

  const dialog = await screen.findByRole('dialog', { name: '完成浏览器登录' })
  expect(dialog).toHaveTextContent('jms:// 回调链接')
  expect(dialog).toHaveTextContent('浏览器地址栏中的完整确认页 URL')
  expect(within(dialog).getByLabelText('回调链接或确认页 URL')).toHaveAttribute(
    'placeholder',
    expect.stringContaining('https://'),
  )
})

test('GUI 认证错误引导到 Profile 页面而不是 CLI', async () => {
  const backend = makeBackend({
    listOrganizations: vi.fn().mockRejectedValue(new Error('login required for profile "production"; run jumpctl auth login')),
  })

  render(<App backend={backend} />)

  const alert = await screen.findByRole('alert')
  expect(alert).toHaveTextContent('当前 Profile 需要登录，请在 Profile 页面完成认证。')
  expect(alert).not.toHaveTextContent('jumpctl')
})

test('新建并登录 Profile 后保持原来的当前 Profile', async () => {
  const addedProfile = {
    name: 'staging',
    url: 'https://staging.example.test',
    organization: '',
    aliasCount: 0,
    auth: { loggedIn: true, expired: false, refreshAvailable: true, expiresAt: '' },
  }
  const addedState: BootstrapState = {
    ...bootstrapState,
    currentProfile: 'production',
    profiles: [...bootstrapState.profiles, addedProfile],
  }
  const backend = makeBackend({
    bootstrap: vi.fn()
      .mockResolvedValueOnce(bootstrapState)
      .mockResolvedValue(addedState),
    startLogin: vi.fn().mockResolvedValue({ id: 'login-staging', profile: 'staging', expiresAt: '2026-08-29T12:00:00Z' }),
    completeLogin: vi.fn().mockResolvedValue(addedProfile.auth),
  })
  const user = userEvent.setup()
  render(<App backend={backend} />)

  await screen.findByRole('heading', { name: '资产' })
  await user.click(screen.getByRole('button', { name: /^打开 Profile/ }))
  await user.click(screen.getByRole('button', { name: '添加 Profile' }))
  const addDialog = await screen.findByRole('dialog', { name: '添加 Profile' })
  await user.type(within(addDialog).getByLabelText('名称'), 'staging')
  await user.type(within(addDialog).getByLabelText('JumpServer URL'), 'https://staging.example.test')
  await user.click(within(addDialog).getByRole('button', { name: '添加并登录' }))

  await waitFor(() => expect(backend.addProfile).toHaveBeenCalledWith('staging', 'https://staging.example.test'))
  expect(backend.useProfile).not.toHaveBeenCalled()
  const loginDialog = await screen.findByRole('dialog', { name: '完成浏览器登录' })
  await user.type(within(loginDialog).getByLabelText('回调链接或确认页 URL'), 'jms://auth/callback?code=test&state=test')
  await user.click(within(loginDialog).getByRole('button', { name: '完成登录' }))

  const productionCard = (await screen.findByRole('heading', { name: 'production' })).closest('article')!
  const stagingCard = screen.getByRole('heading', { name: 'staging' }).closest('article')!
  expect(within(productionCard).getByText('当前')).toBeInTheDocument()
  expect(within(stagingCard).getByRole('button', { name: '设为当前' })).toBeInTheDocument()
})

test('重复的 Profile 名称错误显示在创建弹窗内', async () => {
  const backend = makeBackend({
    addProfile: vi.fn().mockRejectedValue(new Error('Profile production 已存在')),
  })
  const user = userEvent.setup()
  render(<App backend={backend} />)

  await screen.findByRole('heading', { name: '资产' })
  await user.click(screen.getByRole('button', { name: /^打开 Profile/ }))
  await user.click(screen.getByRole('button', { name: '添加 Profile' }))
  const dialog = await screen.findByRole('dialog', { name: '添加 Profile' })
  await user.type(within(dialog).getByLabelText('名称'), 'production')
  await user.type(within(dialog).getByLabelText('JumpServer URL'), 'https://duplicate.example.test')
  await user.click(within(dialog).getByRole('button', { name: '添加并登录' }))

  const alert = await within(dialog).findByRole('alert')
  expect(alert).toHaveTextContent('Profile production 已存在')
  expect(screen.getAllByRole('alert')).toEqual([alert])
  expect(within(dialog).getByLabelText('名称')).toHaveValue('production')
  expect(within(dialog).getByLabelText('JumpServer URL')).toHaveValue('https://duplicate.example.test')
  expect(backend.startLogin).not.toHaveBeenCalled()
})

test('编辑 Profile URL 后保留 Profile，并要求重新登录', async () => {
  const updatedProfile = {
    ...bootstrapState.profiles[0],
    url: 'https://new-jump.example.test',
    auth: { loggedIn: false, expired: false, refreshAvailable: false, expiresAt: '' },
  }
  const backend = makeBackend({
    bootstrap: vi.fn()
      .mockResolvedValueOnce(bootstrapState)
      .mockResolvedValue({ ...bootstrapState, profiles: [updatedProfile] }),
  })
  const user = userEvent.setup()
  render(<App backend={backend} />)

  await screen.findByRole('heading', { name: '资产' })
  await user.click(screen.getByRole('button', { name: /^打开 Profile/ }))
  const card = screen.getByRole('heading', { name: 'production' }).closest('article')!
  await user.click(within(card).getByRole('button', { name: '编辑 production Profile' }))

  const dialog = await screen.findByRole('dialog', { name: '编辑 Profile' })
  expect(within(dialog).getByLabelText('名称')).toBeDisabled()
  expect(within(dialog).getByLabelText('名称')).toHaveValue('production')
  expect(dialog).toHaveTextContent('Organization 和 Alias')
  expect(dialog).toHaveTextContent('需要重新登录')
  const url = within(dialog).getByLabelText('JumpServer URL')
  expect(url).toHaveValue('https://jump.example.test')
  await user.clear(url)
  await user.type(url, 'https://new-jump.example.test')
  await user.click(within(dialog).getByRole('button', { name: '保存' }))

  await waitFor(() => expect(backend.updateProfileURL).toHaveBeenCalledWith('production', 'https://new-jump.example.test'))
  expect(await screen.findByText('https://new-jump.example.test')).toBeInTheDocument()
  expect(within(card).getByText('需要登录')).toBeInTheDocument()
  expect(backend.startLogin).not.toHaveBeenCalled()
})

test('退出 Profile 登录前要求确认', async () => {
  const loggedOutProfile = {
    ...bootstrapState.profiles[0],
    auth: { loggedIn: false, expired: false, refreshAvailable: false, expiresAt: '' },
  }
  const backend = makeBackend({
    bootstrap: vi.fn()
      .mockResolvedValueOnce(bootstrapState)
      .mockResolvedValue({ ...bootstrapState, profiles: [loggedOutProfile] }),
  })
  const user = userEvent.setup()
  render(<App backend={backend} />)

  await screen.findByRole('heading', { name: '资产' })
  await user.click(screen.getByRole('button', { name: /^打开 Profile/ }))
  const card = screen.getByRole('heading', { name: 'production' }).closest('article')!
  await user.click(within(card).getByRole('button', { name: '退出' }))

  const dialog = await screen.findByRole('dialog', { name: '退出登录' })
  expect(dialog).toHaveTextContent('OAuth 登录状态')
  expect(dialog).toHaveTextContent('现有 SSH 会话不会断开')
  expect(backend.logout).not.toHaveBeenCalled()
  await user.click(within(dialog).getByRole('button', { name: '取消' }))
  expect(screen.queryByRole('dialog', { name: '退出登录' })).not.toBeInTheDocument()
  expect(backend.logout).not.toHaveBeenCalled()

  await user.click(within(card).getByRole('button', { name: '退出' }))
  await user.click(within(await screen.findByRole('dialog', { name: '退出登录' })).getByRole('button', { name: '确认退出 production' }))
  await waitFor(() => expect(backend.logout).toHaveBeenCalledWith('production'))
  expect(screen.queryByRole('dialog', { name: '退出登录' })).not.toBeInTheDocument()
  expect(within(card).getByText('需要登录')).toBeInTheDocument()
})

test('Profile 卡片展示 Server URL，并在警告确认后删除全部本地内容', async () => {
  const testProfile = {
    name: 'temporary',
    url: 'https://temporary.example.test',
    organization: 'org-test',
    aliasCount: 3,
    auth: { loggedIn: true, expired: false, refreshAvailable: true, expiresAt: '' },
  }
  const initialState: BootstrapState = {
    ...bootstrapState,
    profiles: [...bootstrapState.profiles, testProfile],
    workspace: {
      activeTabId: 'system:assets',
      tabs: [
        { id: 'system:assets', type: 'assets' },
        { id: 'ssh-temporary', type: 'ssh', profile: 'temporary', organization: 'org-test', target: 'asset-test', account: 'root', assetId: 'asset-test', assetName: 'temporary-shell' },
      ],
    },
  }
  const deletedState: BootstrapState = {
    ...bootstrapState,
    profiles: bootstrapState.profiles,
  }
  const backend = makeBackend({
    bootstrap: vi.fn()
      .mockResolvedValueOnce(initialState)
      .mockResolvedValue(deletedState),
  })
  const user = userEvent.setup()
  const writeClipboard = vi.spyOn(navigator.clipboard, 'writeText')
  render(<App backend={backend} />)

  await screen.findByRole('heading', { name: '资产' })
  await user.click(screen.getByRole('button', { name: /^打开 Profile/ }))
  const card = screen.getByRole('heading', { name: 'temporary' }).closest('article')!
  expect(within(card).getByText('Server URL')).toBeInTheDocument()
  expect(within(card).getByText('https://temporary.example.test')).toBeInTheDocument()
  expect(within(card).queryByText('Alias')).not.toBeInTheDocument()
  await user.click(within(card).getByRole('button', { name: '复制 temporary Server URL' }))
  expect(writeClipboard).toHaveBeenCalledWith('https://temporary.example.test')

  await user.click(within(card).getByRole('button', { name: '删除 temporary Profile' }))
  const dialog = await screen.findByRole('dialog', { name: '删除 Profile' })
  expect(dialog).toHaveTextContent('Organization、全部 Alias 和本地 OAuth 凭据')
  expect(dialog).toHaveTextContent('活动 SSH 会话')
  expect(backend.deleteProfile).not.toHaveBeenCalled()

  await user.click(within(dialog).getByRole('button', { name: '删除 temporary Profile' }))
  await waitFor(() => expect(backend.deleteProfile).toHaveBeenCalledWith('temporary'))
  expect(screen.queryByRole('heading', { name: 'temporary' })).not.toBeInTheDocument()
  expect(screen.queryByRole('tab', { name: /temporary-shell/ })).not.toBeInTheDocument()
})

test('删除 Profile 时由后端统一关闭活动 Session，前端不重复关闭', async () => {
  const deletedState: BootstrapState = { ...bootstrapState, profiles: [] }
  const closeSSHSession = vi.fn().mockRejectedValue(new Error('session already closed'))
  const backend = makeBackend({
    bootstrap: vi.fn()
      .mockResolvedValueOnce(bootstrapState)
      .mockResolvedValue(deletedState),
    closeSSHSession,
  })
  const user = userEvent.setup()
  render(<App backend={backend} />)
  await screen.findByRole('heading', { name: '资产' })

  await user.click(await screen.findByRole('button', { name: '使用 production-web 连接' }))
  await screen.findByRole('tab', { name: /production-web/ })
  await user.click(screen.getByRole('button', { name: /^打开 Profile/ }))
  const card = screen.getByRole('heading', { name: 'production' }).closest('article')!
  await user.click(within(card).getByRole('button', { name: '删除 production Profile' }))
  await user.click(within(await screen.findByRole('dialog', { name: '删除 Profile' })).getByRole('button', { name: '删除 production Profile' }))

  await waitFor(() => expect(backend.deleteProfile).toHaveBeenCalledWith('production'))
  await waitFor(() => expect(screen.queryByRole('tab', { name: /production-web/ })).not.toBeInTheDocument())
  expect(closeSSHSession).not.toHaveBeenCalled()
})
