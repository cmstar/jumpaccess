import { act, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'
import { MCPWorkspace } from './MCPWorkspace'
import { NotificationProvider } from './Notifications'
import { createPreviewBackend } from '../lib/previewBackend'
import type { MCPBackend, MCPLogLine, MCPStatus } from '../lib/mcpTypes'

const config = { listenHost: '127.0.0.1', port: 39287, allowLAN: false, token: 'A'.repeat(43) }
const status: MCPStatus = { state: 'running', config, tokenConfigured: true, restartRequired: false, error: '', instance: { id: 'instance', host: 'gui', url: 'http://127.0.0.1:39287/mcp', startedAt: '2026-10-18T00:00:00Z', config }, counts: { connections: 1, ssh_connections: 0, connecting: 0, executions: 1, unknown_executions: 0, draining: false } }
const line = (sequence: number, text: string, detail = 1): MCPLogLine => ({ sequence, text, detail, severity: detail === 3 ? 'DEBUG' : 'INFO', time: '2026-10-18T00:00:00Z', instance: 'instance', event: 'tool_call' })
function api(lines: MCPLogLine[]): MCPBackend {
  return { status: vi.fn().mockResolvedValue(status), saveConfig: vi.fn(), start: vi.fn(), stop: vi.fn().mockResolvedValue({ ...status, state: 'stopped', instance: null }), restart: vi.fn(), generateToken: vi.fn(), chooseTLSFile: vi.fn(), connections: vi.fn().mockResolvedValue([{ connection_id: 'instance:c:1', target: { profile: 'work', organization: 'org', asset_id: 'asset', account_id: 'ops' }, status: 'disconnected', transport_connected: false, created_at: '2026-10-18T00:00:00Z', last_used_at: '2026-10-18T00:00:00Z', active_execution_id: 'instance:x:1' }]), closeConnection: vi.fn().mockResolvedValue({}), logs: vi.fn(async cursor => ({ lines: lines.filter(l => l.sequence > cursor), nextCursor: lines.at(-1)?.sequence || 0, hasMore: false, gap: false })) }
}
afterEach(() => { vi.useRealTimers() })

test('首次缺少配置时进入配置页，轮询不抢回页面，生成后必须保存才能启动', async () => {
  const { preferences } = await createPreviewBackend().bootstrap()
  const backend = api([])
  let current: MCPStatus = { ...status, state: 'stopped', instance: null, counts: undefined, config: { ...config, token: '' }, tokenConfigured: false }
  backend.status = vi.fn(async () => current)
  backend.generateToken = vi.fn().mockResolvedValue(config.token)
  backend.saveConfig = vi.fn(async value => { current = { ...current, config: value, tokenConfigured: true }; return current })
  vi.useFakeTimers()
  const view = (hidden: boolean) => <NotificationProvider><MCPWorkspace backend={backend} hidden={hidden} preferences={preferences} onTerminalStyle={() => {}} /></NotificationProvider>
  const { rerender } = render(view(true))
  await act(async () => {})
  rerender(view(false))
  expect(screen.getByRole('tab', { name: '配置' })).toHaveAttribute('aria-selected', 'true')
  expect(screen.getByRole('form', { name: 'MCP 配置' })).toBeVisible()
  expect(screen.getByRole('status')).toHaveTextContent('尚未完成 MCP 配置')
  expect(screen.getByRole('button', { name: '启动' })).toBeDisabled()
  expect(screen.getByRole('switch', { name: '随应用启动 MCP' })).toBeDisabled()
  fireEvent.click(screen.getByRole('tab', { name: '日志' }))
  await act(async () => { await vi.advanceTimersByTimeAsync(3000) })
  expect(screen.getByRole('tab', { name: '日志' })).toHaveAttribute('aria-selected', 'true')
  fireEvent.click(screen.getByRole('tab', { name: '配置' }))
  fireEvent.click(screen.getByRole('button', { name: '生成' }))
  await act(async () => {})
  expect(screen.getByRole('button', { name: '启动' })).toBeDisabled()
  fireEvent.click(screen.getByRole('button', { name: '保存' }))
  await act(async () => {})
  expect(screen.queryByText(/尚未完成 MCP 配置/)).not.toBeInTheDocument()
  expect(screen.getByRole('button', { name: '启动' })).toBeEnabled()
  expect(screen.getByRole('switch', { name: '随应用启动 MCP' })).toBeEnabled()
  expect(backend.start).not.toHaveBeenCalled()
})

test('配置丢失后仍可关闭已启用的自动启动', async () => {
  const { preferences } = await createPreviewBackend().bootstrap()
  const backend = api([]), save = vi.fn()
  backend.status = vi.fn().mockResolvedValue({ ...status, state: 'stopped', instance: null, tokenConfigured: false, config: { ...config, token: '' } })
  render(<NotificationProvider><MCPWorkspace backend={backend} hidden={false} preferences={{ ...preferences, mcpAutoStart: true }} onSavePreferences={save} onTerminalStyle={() => {}} /></NotificationProvider>)
  const toggle = await screen.findByRole('switch', { name: '随应用启动 MCP' })
  expect(toggle).toBeEnabled()
  expect(toggle).toHaveAttribute('aria-checked', 'true')
  fireEvent.click(toggle)
  expect(save).toHaveBeenCalledWith(expect.objectContaining({ mcpAutoStart: false }))
})

test('日志级别只影响新增行，暂停继续采集，清空不触及业务记录，复用终端配色', async () => {
  const { preferences } = await createPreviewBackend().bootstrap()
  vi.useFakeTimers()
  const lines = [line(1, 'old detailed', 3)], backend = api(lines)
  render(<NotificationProvider><MCPWorkspace backend={backend} hidden={false} preferences={preferences} onTerminalStyle={() => {}} /></NotificationProvider>)
  await act(async () => {})
  fireEvent.click(screen.getByRole('tab', { name: '日志' }))
  fireEvent.click(screen.getByRole('button', { name: '详细' }))
  lines.push(line(2, 'visible detailed', 3))
  await act(async () => { await vi.advanceTimersByTimeAsync(1000) })
  expect(screen.getByText('visible detailed')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '精简' }))
  expect(screen.getByText('visible detailed')).toBeInTheDocument()
  lines.push(line(3, 'hidden detailed', 3), line(4, 'next service'))
  await act(async () => { await vi.advanceTimersByTimeAsync(1000) })
  expect(screen.queryByText('hidden detailed')).not.toBeInTheDocument()
  expect(screen.getByText('next service')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '暂停显示' }))
  lines.push(line(5, 'pending service'))
  await act(async () => { await vi.advanceTimersByTimeAsync(1000) })
  expect(screen.queryByText('pending service')).not.toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '恢复显示' }))
  expect(screen.getByText('pending service')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '清空显示' }))
  fireEvent.click(screen.getByRole('button', { name: '详细' }))
  await act(async () => { await vi.advanceTimersByTimeAsync(1000) })
  expect(screen.queryByText('visible detailed')).not.toBeInTheDocument()
  expect(screen.queryByText('hidden detailed')).not.toBeInTheDocument()
  expect(backend.stop).not.toHaveBeenCalled()
  expect(screen.getByLabelText('MCP 日志终端').style.getPropertyValue('--mcp-terminal-bg')).toBe('#2e3440')
})

test('标准级别显示查询完成摘要，详细级别仅增加后续查询条件', async () => {
  const { preferences } = await createPreviewBackend().bootstrap()
  vi.useFakeTimers()
  const lines = [line(1, 'list_assets  调用完成  count=2', 2), line(2, '旧查询条件', 3)]
  const backend = api(lines)
  render(<NotificationProvider><MCPWorkspace backend={backend} hidden={false} preferences={preferences} onTerminalStyle={() => {}} /></NotificationProvider>)
  await act(async () => {})
  expect(screen.getByRole('tab', { name: '日志' })).toHaveAttribute('aria-selected', 'true')
  expect(screen.getByText(/list_assets\s+调用完成\s+count=2/)).toBeInTheDocument()
  expect(screen.queryByText('旧查询条件')).not.toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '详细' }))
  lines.push(line(3, 'list_assets  调用完成  count=1', 2), line(4, '查询条件 profile="work"', 3))
  await act(async () => { await vi.advanceTimersByTimeAsync(1000) })
  expect(screen.getByText('查询条件 profile="work"')).toBeInTheDocument()
  expect(screen.queryByText('旧查询条件')).not.toBeInTheDocument()
})

test('逻辑连接与 SSH 释放分别展示，忙碌关闭需确认，停止服务需确认', async () => {
  const backend = api([]), { preferences } = await createPreviewBackend().bootstrap()
  render(<NotificationProvider><MCPWorkspace backend={backend} hidden={false} preferences={preferences} onTerminalStyle={() => {}} /></NotificationProvider>)
  fireEvent.click(screen.getByRole('tab', { name: /连接/ }))
  expect(await screen.findByText('SSH 已释放')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '关闭连接' }))
  expect(backend.closeConnection).not.toHaveBeenCalled()
  fireEvent.click(screen.getByRole('button', { name: '等待并关闭' }))
  expect(backend.closeConnection).toHaveBeenCalledWith('instance:c:1', 'drain')
  await act(async () => {})
  fireEvent.click(screen.getByRole('button', { name: '停止' }))
  expect(backend.stop).not.toHaveBeenCalled()
  fireEvent.click(screen.getByRole('button', { name: '停止服务' }))
  expect(backend.stop).toHaveBeenCalledTimes(1)
})

test('清空后丢弃清空前发出的日志读取结果', async () => {
  const backend = api([]), { preferences } = await createPreviewBackend().bootstrap()
  let finish!: (value: Awaited<ReturnType<MCPBackend['logs']>>) => void
  backend.logs = vi.fn().mockImplementation(() => new Promise(resolve => { finish = resolve }))
  render(<NotificationProvider><MCPWorkspace backend={backend} hidden={false} preferences={preferences} onTerminalStyle={() => {}} /></NotificationProvider>)
  await act(async () => {})
  fireEvent.click(screen.getByRole('tab', { name: '日志' }))
  fireEvent.click(screen.getByRole('button', { name: '清空显示' }))
  await act(async () => finish({ lines: [line(1, 'stale content')], nextCursor: 1, hasMore: false, gap: false }))
  expect(screen.queryByText('stale content')).not.toBeInTheDocument()
})

test('关闭连接失败后，列表刷新不能抹掉操作错误', async () => {
  const backend = api([]), { preferences } = await createPreviewBackend().bootstrap()
  backend.closeConnection = vi.fn().mockRejectedValue(new Error('连接关闭失败'))
  render(<NotificationProvider><MCPWorkspace backend={backend} hidden={false} preferences={preferences} onTerminalStyle={() => {}} /></NotificationProvider>)
  fireEvent.click(screen.getByRole('tab', { name: /连接/ }))
  await screen.findByText('SSH 已释放')
  fireEvent.click(screen.getByRole('button', { name: '关闭连接' }))
  fireEvent.click(screen.getByRole('button', { name: '等待并关闭' }))
  await act(async () => {})
  expect(screen.getByRole('alert')).toHaveTextContent('连接关闭失败')
})
