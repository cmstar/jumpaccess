import { afterEach, expect, test, vi } from 'vitest'
import { createMCPPreviewBackend } from './mcpPreviewBackend'
import { createPreviewBackend } from './previewBackend'

afterEach(() => { vi.clearAllTimers(); vi.useRealTimers() })

test('MCP 首次配置场景打开独立模块，使用已登录 Profile 但没有保存 Token', async () => {
  const backend = createPreviewBackend({ scenario: 'mcp-setup' })
  const state = await backend.bootstrap()
  expect(state.workspace.activeTabId).toBe('system:mcp')
  expect(state.profiles[0].auth.loggedIn).toBe(true)
  expect(await backend.mcp!.status()).toMatchObject({ state: 'stopped', tokenConfigured: false })
  await expect(backend.mcp!.start()).rejects.toThrow('请先保存 Bearer Token')
})

test('演示启动后提供与资产样例一致的三类连接，统计与任务保持一致', async () => {
  const backend = createMCPPreviewBackend()
  const status = await backend.start()
  const connections = await backend.connections()
  expect(connections.map(c => c.target.alias)).toEqual(['web', 'builder', 'prod-db'])
  expect(connections[0]).toMatchObject({ status: 'ready', transport_connected: true })
  expect(connections[0].active_execution_id).toBeUndefined()
  expect(connections[1].active_execution_id).toBeTruthy()
  expect(connections[2]).toMatchObject({ status: 'disconnected', transport_connected: false })
  expect(status.counts).toMatchObject({ connections: 3, ssh_connections: 2, executions: 1 })
  expect(status.instance?.config.token).toBeUndefined()
  const startedAgain = await backend.start()
  expect(startedAgain.instance?.id).toBe(status.instance?.id)
  expect(await backend.connections()).toHaveLength(3)
})

test('日志场景包含查询摘要和条件、命令受理及结束，分页保留关联 ID', async () => {
  const backend = createMCPPreviewBackend()
  await backend.start()
  const first = await backend.logs(0, 3)
  expect(first.hasMore).toBe(true)
  const rest = await backend.logs(first.nextCursor, 100)
  const lines = [...first.lines, ...rest.lines]
  expect(new Set(lines.map(line => line.sequence)).size).toBe(lines.length)
  expect(lines.some(line => line.detail === 2 && line.text.includes('list_assets') && line.text.includes('count=3'))).toBe(true)
  expect(lines.some(line => line.detail === 3 && line.text.includes('查询条件'))).toBe(true)
  const accepted = lines.find(line => line.event === 'execution_accepted')!
  expect(accepted).toBeDefined()
  expect(lines.find(line => line.event === 'command_input' && line.executionID === accepted.executionID)).toMatchObject({ detail: 3, connectionID: accepted.connectionID })
  expect(lines.find(line => line.event === 'execution_finished' && line.executionID === accepted.executionID)).toMatchObject({ severity: 'INFO', detail: 2 })
  expect(rest.hasMore).toBe(false)
})

test('演示活动连接须等待任务结束才能关闭，停止和重启会清理旧等待', async () => {
  vi.useFakeTimers()
  const backend = createMCPPreviewBackend()
  await backend.start()
  const active = (await backend.connections()).find(c => c.active_execution_id)!
  expect(active).toBeDefined()
  await expect(backend.closeConnection(active.connection_id, 'if_idle')).rejects.toThrow('活动任务')
  expect(await backend.closeConnection(active.connection_id, 'drain')).toMatchObject({ status: 'draining' })
  await vi.advanceTimersByTimeAsync(1000)
  expect(await backend.connections()).toHaveLength(2)
  expect((await backend.status()).counts).toMatchObject({ connections: 2, executions: 0 })
  await backend.restart()
  const next = (await backend.connections()).find(c => c.active_execution_id)!
  await backend.closeConnection(next.connection_id, 'drain')
  await backend.stop()
  const restarted = await backend.start()
  await vi.advanceTimersByTimeAsync(2000)
  expect((await backend.status()).instance?.id).toBe(restarted.instance?.id)
  expect(await backend.connections()).toHaveLength(3)
})

test('演示 Token 显式标为样例，配置修改只在重启时更新运行快照且实例互相隔离', async () => {
  const backend = createMCPPreviewBackend()
  const other = createMCPPreviewBackend()
  const token = await backend.generateToken()
  expect(token).toMatch(/^DEMO_ONLY_.*A$/)
  expect(token).toHaveLength(43)
  expect(await backend.generateToken()).not.toBe(token)
  const started = await backend.start()
  const saved = await backend.saveConfig({ ...started.config, port: 39300, token })
  expect(saved).toMatchObject({ restartRequired: true, instance: { url: 'http://127.0.0.1:39287/mcp' } })
  const restarted = await backend.restart()
  expect(restarted).toMatchObject({ restartRequired: false, instance: { url: 'http://127.0.0.1:39300/mcp' } })
  expect((await other.status()).config.port).toBe(39287)
})
