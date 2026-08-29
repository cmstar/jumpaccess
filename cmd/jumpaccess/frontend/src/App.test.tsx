import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, test, vi } from 'vitest'

import App from './App'
import type { AssetDetail, AssetPage, Backend, BootstrapState, HostKeyPrompt, SessionOutput, SessionState } from './lib/backend'

const { terminalWrites } = vi.hoisted(() => ({ terminalWrites: [] as string[] }))

vi.mock('@xterm/xterm', () => ({
  Terminal: class {
    cols = 120
    rows = 34
    loadAddon() {}
    open() {}
    write(value: string) { terminalWrites.push(value) }
    reset() {}
    focus() {}
    dispose() {}
    onData() { return { dispose() {} } }
    onResize() { return { dispose() {} } }
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
    version: 1,
    theme: 'light',
    terminalFontFamily: 'JetBrains Mono',
    terminalFontSize: 13,
    confirmCloseActiveSession: true,
  },
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
    deleteProfile: vi.fn().mockResolvedValue(undefined),
    useProfile: vi.fn().mockResolvedValue(undefined),
    setOrganization: vi.fn().mockResolvedValue(undefined),
    createAlias: vi.fn().mockResolvedValue(assetPage.results[0].aliases[0]),
    deleteAlias: vi.fn().mockResolvedValue(undefined),
    setAliasAccount: vi.fn().mockResolvedValue(undefined),
    savePreferences: vi.fn().mockResolvedValue(undefined),
    getAuthStatus: vi.fn().mockResolvedValue(bootstrapState.profiles[0].auth),
    refreshAuth: vi.fn().mockResolvedValue(bootstrapState.profiles[0].auth),
    startLogin: vi.fn().mockResolvedValue({ id: 'login-1', profile: 'production', expiresAt: '2026-08-29T12:00:00Z' }),
    completeLogin: vi.fn().mockResolvedValue(bootstrapState.profiles[0].auth),
    cancelLogin: vi.fn().mockResolvedValue(undefined),
    logout: vi.fn().mockResolvedValue(undefined),
    licenseText: vi.fn().mockResolvedValue('MIT License'),
    openConfig: vi.fn().mockResolvedValue(undefined),
    startSSHSession: vi.fn().mockResolvedValue(session),
    listSSHSessions: vi.fn().mockResolvedValue([]),
    writeSSHSession: vi.fn().mockResolvedValue(undefined),
    resizeSSHSession: vi.fn().mockResolvedValue(undefined),
    closeSSHSession: vi.fn().mockResolvedValue(undefined),
    resolveSSHHostKey: vi.fn().mockResolvedValue(undefined),
    onSessionState: vi.fn().mockReturnValue(() => undefined),
    onSessionOutput: vi.fn().mockReturnValue(() => undefined),
    onHostKeyPrompt: vi.fn().mockReturnValue(() => undefined),
    ...overrides,
  }
}

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
  await user.click(screen.getByRole('button', { name: '设置' }))
  await user.click(screen.getByRole('button', { name: '深色' }))
  await waitFor(() => expect(backend.savePreferences).toHaveBeenCalledWith(expect.objectContaining({ theme: 'dark' })))
  expect(document.documentElement).toHaveClass('dark')

  await user.click(screen.getByRole('button', { name: '查看许可证' }))
  expect(await screen.findByRole('dialog', { name: '开源许可证' })).toHaveTextContent('MIT License')
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

  act(() => stateHandler({
    id: 'live-1', status: 'active', title: 'prod-web-01', profile: 'production',
    organization: 'org-1', asset: 'asset-1', account: 'account-1', error: '',
  }))
  await user.click(screen.getByRole('button', { name: /会话/ }))
  act(() => outputHandler({ id: 'live-1', data: 'hello from ssh\r\n' }))
  await waitFor(() => expect(terminalWrites).toContain('hello from ssh\r\n'))

  act(() => hostKeyHandler({ id: 'host-key-1', host: 'gateway.example.test:22', fingerprint: 'SHA256:abc123' }))
  expect(await screen.findByRole('dialog', { name: '确认新的 SSH Gateway' })).toHaveTextContent('SHA256:abc123')
  await user.click(screen.getByRole('button', { name: '拒绝' }))
  await waitFor(() => expect(backend.resolveSSHHostKey).toHaveBeenCalledWith('host-key-1', false))
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

test('资产请求自动续期后同步顶部认证状态', async () => {
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
  expect(screen.getByRole('button', { name: /已认证/ })).toHaveTextContent(/分钟后到期/)
})

test('使用应用内确认框断开活动 SSH 会话', async () => {
  const activeSession: SessionState = {
    id: 'live-1', status: 'active', title: 'production-web', profile: 'production',
    organization: 'org-1', asset: 'asset-1', account: 'account-1', error: '',
  }
  const backend = makeBackend({ listSSHSessions: vi.fn().mockResolvedValue([activeSession]) })
  const user = userEvent.setup()
  render(<App backend={backend} />)

  await screen.findByRole('heading', { name: '资产' })
  expect(screen.getByRole('button', { name: '资产' })).toHaveAttribute('title', '资产')
  await user.click(screen.getByRole('button', { name: '会话' }))
  await user.click(screen.getByRole('button', { name: '断开 production-web 会话' }))
  expect(await screen.findByRole('dialog', { name: '断开 SSH 会话' })).toBeInTheDocument()
  expect(backend.closeSSHSession).not.toHaveBeenCalled()

  await user.click(screen.getByRole('button', { name: '取消' }))
  expect(screen.queryByRole('dialog', { name: '断开 SSH 会话' })).not.toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: '断开 production-web 会话' }))
  await user.click(screen.getByRole('button', { name: '断开连接' }))
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

  await screen.findByRole('heading', { name: '资产' })
  await user.click(screen.getByRole('button', { name: 'Profile' }))
  await user.click(screen.getByRole('button', { name: '登录' }))

  const dialog = await screen.findByRole('dialog', { name: '完成浏览器登录' })
  expect(dialog).toHaveTextContent('jms:// 回调链接')
  expect(dialog).toHaveTextContent('浏览器地址栏中的完整确认页 URL')
  expect(within(dialog).getByLabelText('回调链接或确认页 URL')).toHaveAttribute(
    'placeholder',
    expect.stringContaining('https://'),
  )
})

test('Profile 卡片展示 Server URL，并在警告确认后删除全部本地内容', async () => {
  const temporarySession: SessionState = {
    id: 'temporary-session', status: 'active', title: 'temporary-shell', profile: 'temporary',
    organization: 'org-test', asset: 'asset-test', account: 'root', error: '',
  }
  let emitSessionState: ((event: SessionState) => void) | undefined
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
  }
  const deletedState: BootstrapState = {
    ...bootstrapState,
    profiles: bootstrapState.profiles,
  }
  const backend = makeBackend({
    bootstrap: vi.fn()
      .mockResolvedValueOnce(initialState)
      .mockResolvedValue(deletedState),
    listSSHSessions: vi.fn().mockResolvedValue([temporarySession]),
    onSessionState: vi.fn((handler) => {
      emitSessionState = handler
      return () => undefined
    }),
  })
  const user = userEvent.setup()
  render(<App backend={backend} />)

  await screen.findByRole('heading', { name: '资产' })
  await user.click(screen.getByRole('button', { name: 'Profile' }))
  const card = screen.getByRole('heading', { name: 'temporary' }).closest('article')!
  expect(within(card).getByText('Server URL')).toBeInTheDocument()
  expect(within(card).getByText('https://temporary.example.test')).toBeInTheDocument()
  expect(within(card).queryByText('Alias')).not.toBeInTheDocument()

  await user.click(within(card).getByRole('button', { name: '删除 temporary Profile' }))
  const dialog = await screen.findByRole('dialog', { name: '删除 Profile' })
  expect(dialog).toHaveTextContent('Organization、全部 Alias 和本地 OAuth 凭据')
  expect(dialog).toHaveTextContent('活动 SSH 会话')
  expect(backend.deleteProfile).not.toHaveBeenCalled()

  await user.click(within(dialog).getByRole('button', { name: '删除 temporary Profile' }))
  await waitFor(() => expect(backend.deleteProfile).toHaveBeenCalledWith('temporary'))
  expect(screen.queryByRole('heading', { name: 'temporary' })).not.toBeInTheDocument()

  act(() => emitSessionState?.({ ...temporarySession, status: 'closed' }))
  await user.click(screen.getByRole('button', { name: '会话' }))
  expect(screen.queryByRole('button', { name: /temporary-shell/ })).not.toBeInTheDocument()
})
