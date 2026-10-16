import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, test, vi } from 'vitest'
import { MCPSettings } from './MCPSettings'
import { NotificationProvider } from './Notifications'
import type { MCPBackend, MCPStatus } from '../lib/mcpTypes'

const token = 'A'.repeat(43), nextToken = 'B'.repeat(42) + 'A'
const config = { listenHost: '127.0.0.1', port: 39287, allowLAN: false, tlsCert: '', tlsKey: '', token }
const stopped: MCPStatus = { state: 'stopped', instance: null, config, tokenConfigured: true, restartRequired: false, error: '' }
const running: MCPStatus = { ...stopped, state: 'running', instance: { id: 'instance', host: 'gui', url: 'http://127.0.0.1:39287/mcp', startedAt: '2026-10-16T00:00:00Z', config: { ...config, token: '' } } }
function backend(overrides: Partial<MCPBackend> = {}): MCPBackend {
  return { status: vi.fn().mockResolvedValue(stopped), saveConfig: vi.fn().mockResolvedValue(stopped), start: vi.fn().mockResolvedValue(running), stop: vi.fn().mockResolvedValue(stopped), restart: vi.fn().mockResolvedValue(running), generateToken: vi.fn().mockResolvedValue(nextToken), chooseTLSFile: vi.fn().mockResolvedValue(''), ...overrides }
}
function view(value: MCPBackend, active = true) { return <NotificationProvider><MCPSettings backend={value} active={active} /></NotificationProvider> }

test('初始化不自动启动；Token 明文单栏，缺少 Token 时不能启动', async () => {
  const api = backend({ status: vi.fn().mockResolvedValue({ ...stopped, config: { ...config, token: '' }, tokenConfigured: false }) })
  render(view(api))
  expect(await screen.findByLabelText('MCP Bearer Token')).toHaveAttribute('type', 'text')
  expect(screen.getByText('Bearer Token', { exact: true })).toBeInTheDocument()
  expect(screen.queryByText('必填')).not.toBeInTheDocument()
  expect(screen.getByRole('button', { name: '启动' })).toBeDisabled()
  expect(screen.queryByRole('button', { name: '保存 Token' })).not.toBeInTheDocument()
  expect(screen.getByText('传输协议：Streamable HTTP')).toBeInTheDocument()
  expect(api.start).not.toHaveBeenCalled()
})

test('生成仅修改草稿，统一保存包含 Token，运行中也可编辑', async () => {
  const api = backend({ status: vi.fn().mockResolvedValue(running), saveConfig: vi.fn().mockResolvedValue({ ...running, config: { ...config, token: nextToken }, restartRequired: true }) })
  render(view(api)); await screen.findByLabelText('MCP Bearer Token')
  await userEvent.click(screen.getByRole('button', { name: '生成' }))
  expect(api.generateToken).toHaveBeenCalledWith()
  expect(screen.getByLabelText('MCP Bearer Token')).toHaveValue(nextToken)
  expect(api.saveConfig).not.toHaveBeenCalled()
  expect(screen.getByText('未保存', { exact: true })).toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: '保存' }))
  expect(api.saveConfig).toHaveBeenCalledWith({ ...config, token: nextToken })
  expect(await screen.findByText('配置已保存，重启服务后生效。')).toBeInTheDocument()
  expect(api.restart).not.toHaveBeenCalled()
})

test.each([false, true])('重置恢复已保存配置，仍需重启与否取决于运行快照：%s', async pending => {
  const saved = { ...running, config: { ...config, port: pending ? 39288 : 39287 }, restartRequired: pending }
  const api = backend({ status: vi.fn().mockResolvedValue(saved) })
  render(view(api))
  const port = await screen.findByLabelText('MCP 端口')
  fireEvent.change(port, { target: { value: '39999' } })
  expect(screen.getByText('配置有未保存的修改。保存并重启后生效。')).toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: '重置' }))
  expect(port).toHaveValue(String(saved.config.port))
  if (pending) expect(screen.getByText('配置已保存，重启服务后生效。')).toBeInTheDocument()
  else expect(screen.queryByText(/重启服务后生效/)).not.toBeInTheDocument()
  expect(api.saveConfig).not.toHaveBeenCalled()
  expect(screen.getByRole('button', { name: '保存' })).toBeDisabled()
})

test.each(['', '0', '-1', '65536', '100001', '1.5', '1e3', 'abc'])('无效端口 %s 不能保存', async port => {
  const api = backend(); render(view(api))
  fireEvent.change(await screen.findByLabelText('MCP 端口'), { target: { value: port } })
  await userEvent.click(screen.getByRole('button', { name: '保存' }))
  const heading = screen.getByRole('heading', { name: '连接与访问配置' }).parentElement!
  expect(await within(heading).findByRole('alert')).toHaveTextContent('端口必须为 1–65535 的整数')
  expect(api.saveConfig).not.toHaveBeenCalled()
})

test.each([
  ['MCP 监听地址', '', '监听地址'], ['MCP 监听地址', '999.1.2.3', '监听地址'],
  ['MCP 监听地址', '0.0.0.0', '允许局域网访问'], ['MCP TLS 证书路径', '/missing.crt', '证书和私钥必须同时配置'],
  ['MCP Bearer Token', '', 'Bearer Token 不能为空'], ['MCP Bearer Token', 'invalid', 'Base64url'],
])('校验必要配置 %s=%s', async (label, value, message) => {
  const api = backend(); render(view(api))
  fireEvent.change(await screen.findByLabelText(label), { target: { value } })
  await userEvent.click(screen.getByRole('button', { name: '保存' }))
  expect(await screen.findByRole('alert')).toHaveTextContent(message)
  expect(api.saveConfig).not.toHaveBeenCalled()
})

test('保存失败保留草稿，实际地址只在重启后变化', async () => {
  const changed = { ...config, port: 39288 }
  const api = backend({ status: vi.fn().mockResolvedValue(running), saveConfig: vi.fn().mockRejectedValueOnce(new Error('配置保存失败')).mockResolvedValue({ ...running, config: changed, restartRequired: true }), restart: vi.fn().mockResolvedValue({ ...running, config: changed, instance: { ...running.instance!, url: 'http://127.0.0.1:39288/mcp' } }) })
  render(view(api)); const port = await screen.findByLabelText('MCP 端口')
  fireEvent.change(port, { target: { value: '39288' } })
  await userEvent.click(screen.getByRole('button', { name: '保存' }))
  const form = screen.getByRole('form', { name: 'MCP 配置' })
  expect(await within(form).findByRole('alert')).toHaveTextContent('配置保存失败'); expect(port).toHaveValue('39288')
  await userEvent.click(screen.getByRole('button', { name: '保存' }))
  expect(await screen.findByText('配置已保存，重启服务后生效。')).toBeInTheDocument()
  expect(screen.getByLabelText('MCP 实际地址')).toHaveTextContent(':39287/mcp')
  await userEvent.click(screen.getByRole('button', { name: '重启' }))
  await waitFor(() => expect(screen.queryByText(/重启服务后生效/)).not.toBeInTheDocument())
  expect(screen.getByLabelText('MCP 实际地址')).toHaveTextContent(':39288/mcp')
})

test('选择路径只修改草稿，取消保留原路径；停止时不提示重启', async () => {
  const api = backend({ chooseTLSFile: vi.fn().mockResolvedValueOnce('C:/certs/server.crt').mockResolvedValueOnce('') })
  render(view(api)); await screen.findByLabelText('MCP TLS 证书路径')
  await userEvent.click(screen.getByRole('button', { name: '选择 TLS 证书文件' }))
  expect(api.chooseTLSFile).toHaveBeenCalledWith('certificate', '')
  expect(screen.getByLabelText('MCP TLS 证书路径')).toHaveValue('C:/certs/server.crt')
  await userEvent.click(screen.getByRole('button', { name: '选择 TLS 证书文件' }))
  expect(screen.getByLabelText('MCP TLS 证书路径')).toHaveValue('C:/certs/server.crt')
  expect(screen.queryByText(/重启后生效|重启服务后生效/)).not.toBeInTheDocument()
  expect(api.saveConfig).not.toHaveBeenCalled()
})

test('局域网允许保存无 TLS 的 HTTP + Token 配置', async () => {
  const api = backend(); render(view(api))
  fireEvent.change(await screen.findByLabelText('MCP 监听地址'), { target: { value: '0.0.0.0' } })
  await userEvent.click(screen.getByRole('switch', { name: '允许 MCP 局域网访问' }))
  await userEvent.click(screen.getByRole('button', { name: '保存' }))
  expect(api.saveConfig).toHaveBeenCalledWith({ ...config, listenHost: '0.0.0.0', allowLAN: true })
})

test('启动失败保留停止状态；外部宿主不能在本窗口启停或重启', async () => {
  const api = backend({ start: vi.fn().mockRejectedValue(new Error('端口占用')) })
  const shown = render(view(api)); await screen.findByLabelText('MCP 端口')
  await userEvent.click(screen.getByRole('button', { name: '启动' }))
  expect(await screen.findByText('端口占用')).toBeInTheDocument()
  expect(within(screen.getByRole('form', { name: 'MCP 配置' })).queryByRole('alert')).not.toBeInTheDocument()
  fireEvent.change(screen.getByLabelText('MCP 端口'), { target: { value: '100001' } })
  await userEvent.click(screen.getByRole('button', { name: '保存' }))
  expect(screen.getByText('端口占用')).toBeInTheDocument()
  expect(within(screen.getByRole('form', { name: 'MCP 配置' })).getByRole('alert')).toHaveTextContent('端口必须为 1–65535 的整数')
  await userEvent.click(screen.getByRole('button', { name: '重置' }))
  expect(within(screen.getByRole('form', { name: 'MCP 配置' })).queryByRole('alert')).not.toBeInTheDocument()
  expect(screen.getByText('端口占用')).toBeInTheDocument()
  expect(screen.getByText('已停止', { exact: true })).toBeInTheDocument(); shown.unmount()
  render(view(backend({ status: vi.fn().mockResolvedValue({ ...running, state: 'external', instance: { ...running.instance!, host: 'cli' } }) })))
  expect(await screen.findByText('其他进程占用')).toBeInTheDocument()
  for (const name of ['启动', '停止', '重启']) expect(screen.getByRole('button', { name })).toBeDisabled()
})

test('启动合并重复点击；慢轮询不覆盖启动结果和配置草稿', async () => {
  vi.useFakeTimers()
  try {
    let finishPoll!: (value: MCPStatus) => void, finishStart!: (value: MCPStatus) => void
    const api = backend({ status: vi.fn().mockResolvedValueOnce(stopped).mockImplementationOnce(() => new Promise(resolve => { finishPoll = resolve })).mockResolvedValue(running), start: vi.fn().mockImplementation(() => new Promise(resolve => { finishStart = resolve })) })
    const shown = render(view(api)); await act(async () => {})
    await act(async () => { await vi.advanceTimersByTimeAsync(3000) })
    fireEvent.click(screen.getByRole('button', { name: '启动' })); fireEvent.click(screen.getByRole('button', { name: '启动' }))
    expect(api.start).toHaveBeenCalledTimes(1)
    await act(async () => finishStart(running))
    fireEvent.change(screen.getByLabelText('MCP 端口'), { target: { value: '39300' } })
    await act(async () => finishPoll(stopped)); expect(screen.getByText('运行中')).toBeInTheDocument()
    await act(async () => { await vi.advanceTimersByTimeAsync(3000) })
    expect(screen.getByLabelText('MCP 端口')).toHaveValue('39300'); shown.unmount()
  } finally { vi.useRealTimers() }
})
