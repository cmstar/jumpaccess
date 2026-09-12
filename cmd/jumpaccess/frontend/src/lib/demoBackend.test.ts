import { afterEach, expect, test, vi } from 'vitest'
import { createPreviewBackend } from './previewBackend'

afterEach(() => { vi.clearAllTimers(); vi.useRealTimers() })

test('已配置演示环境延续教程的 office Profile 和站点地址', async () => {
  const state = await createPreviewBackend({ scenario: 'ready' }).bootstrap()
  expect(state.currentProfile).toBe('office')
  expect(state.profiles.map(profile => profile.name)).toEqual(['office', 'staging'])
  expect(state.profiles[0]).toMatchObject({ url: 'https://jump.example.com', auth: { loggedIn: true } })
})

test('独立演示实例隔离 Profile、Alias 和偏好修改', async () => {
  const first = createPreviewBackend()
  const second = createPreviewBackend()
  await first.deleteProfile('staging')
  await first.deleteAlias('office', 'web')
  const preferences = (await first.bootstrap()).preferences
  await first.savePreferences({ ...preferences, theme: 'dark' })
  expect((await second.bootstrap()).preferences.theme).toBe('light')
  expect((await second.bootstrap()).profiles).toHaveLength(2)
  const request = { profile: 'office', organization: 'org-dev', asset: '7f3c91bd' }
  expect((await first.getAsset(request)).aliases.some(alias => alias.name === 'web')).toBe(false)
  expect((await second.getAsset(request)).aliases.some(alias => alias.name === 'web')).toBe(true)
})

test('演示登录完成后持久到实例内，取消的尝试不能完成', async () => {
  const backend = createPreviewBackend()
  const attempt = await backend.startLogin('staging')
  await backend.completeLogin(attempt.id, 'demo')
  expect((await backend.getAuthStatus('staging')).loggedIn).toBe(true)
  const cancelled = await backend.startLogin('staging')
  await backend.cancelLogin(cancelled.id)
  await expect(backend.completeLogin(cancelled.id, 'demo')).rejects.toThrow()
})

test('演示提供空白和登录过期场景', async () => {
  expect((await createPreviewBackend({ scenario: 'empty' }).bootstrap()).profiles).toEqual([])
  expect(await createPreviewBackend({ scenario: 'expired' }).getAuthStatus('office')).toMatchObject({ expired: true, refreshAvailable: false })
})

test('模拟终端执行示例命令，关闭连接后不会重新激活', async () => {
  vi.useFakeTimers()
  const backend = createPreviewBackend()
  const output: string[] = []
  backend.onSessionOutput(event => output.push(event.data))
  const request = { profile: 'office', organization: 'org-dev', target: '7f3c91bd', account: 'account-deploy', columns: 100, rows: 30 }
  const session = await backend.startSSHSession(request)
  await vi.advanceTimersByTimeAsync(600)
  await backend.writeSSHSession(session.id, 'pwd\r')
  await backend.writeSSHSession(session.id, 'ls\r')
  expect(output.join('')).toContain('/home/deploy')
  expect(output.join('')).toContain('app.yaml')
  const early = await backend.startSSHSession(request)
  await backend.closeSSHSession(early.id)
  await vi.advanceTimersByTimeAsync(600)
  const states = backend.listSSHSessions()
  await vi.advanceTimersByTimeAsync(100)
  expect((await states).some(item => item.id === early.id)).toBe(false)
  await expect(backend.writeSSHSession(early.id, 'ls\r')).rejects.toThrow()
})
