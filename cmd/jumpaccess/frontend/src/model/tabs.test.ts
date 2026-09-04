import { describe, expect, it } from 'vitest'

import { emptyTabWorkspace, reduceTabs } from './tabs'

const descriptor = {
  profile: 'production',
  organization: 'org-1',
  target: 'production-web',
  alias: 'production-web',
  assetID: 'asset-1',
  assetName: 'prod-web-01',
  account: 'account-1',
}

describe('reduceTabs', () => {
  it('opens each singleton tab once and focuses the existing tab when reopened', () => {
    const opened = reduceTabs(emptyTabWorkspace, { type: 'open-singleton', kind: 'profiles' })
    const reopened = reduceTabs(opened, { type: 'open-singleton', kind: 'profiles' })

    expect(reopened).toEqual({
      tabs: [{ id: 'system:profiles', kind: 'profiles' }],
      activeTabID: 'system:profiles',
    })
  })

  it('allows multiple SSH tabs for the same connection descriptor', () => {
    const first = reduceTabs(emptyTabWorkspace, { type: 'open-ssh', id: 'ssh-1', descriptor })
    const second = reduceTabs(first, { type: 'open-ssh', id: 'ssh-2', descriptor })

    expect(second.tabs.map((tab) => tab.id)).toEqual(['ssh-1', 'ssh-2'])
    expect(second.activeTabID).toBe('ssh-2')
  })

  it('leaves an empty workspace after closing the last tab', () => {
    const opened = reduceTabs(emptyTabWorkspace, { type: 'open-singleton', kind: 'assets' })

    const closed = reduceTabs(opened, { type: 'close', id: 'system:assets' })

    expect(closed).toEqual({ tabs: [], activeTabID: '' })
  })

  it('selects the right neighbour when closing the active tab and falls back to the left', () => {
    const assets = reduceTabs(emptyTabWorkspace, { type: 'open-singleton', kind: 'assets' })
    const profiles = reduceTabs(assets, { type: 'open-singleton', kind: 'profiles' })
    const settings = reduceTabs(profiles, { type: 'open-singleton', kind: 'settings' })
    const middleActive = reduceTabs(settings, { type: 'open-singleton', kind: 'profiles' })

    const middleClosed = reduceTabs(middleActive, { type: 'close', id: 'system:profiles' })
    expect(middleClosed.activeTabID).toBe('system:settings')

    const rightClosed = reduceTabs(middleClosed, { type: 'close', id: 'system:settings' })
    expect(rightClosed.activeTabID).toBe('system:assets')
  })

  it('moves a tab to the requested index without changing the active tab', () => {
    const assets = reduceTabs(emptyTabWorkspace, { type: 'open-singleton', kind: 'assets' })
    const profiles = reduceTabs(assets, { type: 'open-singleton', kind: 'profiles' })
    const settings = reduceTabs(profiles, { type: 'open-singleton', kind: 'settings' })

    const moved = reduceTabs(settings, {
      type: 'move', id: 'system:assets', toIndex: 2,
    })

    expect(moved.tabs.map((tab) => tab.id)).toEqual([
      'system:profiles', 'system:settings', 'system:assets',
    ])
    expect(moved.activeTabID).toBe('system:settings')
  })

  it('hydrates singleton tabs once and restores SSH tabs disconnected without live session fields', () => {
    const hydrated = reduceTabs(emptyTabWorkspace, {
      type: 'hydrate',
      workspace: {
        tabs: [
          { id: 'system:profiles', kind: 'profiles' },
          { id: 'system:profiles', kind: 'profiles' },
          {
            id: 'ssh-1', kind: 'ssh', descriptor,
            connectionStatus: 'active', sessionID: 'live-1', error: 'stale error',
          },
        ],
        activeTabID: 'ssh-1',
      },
    })

    expect(hydrated).toEqual({
      tabs: [
        { id: 'system:profiles', kind: 'profiles' },
        { id: 'ssh-1', kind: 'ssh', descriptor, connectionStatus: 'disconnected' },
      ],
      activeTabID: 'ssh-1',
    })
  })

  it('attaches a live session to an SSH tab and marks it connecting', () => {
    const opened = reduceTabs(emptyTabWorkspace, { type: 'open-ssh', id: 'ssh-1', descriptor })

    const attached = reduceTabs(opened, {
      type: 'attach-session', tabID: 'ssh-1', sessionID: 'live-1',
    })

    expect(attached.tabs[0]).toEqual({
      id: 'ssh-1', kind: 'ssh', descriptor,
      connectionStatus: 'connecting', sessionID: 'live-1',
    })
  })

  it('marks an SSH tab as connecting or reconnecting before a live session exists', () => {
    const opened = reduceTabs(emptyTabWorkspace, { type: 'open-ssh', id: 'ssh-1', descriptor })

    const connecting = reduceTabs(opened, {
      type: 'begin-connection', tabID: 'ssh-1', reconnecting: false,
    })
    const reconnecting = reduceTabs(connecting, {
      type: 'begin-connection', tabID: 'ssh-1', reconnecting: true,
    })

    expect(connecting.tabs[0]).toMatchObject({ connectionStatus: 'connecting' })
    expect(reconnecting.tabs[0]).toMatchObject({ connectionStatus: 'reconnecting' })
  })

  it('marks a connection attempt failed without requiring a live session ID', () => {
    const opened = reduceTabs(emptyTabWorkspace, { type: 'open-ssh', id: 'ssh-1', descriptor })
    const connecting = reduceTabs(opened, {
      type: 'begin-connection', tabID: 'ssh-1', reconnecting: false,
    })

    const failed = reduceTabs(connecting, {
      type: 'connection-error', tabID: 'ssh-1', error: 'connection refused',
    })

    expect(failed.tabs[0]).toMatchObject({ connectionStatus: 'failed', error: 'connection refused' })
  })

  it('routes an active session state to the SSH tab attached to that session', () => {
    const opened = reduceTabs(emptyTabWorkspace, { type: 'open-ssh', id: 'ssh-1', descriptor })
    const attached = reduceTabs(opened, {
      type: 'attach-session', tabID: 'ssh-1', sessionID: 'live-1',
    })

    const active = reduceTabs(attached, {
      type: 'session-state', sessionID: 'live-1', status: 'active', error: '',
    })

    expect(active.tabs[0]).toMatchObject({
      id: 'ssh-1', connectionStatus: 'active', sessionID: 'live-1',
    })
  })

  it('keeps an SSH tab disconnected and removes its live session when the session closes', () => {
    const opened = reduceTabs(emptyTabWorkspace, { type: 'open-ssh', id: 'ssh-1', descriptor })
    const attached = reduceTabs(opened, {
      type: 'attach-session', tabID: 'ssh-1', sessionID: 'live-1',
    })

    const closed = reduceTabs(attached, {
      type: 'session-state', sessionID: 'live-1', status: 'closed', error: '',
    })

    expect(closed.tabs[0]).toEqual({
      id: 'ssh-1', kind: 'ssh', descriptor, connectionStatus: 'disconnected',
    })
  })

  it('keeps a failed SSH tab retryable with its error and without a live session', () => {
    const opened = reduceTabs(emptyTabWorkspace, { type: 'open-ssh', id: 'ssh-1', descriptor })
    const attached = reduceTabs(opened, {
      type: 'attach-session', tabID: 'ssh-1', sessionID: 'live-1',
    })

    const failed = reduceTabs(attached, {
      type: 'session-state', sessionID: 'live-1', status: 'failed', error: 'connection refused',
    })

    expect(failed.tabs[0]).toEqual({
      id: 'ssh-1', kind: 'ssh', descriptor,
      connectionStatus: 'failed', error: 'connection refused',
    })
  })

  it('drops only SSH tabs owned by a deleted profile and selects the nearest surviving tab', () => {
    const stagingDescriptor = { ...descriptor, profile: 'staging', target: 'staging-web' }
    const assets = reduceTabs(emptyTabWorkspace, { type: 'open-singleton', kind: 'assets' })
    const productionOne = reduceTabs(assets, {
      type: 'open-ssh', id: 'ssh-production-1', descriptor,
    })
    const staging = reduceTabs(productionOne, {
      type: 'open-ssh', id: 'ssh-staging', descriptor: stagingDescriptor,
    })
    const productionTwo = reduceTabs(staging, {
      type: 'open-ssh', id: 'ssh-production-2', descriptor,
    })

    const dropped = reduceTabs(productionTwo, {
      type: 'drop-profile', profile: 'production',
    })

    expect(dropped.tabs.map((tab) => tab.id)).toEqual(['system:assets', 'ssh-staging'])
    expect(dropped.activeTabID).toBe('ssh-staging')
  })

  it('updates matching SSH descriptors when an Alias is renamed', () => {
    const production = reduceTabs(emptyTabWorkspace, { type: 'open-ssh', id: 'ssh-production', descriptor })
    const staging = reduceTabs(production, {
      type: 'open-ssh',
      id: 'ssh-staging',
      descriptor: { ...descriptor, profile: 'staging' },
    })

    const renamed = reduceTabs(staging, {
      type: 'rename-alias', profile: 'production', currentName: 'production-web', newName: 'primary-web',
    })

    expect(renamed.tabs[0]).toMatchObject({
      descriptor: { alias: 'primary-web', target: 'primary-web' },
    })
    expect(renamed.tabs[1]).toMatchObject({
      descriptor: { alias: 'production-web', target: 'production-web' },
    })
  })

  it('activates an existing tab without changing tab order', () => {
    const assets = reduceTabs(emptyTabWorkspace, { type: 'open-singleton', kind: 'assets' })
    const ssh = reduceTabs(assets, { type: 'open-ssh', id: 'ssh-1', descriptor })

    const activated = reduceTabs(ssh, { type: 'activate', id: 'system:assets' })

    expect(activated.tabs.map((tab) => tab.id)).toEqual(['system:assets', 'ssh-1'])
    expect(activated.activeTabID).toBe('system:assets')
  })
})

it('SFTP 可以为同一目标打开多个独立 Tab 并在恢复时保持断连', () => {
  const first = reduceTabs(emptyTabWorkspace, { type: 'open-sftp', id: 'sftp-1', descriptor } as any)
  const second = reduceTabs(first, { type: 'open-sftp', id: 'sftp-2', descriptor } as any)
  expect(second.tabs.map((tab) => [tab.id, tab.kind])).toEqual([['sftp-1', 'sftp'], ['sftp-2', 'sftp']])
  const attached = reduceTabs(second, { type: 'attach-session', tabID: 'sftp-2', sessionID: 'live-sftp' })
  const active = reduceTabs(attached, { type: 'session-state', sessionID: 'live-sftp', status: 'active', error: '' })
  expect(active.tabs[1]).toMatchObject({ connectionStatus: 'active', sessionID: 'live-sftp' })
  const restored = reduceTabs(emptyTabWorkspace, { type: 'hydrate', workspace: active })
  expect(restored.tabs[1]).toEqual({ id: 'sftp-2', kind: 'sftp', descriptor, connectionStatus: 'disconnected' })
  expect(restored.activeTabID).toBe('sftp-2')
})

it('重命名 Alias 保留 SFTP 的固定资产目标，删除 Profile 清理其 SFTP Tab', () => {
  const opened = reduceTabs(emptyTabWorkspace, { type: 'open-sftp', id: 'sftp-1', descriptor: { ...descriptor, target: 'asset-1' } })
  const other = reduceTabs(opened, { type: 'open-sftp', id: 'sftp-2', descriptor: { ...descriptor, profile: 'staging' } })
  const renamed = reduceTabs(other, { type: 'rename-alias', profile: 'production', currentName: 'production-web', newName: 'prod-web' })
  expect(renamed.tabs[0]).toMatchObject({ descriptor: { alias: 'prod-web', target: 'asset-1' } })
  const removed = reduceTabs(renamed, { type: 'drop-profile', profile: 'production' })
  expect(removed.tabs.map((tab) => tab.id)).toEqual(['sftp-2'])
})
