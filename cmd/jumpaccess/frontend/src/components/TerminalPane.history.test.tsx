import { act, render } from '@testing-library/react'
import { beforeEach, expect, test, vi } from 'vitest'
import type { Terminal } from '@xterm/xterm'
import { TerminalPane } from './TerminalPane'
import { TerminalOutput } from '../model/terminalOutput'
import { createPreviewBackend } from '../lib/previewBackend'
import type { SessionState } from '../lib/backend'

const { terminals } = vi.hoisted(() => {
  vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue(null)
  return { terminals: [] as Terminal[] }
})
// 使用真实解析器和缓冲区；只替身不属于历史管理的 DOM 渲染、尺寸测量。
vi.mock('@xterm/xterm', async importOriginal => {
  const original = await importOriginal<typeof import('@xterm/xterm')>()
  return { ...original, Terminal: class extends original.Terminal {
    constructor(options: ConstructorParameters<typeof original.Terminal>[0]) { super(options); terminals.push(this) }
    open() {}
    focus() {}
  } }
})
vi.mock('./TerminalFitAddon', () => ({ TerminalFitAddon: class { activate() {} fit() {} dispose() {} } }))

beforeEach(() => { terminals.length = 0 })
const session: SessionState = { id: 'ssh-a', status: 'active', title: 'A', profile: 'p', organization: 'o', asset: 'a', account: 'root', error: '' }
const line = (n: number) => `${String(n).padStart(6, '0')}${'x'.repeat(100)}\r\n`

test.each([0, 1, 10000, 50000])('真实终端按 %i 行裁剪，不受旧字符额度限制', async limit => {
  const backend = createPreviewBackend()
  const preferences = { ...(await backend.bootstrap()).preferences, terminalScrollbackLines: limit }
  const output = new TerminalOutput()
  const { unmount } = render(<TerminalPane backend={backend} output={output} preferences={preferences} session={session} />)
  const terminal = terminals[0]
  const data = Array.from({ length: limit + terminal.rows + 5 }, (_, i) => line(i)).join('')
  await act(async () => { output.write(data); await output.drain() })
  expect(terminal.buffer.active.baseY).toBe(limit)
  expect(terminal.buffer.active.length).toBe(limit + terminal.rows)
  expect(terminal.buffer.active.getLine(0)?.translateToString(true)).toBe(line(6).trim())
  unmount()
  output.dispose()
}, 15000)

test('各 Tab 独立保留历史，后台输出和切换不清空，调整额度不重建终端', async () => {
  const backend = createPreviewBackend()
  const preferences = { ...(await backend.bootstrap()).preferences, terminalScrollbackLines: 10000 }
  const a = new TerminalOutput()
  const b = new TerminalOutput()
  const view = (activeA: boolean, limit = 10000) => <>
    <TerminalPane key="a" active={activeA} backend={backend} output={a} preferences={{ ...preferences, terminalScrollbackLines: limit }} session={session} />
    <TerminalPane key="b" active={!activeA} backend={backend} output={b} preferences={{ ...preferences, terminalScrollbackLines: limit }} session={{ ...session, id: 'ssh-b', title: 'B' }} />
  </>
  const { rerender } = render(view(true))
  await act(async () => {
    a.write('A\r\n'.repeat(11000))
    b.write('B\r\n'.repeat(11000))
    await Promise.all([a.drain(), b.drain()])
  })
  expect(terminals.map(t => t.buffer.active.baseY)).toEqual([10000, 10000])
  expect(terminals[0].buffer.active.getLine(0)?.translateToString(true)).toBe('A')
  expect(terminals[1].buffer.active.getLine(0)?.translateToString(true)).toBe('B')
  rerender(view(false))
  rerender(view(true, 1))
  expect(terminals).toHaveLength(2)
  expect(terminals.map(t => t.buffer.active.baseY)).toEqual([1, 1])
  rerender(view(false, 50000))
  expect(terminals.map(t => t.buffer.active.baseY)).toEqual([1, 1])
})

test('真实终端缩窄后按折行清理，放宽不恢复已经删除的历史', async () => {
  const backend = createPreviewBackend()
  const preferences = { ...(await backend.bootstrap()).preferences, terminalScrollbackLines: 5 }
  const output = new TerminalOutput()
  render(<TerminalPane backend={backend} output={output} preferences={preferences} session={session} />)
  const terminal = terminals[0]
  terminal.resize(12, 4)
  await act(async () => { output.write(Array.from({ length: 8 }, (_, i) => `${i}abcdefghijk`).join('\r\n') + '\r\n>'); await output.drain() })
  terminal.resize(6, 4)
  expect(terminal.buffer.active.baseY).toBe(5)
  expect(terminal.buffer.active.getLine(0)?.translateToString(true)).toBe('4abcde')
  terminal.resize(12, 4)
  expect(terminal.buffer.active.baseY).toBe(1)
  expect(terminal.buffer.active.getLine(0)?.translateToString(true)).toBe('4abcdefghijk')
})

test('断连与重连保留同一终端及历史，输入和尺寸更新切换到新会话', async () => {
  const backend = createPreviewBackend()
  const write = vi.spyOn(backend, 'writeSSHSession').mockResolvedValue(undefined)
  const resize = vi.spyOn(backend, 'resizeSSHSession').mockResolvedValue(undefined)
  const preferences = (await backend.bootstrap()).preferences
  const output = new TerminalOutput()
  const view = (state: SessionState, stream = output) => <TerminalPane backend={backend} output={stream} preferences={preferences} session={state} />
  const { rerender } = render(view({ ...session, id: '', status: 'connecting' }))
  rerender(view(session))
  terminals[0].input('pwd\r', true)
  expect(write).toHaveBeenLastCalledWith(session.id, 'pwd\r')
  await act(async () => { output.write('keep-before-disconnect\r\n'); await output.drain() })
  rerender(view({ ...session, id: '', status: 'closed' }))
  expect(terminals).toHaveLength(1)
  expect(terminals[0].buffer.active.getLine(0)?.translateToString(true)).toBe('keep-before-disconnect')
  write.mockClear()
  resize.mockClear()
  terminals[0].input('closed', true)
  terminals[0].resize(90, 24)
  expect(write).not.toHaveBeenCalled()
  expect(resize).not.toHaveBeenCalled()
  await act(async () => { output.writeStatus('\r\nReconnecting ...\r\n'); await output.drain() })
  rerender(view({ ...session, id: 'ssh-reconnected' }))
  expect(terminals).toHaveLength(1)
  expect(terminals[0].buffer.active.getLine(0)?.translateToString(true)).toBe('keep-before-disconnect')
  terminals[0].input('whoami\r', true)
  expect(write).toHaveBeenLastCalledWith('ssh-reconnected', 'whoami\r')
  terminals[0].resize(80, 24)
  expect(resize).toHaveBeenLastCalledWith('ssh-reconnected', 80, 24)
})

test.each(['normal', 'alternate'])('%s 屏幕断线后保留最后内容，恢复终端模式并在末尾追加提示', async mode => {
  const backend = createPreviewBackend()
  const preferences = (await backend.bootstrap()).preferences
  const output = new TerminalOutput()
  render(<TerminalPane backend={backend} output={output} preferences={preferences} session={session} />)
  const terminal = terminals[0]
  terminal.resize(80, 6)
  await act(async () => {
    output.write('shell-history\r\n$ ')
    if (mode === 'alternate') output.write('\x1b[?1049h')
    output.write('\x1b[4;1Himportant-last-screen\x1b[1;1H\x1b[31m\x1b[?25l\x1b[?1003h\x1b[?1006h\x1b[?2004h\x1b[?1h\x1b[2;3r')
    // 断线可能截断一个控制序列；本地提示必须仍然可见。
    output.write('\x1b]0;unfinished-title')
    output.writeStatus('\r\nConnection closed.\r\n\r\nPress Enter to reconnect ...\r\n')
    output.writeStatus('\r\nReconnecting ...\r\n')
    output.write('new-shell> ')
    await output.drain()
  })
  const lines = Array.from({ length: terminal.buffer.active.length }, (_, i) => terminal.buffer.active.getLine(i)!.translateToString(true))
  expect(terminal.buffer.active.type).toBe('normal')
  expect(lines.join('\n')).toMatch(/shell-history[\s\S]*important-last-screen[\s\S]*Connection closed\.[\s\S]*Reconnecting \.\.\.[\s\S]*new-shell>/)
  expect(terminal.modes).toMatchObject({ applicationCursorKeysMode: false, bracketedPasteMode: false, mouseTrackingMode: 'none' })
  const prompt = lines.findIndex(text => text === 'new-shell> ')
  expect(terminal.buffer.active.getLine(prompt)?.getCell(0)?.isFgDefault()).toBeTruthy()
})

test.each([0, 1, 5])('多轮连接共用 %i 行历史额度，关闭才销毁终端', async limit => {
  const backend = createPreviewBackend()
  const preferences = { ...(await backend.bootstrap()).preferences, terminalScrollbackLines: limit }
  const output = new TerminalOutput()
  const { unmount } = render(<TerminalPane backend={backend} output={output} preferences={preferences} session={session} />)
  const terminal = terminals[0]
  const dispose = vi.spyOn(terminal, 'dispose')
  terminal.resize(80, 4)
  await act(async () => {
    output.write('old-history\r\n'.repeat(20))
    output.writeStatus('\r\nConnection closed.\r\n')
    output.writeStatus('\r\nReconnecting ...\r\n')
    output.write('new-history\r\n'.repeat(20))
    await output.drain()
  })
  expect(terminals).toHaveLength(1)
  expect(terminal.buffer.active.baseY).toBe(limit)
  expect(terminal.buffer.active.length).toBe(limit + terminal.rows)
  expect(terminal.buffer.active.getLine(0)?.translateToString(true)).toBe('new-history')
  expect(dispose).not.toHaveBeenCalled()
  unmount()
  expect(dispose).toHaveBeenCalledOnce()
})

test('只有本地提示时更换输出队列，真实终端从第一行显示当前重连状态', async () => {
  const backend = createPreviewBackend()
  const preferences = (await backend.bootstrap()).preferences
  const old = new TerminalOutput()
  const view = (output: TerminalOutput) => <TerminalPane backend={backend} output={output} preferences={preferences} session={{ ...session, id: '', status: 'closed' }} />
  const { rerender } = render(view(old))
  await act(async () => {
    old.writeStatus('Connection closed.\r\n\r\nPress Enter to reconnect ...\r\n')
    await old.drain()
  })
  expect(terminals[0].buffer.active.getLine(0)?.translateToString(true)).toBe('Connection closed.')
  const next = new TerminalOutput()
  old.dispose()
  rerender(view(next))
  await act(async () => {
    next.writeStatus('Reconnecting ...\r\n')
    next.write('welcome\r\n')
    await next.drain()
  })
  expect(terminals).toHaveLength(2)
  const terminal = terminals[1]
  expect(terminal.buffer.active.baseY).toBe(0)
  expect(terminal.buffer.active.getLine(0)?.translateToString(true)).toBe('Reconnecting ...')
  expect(terminal.buffer.active.getLine(1)?.translateToString(true)).toBe('welcome')
  expect(terminal.buffer.active.getLine(2)?.translateToString(true)).toBe('')
})
