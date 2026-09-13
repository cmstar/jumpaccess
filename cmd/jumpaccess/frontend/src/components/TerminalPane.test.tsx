import { defaultTerminalBackground } from '../model/terminalBackground'
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, expect, test, vi } from 'vitest'

import { TerminalPane } from './TerminalPane'
import { TerminalBackgroundProvider } from './TerminalBackground'
import type { Backend, Preferences, SessionState } from '../lib/backend'

const terminalMock = vi.hoisted(() => ({
  dataHandler: undefined as ((data: string) => void) | undefined,
  keyHandler: undefined as ((event: KeyboardEvent) => boolean) | undefined,
  instances: 0,
  options: undefined as { fontFamily?: string; fontSize?: number; theme?: { background?: string; red?: string } } | undefined,
  oscHandlers: new Map<number, (data: string) => boolean | Promise<boolean>>(),
  resizeHandler: undefined as ((size: { cols: number; rows: number }) => void) | undefined,
  selection: '',
  selectionHandler: undefined as (() => void) | undefined,
  inputs: [] as string[],
  pasted: [] as string[],
  writeCallbacks: [] as Array<() => void>,
  writeResponse: '',
}))

vi.mock('@xterm/xterm', () => ({
  Terminal: class {
    cols = 120
    rows = 34
    parser = {
      registerOscHandler: (ident: number, handler: (data: string) => boolean | Promise<boolean>) => {
        terminalMock.oscHandlers.set(ident, handler)
        return { dispose: () => terminalMock.oscHandlers.delete(ident) }
      },
    }
    constructor(public options: NonNullable<typeof terminalMock.options>) { terminalMock.instances += 1; terminalMock.options = options }
    loadAddon() {}
    open() {}
    write(_data: string, callback?: () => void) {
      if (terminalMock.writeResponse) terminalMock.dataHandler?.(terminalMock.writeResponse)
      if (callback) terminalMock.writeCallbacks.push(callback)
    }
    reset() {}
    focus() {}
    getSelection() { return terminalMock.selection }
    hasSelection() { return terminalMock.selection.length > 0 }
    clearSelection() { terminalMock.selection = ''; terminalMock.selectionHandler?.() }
    input(data: string) { terminalMock.inputs.push(data); terminalMock.dataHandler?.(data) }
    paste(data: string) { terminalMock.pasted.push(data) }
    dispose() {}
    onData(handler: (data: string) => void) {
      terminalMock.dataHandler = handler
      return { dispose() {} }
    }
    onResize(handler: (size: { cols: number; rows: number }) => void) {
      terminalMock.resizeHandler = handler
      return { dispose() {} }
    }
    onSelectionChange(handler: () => void) {
      terminalMock.selectionHandler = handler
      return { dispose() {} }
    }
    attachCustomKeyEventHandler(handler: (event: KeyboardEvent) => boolean) {
      terminalMock.keyHandler = handler
    }
  },
}))

vi.mock('@xterm/addon-fit', () => ({
  FitAddon: class {
    fit() {}
  },
}))

const preferences: Preferences = {
  terminalBackground: { ...defaultTerminalBackground },
  version: 6,
  theme: 'light',
  terminalFontFamily: 'JetBrains Mono',
  terminalFontSize: 12,
  terminalLineHeight: 1,
  terminalCursorStyle: 'block',
  terminalCursorBlink: true,
  terminalScrollbarVisibility: 'active',
  terminalColorScheme: 'nord',
  terminalRightClickAction: 'paste',
  terminalWarnOnMultiLinePaste: true,
  terminalCopyOnEnter: true, downloadMode: 'ask', downloadDirectory: '',
  confirmCloseActiveSession: true,
  showTabCloseButtons: true,
  newTabPosition: 'end',
}

const disconnectedSession: SessionState = {
  id: 'session-1',
  status: 'closed',
  title: 'production-web',
  profile: 'production',
  organization: 'org-1',
  asset: 'asset-1',
  account: 'account-1',
  error: '',
}

const activeSession: SessionState = { ...disconnectedSession, status: 'active' }

// 模拟 xterm：仅当按键被放行时，才进入现有的 SSH 输入通道。
function pressEnter(type = 'keydown', init: KeyboardEventInit = {}) {
  const event = new KeyboardEvent(type, { key: 'Enter', cancelable: true, ...init })
  let allowed: boolean | undefined
  act(() => {
    allowed = terminalMock.keyHandler?.(event)
    if (allowed && type === 'keydown') terminalMock.dataHandler?.('\r')
  })
  return { event, allowed }
}

function setupEnterCopy(session = activeSession, transferBusy = false) {
  const writeText = vi.fn().mockResolvedValue(undefined)
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
  const backend = { resizeSSHSession: vi.fn().mockResolvedValue(undefined), writeSSHSession: vi.fn().mockResolvedValue(undefined) } as unknown as Backend
  const onReconnect = vi.fn()
  const onActionsChange = vi.fn()
  const view = (enabled: boolean) => <TerminalPane backend={backend} onReconnect={onReconnect} onActionsChange={onActionsChange} output="" preferences={{ ...preferences, terminalCopyOnEnter: enabled }} session={session} transferBusy={transferBusy} />
  const rendered = render(view(true))
  return { ...rendered, view, backend, writeText, onReconnect, onActionsChange }
}

test('有选区时 Enter 复制并取消选区，无选区时正常发送回车', async () => {
  const { backend, writeText, onActionsChange } = setupEnterCopy()
  terminalMock.selection = '第一行\nsecond line'
  const { event, allowed } = pressEnter()
  expect(allowed).toBe(false)
  expect(event.defaultPrevented).toBe(true)
  await waitFor(() => expect(terminalMock.selection).toBe(''))
  expect(writeText).toHaveBeenCalledWith('第一行\nsecond line')
  expect(onActionsChange.mock.calls.at(-1)?.[0]?.canCopy).toBe(false)
  expect(backend.writeSSHSession).not.toHaveBeenCalled()
  pressEnter('keyup')
  expect(pressEnter().allowed).toBe(true)
  expect(backend.writeSSHSession).toHaveBeenCalledExactlyOnceWith(activeSession.id, '\r')
})

test('复制回车的 keypress 和长按重复事件在松键前全部拦截', async () => {
  const { backend, writeText, rerender, view } = setupEnterCopy()
  terminalMock.selection = 'selected'
  pressEnter()
  await waitFor(() => expect(terminalMock.selection).toBe(''))
  rerender(view(false))
  expect(pressEnter('keypress').allowed).toBe(false)
  expect(pressEnter('keydown', { repeat: true }).allowed).toBe(false)
  expect(pressEnter('keyup').allowed).toBe(false)
  expect(writeText).toHaveBeenCalledTimes(1)
  expect(backend.writeSSHSession).not.toHaveBeenCalled()
  expect(pressEnter().allowed).toBe(true)
})

test('回车复制开关立即作用于已有终端，关闭后保留选区并放行回车', async () => {
  const { rerender, view, backend, writeText } = setupEnterCopy()
  const instances = terminalMock.instances
  terminalMock.selection = 'selected'
  rerender(view(false))
  expect(terminalMock.instances).toBe(instances)
  expect(terminalMock.selection).toBe('selected')
  expect(pressEnter().allowed).toBe(true)
  expect(writeText).not.toHaveBeenCalled()
  expect(backend.writeSSHSession).toHaveBeenCalledTimes(1)
  rerender(view(true))
  expect(pressEnter().allowed).toBe(false)
  await waitFor(() => expect(terminalMock.selection).toBe(''))
  expect(terminalMock.instances).toBe(instances)
})

test.each(['rejected', 'unavailable'])('剪贴板 %s 时保留选区且不发送回车', async (failure) => {
  const { backend, writeText } = setupEnterCopy()
  if (failure === 'rejected') writeText.mockRejectedValue(new Error('denied'))
  else Object.defineProperty(navigator, 'clipboard', { configurable: true, value: undefined })
  terminalMock.selection = 'selected'
  await act(async () => { expect(pressEnter().allowed).toBe(false) })
  expect(terminalMock.selection).toBe('selected')
  expect(backend.writeSSHSession).not.toHaveBeenCalled()
})

test.each([
  { ctrlKey: true }, { altKey: true }, { shiftKey: true }, { metaKey: true },
  { isComposing: true }, { keyCode: 229 },
])('组合键或输入法确认不触发回车复制：%j', (init) => {
  const { writeText } = setupEnterCopy()
  terminalMock.selection = 'selected'
  expect(pressEnter('keydown', init).allowed).toBe(true)
  expect(terminalMock.selection).toBe('selected')
  expect(writeText).not.toHaveBeenCalled()
})

test('composition 期间不复制，结束后普通 Enter 恢复复制', async () => {
  const { container, writeText } = setupEnterCopy()
  const host = container.querySelector('.terminal-host')!
  terminalMock.selection = 'selected'
  fireEvent.compositionStart(host)
  expect(pressEnter().allowed).toBe(true)
  expect(writeText).not.toHaveBeenCalled()
  fireEvent.compositionEnd(host)
  expect(pressEnter().allowed).toBe(false)
  await waitFor(() => expect(writeText).toHaveBeenCalledTimes(1))
})

test('断线后优先复制选区，松键后的下一次回车才重连', async () => {
  const { onReconnect } = setupEnterCopy(disconnectedSession)
  terminalMock.selection = 'history'
  pressEnter()
  await waitFor(() => expect(terminalMock.selection).toBe(''))
  expect(onReconnect).not.toHaveBeenCalled()
  pressEnter('keydown', { repeat: true })
  expect(onReconnect).not.toHaveBeenCalled()
  pressEnter('keyup')
  pressEnter()
  expect(onReconnect).toHaveBeenCalledTimes(1)
})

test('文件传输期间允许回车复制历史文本但不发送输入', async () => {
  const { backend, writeText } = setupEnterCopy(activeSession, true)
  terminalMock.selection = 'history'
  pressEnter()
  await waitFor(() => expect(writeText).toHaveBeenCalledWith('history'))
  pressEnter('keyup')
  expect(pressEnter().allowed).toBe(false)
  expect(backend.writeSSHSession).not.toHaveBeenCalled()
})

test('异步复制完成不清除用户后来重新建立的同文选区', async () => {
  const { writeText } = setupEnterCopy()
  let finish!: () => void
  writeText.mockImplementation(() => new Promise<void>((resolve) => { finish = resolve }))
  terminalMock.selection = 'same text'
  pressEnter()
  act(() => terminalMock.selectionHandler?.())
  await act(async () => finish())
  expect(terminalMock.selection).toBe('same text')
})

test('复制期间终端卸载，迟到的剪贴板结果不再修改终端', async () => {
  const { writeText, unmount } = setupEnterCopy()
  let finish!: () => void
  writeText.mockImplementation(() => new Promise<void>((resolve) => { finish = resolve }))
  terminalMock.selection = 'history'
  pressEnter()
  unmount()
  await act(async () => finish())
  expect(terminalMock.selection).toBe('history')
})

test('启用和关闭背景图不重建终端，保留选区及输入通道', async () => {
  let loaded: (() => void) | null = null
  vi.stubGlobal('Image', class { naturalWidth = 100; naturalHeight = 100; src = ''; onerror = null; set onload(value: (() => void) | null) { loaded = value } })
  try {
    const backend = { resizeSSHSession: vi.fn().mockResolvedValue(undefined), writeSSHSession: vi.fn().mockResolvedValue(undefined), readTerminalBackground: vi.fn().mockResolvedValue('data:image/png;base64,test') } as unknown as Backend
    const view = (enabled: boolean) => <TerminalBackgroundProvider backend={backend} settings={{ ...defaultTerminalBackground, enabled, filePath: 'a.png' }}><TerminalPane backend={backend} output="" preferences={preferences} session={activeSession} /></TerminalBackgroundProvider>
    const { rerender } = render(view(false))
    terminalMock.selection = '保留选区'
    rerender(view(true))
    await waitFor(() => expect(loaded).not.toBeNull())
    act(() => loaded?.())
    expect(terminalMock.options?.theme?.background).toBe('#2e344000')
    expect(terminalMock.instances).toBe(1)
    expect(terminalMock.selection).toBe('保留选区')
    act(() => terminalMock.dataHandler?.('pwd\r'))
    expect(backend.writeSSHSession).toHaveBeenCalledWith('session-1', 'pwd\r')
    rerender(view(false))
    expect(terminalMock.options?.theme?.background).toBe('#2e3440')
    expect(terminalMock.instances).toBe(1)
  } finally { vi.unstubAllGlobals() }
})

test('滚动条开关即时生效，保留终端历史、选区及输入通道', () => {
  const backend = { resizeSSHSession: vi.fn().mockResolvedValue(undefined), writeSSHSession: vi.fn().mockResolvedValue(undefined) } as unknown as Backend
  const view = (terminalScrollbarVisibility: 'always' | 'active' | 'hidden') => <TerminalPane backend={backend} output="history" preferences={{ ...preferences, terminalScrollbarVisibility }} session={activeSession} />
  const { rerender } = render(view('active'))
  const host = screen.getByLabelText('production-web SSH 终端')
  expect(host).toHaveAttribute('data-terminal-scrollbar-visibility', 'active')
  act(() => terminalMock.writeCallbacks.forEach(callback => callback()))
  const writes = terminalMock.writeCallbacks.length
  terminalMock.selection = 'history'
  vi.mocked(backend.resizeSSHSession).mockClear()
  rerender(view('hidden'))
  expect(host).toHaveAttribute('data-terminal-scrollbar-visibility', 'hidden')
  expect(terminalMock.instances).toBe(1)
  expect(terminalMock.writeCallbacks).toHaveLength(writes)
  expect(terminalMock.selection).toBe('history')
  expect(backend.resizeSSHSession).toHaveBeenCalled()
  act(() => terminalMock.dataHandler?.('pwd\r'))
  expect(backend.writeSSHSession).toHaveBeenCalledWith('session-1', 'pwd\r')
  rerender(view('active'))
  expect(host).toHaveAttribute('data-terminal-scrollbar-visibility', 'active')
  expect(terminalMock.instances).toBe(1)
})

test('ZMODEM 传输期间暂停终端输入，完成后恢复', () => {
  const backend = { resizeSSHSession: vi.fn().mockResolvedValue(undefined), writeSSHSession: vi.fn().mockResolvedValue(undefined) } as unknown as Backend
  const { rerender } = render(<TerminalPane backend={backend} output="" preferences={preferences} session={activeSession} transferBusy />)
  act(() => terminalMock.dataHandler?.('x'))
  expect(backend.writeSSHSession).not.toHaveBeenCalled()
  rerender(<TerminalPane backend={backend} output="" preferences={preferences} session={activeSession} transferBusy={false} />)
  act(() => terminalMock.dataHandler?.('pwd\r'))
  expect(backend.writeSSHSession).toHaveBeenCalledWith('session-1', 'pwd\r')
})

beforeEach(() => {
  terminalMock.dataHandler = undefined
  terminalMock.keyHandler = undefined
  terminalMock.instances = 0
  terminalMock.oscHandlers.clear()
  terminalMock.resizeHandler = undefined
  terminalMock.selection = ''
  terminalMock.selectionHandler = undefined
  terminalMock.inputs = []
  terminalMock.pasted = []
  terminalMock.writeCallbacks = []
  terminalMock.writeResponse = ''
})

test('配色和字体即时更新且保留终端选区，外围主题不改变终端配色', () => {
  const backend = { resizeSSHSession: vi.fn().mockResolvedValue(undefined), writeSSHSession: vi.fn() } as unknown as Backend
  const initial = { ...preferences, terminalColorScheme: 'nord' }
  const { rerender } = render(<TerminalPane backend={backend} output="history" preferences={initial} session={activeSession} />)
  const instances = terminalMock.instances
  terminalMock.selection = 'history'
  const changed = { ...initial, terminalColorScheme: 'catppuccin-latte', terminalFontFamily: 'Consolas', terminalFontSize: 18 }
  rerender(<TerminalPane backend={backend} output="history" preferences={changed} session={activeSession} />)
  expect(terminalMock.instances).toBe(instances)
  expect(terminalMock.options).toMatchObject({ fontFamily: 'Consolas', fontSize: 18, theme: { background: '#eff1f5', red: '#d20f39' } })
  expect(terminalMock.selection).toBe('history')
  expect(screen.getByLabelText('production-web SSH 终端')).toHaveStyle({ backgroundColor: '#eff1f5' })
  rerender(<TerminalPane backend={backend} output="history" preferences={{ ...changed, theme: 'dark' }} session={activeSession} />)
  expect(terminalMock.instances).toBe(instances)
  expect(terminalMock.options?.theme?.background).toBe('#eff1f5')
})

test.each([
  { change: { terminalLineHeight: 1.5 }, expected: { lineHeight: 1.5 } },
  { change: { terminalCursorStyle: 'underline' as const }, expected: { cursorStyle: 'underline' } },
  { change: { terminalCursorStyle: 'quarter_block' as const }, expected: { cursorStyle: 'underline' } },
  { change: { terminalCursorBlink: false }, expected: { cursorBlink: false } },
])('行高和光标设置 $change 即时更新，不重建终端且重新报告尺寸', ({ change, expected }) => {
  const backend = { resizeSSHSession: vi.fn().mockResolvedValue(undefined), writeSSHSession: vi.fn() } as unknown as Backend
  const { rerender } = render(<TerminalPane backend={backend} output="history" preferences={preferences} session={activeSession} />)
  const instances = terminalMock.instances
  const writes = terminalMock.writeCallbacks.length
  terminalMock.selection = 'history'
  vi.mocked(backend.resizeSSHSession).mockClear()
  const changed = { ...preferences, ...change }
  rerender(<TerminalPane backend={backend} output="history" preferences={changed} session={activeSession} />)
  expect(terminalMock.options).toMatchObject(expected)
  expect(screen.getByLabelText('production-web SSH 终端')).toHaveAttribute('data-terminal-cursor-style', changed.terminalCursorStyle)
  expect(terminalMock.instances).toBe(instances)
  expect(terminalMock.selection).toBe('history')
  expect(terminalMock.writeCallbacks).toHaveLength(writes)
  expect(backend.resizeSSHSession).toHaveBeenCalledWith(activeSession.id, 120, 34)
})

test('终端动作随选区变化并复用复制粘贴实现', async () => {
  const writeText = vi.fn().mockResolvedValue(undefined)
  const readText = vi.fn().mockResolvedValue('whoami')
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { readText, writeText } })
  const onActionsChange = vi.fn()
  const backend = {
    writeSSHSession: vi.fn().mockResolvedValue(undefined),
    resizeSSHSession: vi.fn().mockResolvedValue(undefined),
  } as unknown as Backend

  render(<TerminalPane backend={backend} onActionsChange={onActionsChange} output="" preferences={preferences} session={activeSession} />)
  expect(onActionsChange.mock.calls.at(-1)?.[0]?.canCopy).toBe(false)

  terminalMock.selection = 'selected output'
  act(() => terminalMock.selectionHandler?.())
  const actions = onActionsChange.mock.calls.at(-1)?.[0]
  expect(actions.canCopy).toBe(true)
  await actions.copy()
  await actions.paste()

  expect(writeText).toHaveBeenCalledWith('selected output')
  expect(terminalMock.inputs).toEqual(['whoami'])
  expect(terminalMock.pasted).toEqual([])
})

test('Ctrl + Insert 和 Shift + Insert 调用终端复制粘贴', async () => {
  const writeText = vi.fn().mockResolvedValue(undefined)
  const readText = vi.fn().mockResolvedValue('pwd')
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { readText, writeText } })
  terminalMock.selection = 'copy me'
  const backend = {
    writeSSHSession: vi.fn().mockResolvedValue(undefined),
    resizeSSHSession: vi.fn().mockResolvedValue(undefined),
  } as unknown as Backend
  render(<TerminalPane backend={backend} output="" preferences={preferences} session={activeSession} />)

  const copy = new KeyboardEvent('keydown', { key: 'Insert', ctrlKey: true, cancelable: true })
  const paste = new KeyboardEvent('keydown', { key: 'Insert', shiftKey: true, cancelable: true })
  act(() => {
    terminalMock.keyHandler?.(copy)
    terminalMock.keyHandler?.(paste)
  })

  await waitFor(() => expect(writeText).toHaveBeenCalledWith('copy me'))
  await waitFor(() => expect(terminalMock.inputs).toEqual(['pwd']))
  expect(terminalMock.pasted).toEqual([])
  expect(copy.defaultPrevented).toBe(true)
  expect(paste.defaultPrevented).toBe(true)
})

test('默认右键读取剪贴板并通过终端粘贴', async () => {
  const readText = vi.fn().mockResolvedValue('printf hello')
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { readText, writeText: vi.fn() } })
  const backend = {
    writeSSHSession: vi.fn().mockResolvedValue(undefined),
    resizeSSHSession: vi.fn().mockResolvedValue(undefined),
  } as unknown as Backend

  render(<TerminalPane backend={backend} output="" preferences={preferences} session={activeSession} />)
  fireEvent.contextMenu(screen.getByLabelText('production-web SSH 终端'))

  await waitFor(() => expect(terminalMock.inputs).toEqual(['printf hello']))
  expect(terminalMock.pasted).toEqual([])
  expect(screen.queryByRole('menu')).not.toBeInTheDocument()
})

test('多行或末尾换行的粘贴默认显示预览，确认后才按普通输入发送', async () => {
  const readText = vi.fn().mockResolvedValue('echo first\r\necho second\n')
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { readText, writeText: vi.fn() } })
  const onActionsChange = vi.fn()
  const backend = {
    writeSSHSession: vi.fn().mockResolvedValue(undefined),
    resizeSSHSession: vi.fn().mockResolvedValue(undefined),
  } as unknown as Backend

  render(<TerminalPane backend={backend} onActionsChange={onActionsChange} output="" preferences={preferences} session={activeSession} />)
  await onActionsChange.mock.calls.at(-1)?.[0].paste()

  const dialog = await screen.findByRole('dialog', { name: '多行粘贴警告' })
  expect(screen.getByLabelText('剪贴板内容预览')).toHaveTextContent('echo first echo second')
  expect(dialog).toHaveTextContent('3 行')
  expect(dialog).toHaveTextContent('末尾包含换行')
  expect(terminalMock.inputs).toEqual([])
  expect(terminalMock.pasted).toEqual([])

  fireEvent.click(screen.getByRole('button', { name: '仍然粘贴' }))
  await waitFor(() => expect(terminalMock.inputs).toEqual(['echo first\recho second\r']))
  expect(screen.queryByRole('dialog', { name: '多行粘贴警告' })).not.toBeInTheDocument()
  expect(terminalMock.pasted).toEqual([])
})

test('取消多行粘贴不会写入终端，关闭警告后则直接发送', async () => {
  const readText = vi.fn().mockResolvedValue('echo first\necho second')
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { readText, writeText: vi.fn() } })
  const onActionsChange = vi.fn()
  const backend = {
    writeSSHSession: vi.fn().mockResolvedValue(undefined),
    resizeSSHSession: vi.fn().mockResolvedValue(undefined),
  } as unknown as Backend
  const { rerender } = render(<TerminalPane backend={backend} onActionsChange={onActionsChange} output="" preferences={preferences} session={activeSession} />)

  await onActionsChange.mock.calls.at(-1)?.[0].paste()
  fireEvent.click(await screen.findByRole('button', { name: '取消' }))
  expect(terminalMock.inputs).toEqual([])

  rerender(<TerminalPane backend={backend} onActionsChange={onActionsChange} output="" preferences={{ ...preferences, terminalWarnOnMultiLinePaste: false }} session={activeSession} />)
  await onActionsChange.mock.calls.at(-1)?.[0].paste()
  await waitFor(() => expect(terminalMock.inputs).toEqual(['echo first\recho second']))
  expect(screen.queryByRole('dialog', { name: '多行粘贴警告' })).not.toBeInTheDocument()
})

test('终端原生 paste 事件也经过多行警告', async () => {
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { readText: vi.fn(), writeText: vi.fn() } })
  const backend = {
    writeSSHSession: vi.fn().mockResolvedValue(undefined),
    resizeSSHSession: vi.fn().mockResolvedValue(undefined),
  } as unknown as Backend
  render(<TerminalPane backend={backend} output="" preferences={preferences} session={activeSession} />)

  fireEvent.paste(screen.getByLabelText('production-web SSH 终端'), {
    clipboardData: { getData: () => 'first\nsecond' },
  })

  expect(await screen.findByRole('dialog', { name: '多行粘贴警告' })).toBeInTheDocument()
  expect(terminalMock.inputs).toEqual([])
})

test('等待确认时断开 Session 会取消粘贴', async () => {
  const readText = vi.fn().mockResolvedValue('first\nsecond')
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { readText, writeText: vi.fn() } })
  const onActionsChange = vi.fn()
  const backend = {
    writeSSHSession: vi.fn().mockResolvedValue(undefined),
    resizeSSHSession: vi.fn().mockResolvedValue(undefined),
  } as unknown as Backend
  const { rerender } = render(<TerminalPane backend={backend} onActionsChange={onActionsChange} output="" preferences={preferences} session={activeSession} />)

  await onActionsChange.mock.calls.at(-1)?.[0].paste()
  expect(await screen.findByRole('dialog', { name: '多行粘贴警告' })).toBeInTheDocument()
  rerender(<TerminalPane backend={backend} onActionsChange={onActionsChange} output="" preferences={preferences} session={disconnectedSession} />)

  await waitFor(() => expect(screen.queryByRole('dialog', { name: '多行粘贴警告' })).not.toBeInTheDocument())
  expect(terminalMock.inputs).toEqual([])
})

test('上下文菜单按选区和连接状态提供复制、粘贴', async () => {
  const writeText = vi.fn().mockResolvedValue(undefined)
  const readText = vi.fn().mockResolvedValue('pwd')
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { readText, writeText } })
  terminalMock.selection = 'selected output'
  const backend = {
    writeSSHSession: vi.fn().mockResolvedValue(undefined),
    resizeSSHSession: vi.fn().mockResolvedValue(undefined),
  } as unknown as Backend
  const menuPreferences = { ...preferences, terminalRightClickAction: 'context_menu' as const }

  const { rerender } = render(<TerminalPane backend={backend} output="" preferences={menuPreferences} session={activeSession} />)
  fireEvent.contextMenu(screen.getByLabelText('production-web SSH 终端'))
  expect(screen.getByRole('menuitem', { name: '复制' })).toBeEnabled()
  expect(screen.getByRole('menuitem', { name: '粘贴' })).toBeEnabled()
  fireEvent.click(screen.getByRole('menuitem', { name: '复制' }))
  await waitFor(() => expect(writeText).toHaveBeenCalledWith('selected output'))

  rerender(<TerminalPane backend={backend} output="" preferences={menuPreferences} session={disconnectedSession} />)
  fireEvent.contextMenu(screen.getByLabelText('production-web SSH 终端'))
  expect(screen.getByRole('menuitem', { name: '复制' })).toBeEnabled()
  expect(screen.getByRole('menuitem', { name: '粘贴' })).toBeDisabled()
})

test('切换右键行为即时生效且不重建终端', () => {
  const backend = {
    writeSSHSession: vi.fn().mockResolvedValue(undefined),
    resizeSSHSession: vi.fn().mockResolvedValue(undefined),
  } as unknown as Backend
  const { rerender } = render(<TerminalPane backend={backend} output="" preferences={preferences} session={activeSession} />)

  rerender(<TerminalPane backend={backend} output="" preferences={{ ...preferences, terminalRightClickAction: 'context_menu' }} session={activeSession} />)
  fireEvent.contextMenu(screen.getByLabelText('production-web SSH 终端'))

  expect(screen.getByRole('menuitem', { name: '复制' })).toBeDisabled()
  expect(terminalMock.instances).toBe(1)
})

test('终端字号保持设置值，不跟随应用 UI 字体缩放', () => {
  const backend = {
    writeSSHSession: vi.fn().mockResolvedValue(undefined),
    resizeSSHSession: vi.fn().mockResolvedValue(undefined),
  } as unknown as Backend

  render(<TerminalPane backend={backend} output="" preferences={preferences} session={disconnectedSession} />)

  expect(terminalMock.options).toMatchObject({ fontFamily: 'JetBrains Mono', fontSize: 12 })
})

test('SSH 断开后不再把普通字符写入旧 Session', () => {
  const writeSSHSession = vi.fn().mockResolvedValue(undefined)
  const backend = {
    writeSSHSession,
    resizeSSHSession: vi.fn().mockResolvedValue(undefined),
  } as unknown as Backend

  render(<TerminalPane backend={backend} output="" preferences={preferences} session={disconnectedSession} />)
  act(() => terminalMock.dataHandler?.('x'))

  expect(writeSSHSession).not.toHaveBeenCalled()
})

test('SSH 断开后仅由无修饰键的 Enter 触发重连', () => {
  const onReconnect = vi.fn()
  const backend = {
    writeSSHSession: vi.fn().mockResolvedValue(undefined),
    resizeSSHSession: vi.fn().mockResolvedValue(undefined),
  } as unknown as Backend

  render(<TerminalPane backend={backend} onReconnect={onReconnect} output="" preferences={preferences} session={disconnectedSession} />)

  expect(terminalMock.keyHandler).toBeDefined()
  expect(terminalMock.keyHandler?.(new KeyboardEvent('keydown', { key: 'Enter' }))).toBe(false)
  expect(onReconnect).toHaveBeenCalledTimes(1)

  for (const modifier of ['ctrlKey', 'altKey', 'shiftKey', 'metaKey'] as const) {
    terminalMock.keyHandler?.(new KeyboardEvent('keydown', { key: 'Enter', [modifier]: true }))
  }
  terminalMock.keyHandler?.(new KeyboardEvent('keydown', { key: 'x' }))
  expect(onReconnect).toHaveBeenCalledTimes(1)
})

test('恢复的断连 Tab 没有 Session ID 时不发送 resize', () => {
  const resizeSSHSession = vi.fn().mockResolvedValue(undefined)
  const backend = {
    writeSSHSession: vi.fn().mockResolvedValue(undefined),
    resizeSSHSession,
  } as unknown as Backend

  render(<TerminalPane
    backend={backend}
    output=""
    preferences={preferences}
    session={{ ...disconnectedSession, id: '' }}
  />)
  act(() => terminalMock.resizeHandler?.({ cols: 100, rows: 30 }))

  expect(resizeSSHSession).not.toHaveBeenCalled()
})

test('连接建立期间也向远端同步最新终端尺寸', () => {
  const resizeSSHSession = vi.fn().mockResolvedValue(undefined)
  const backend = {
    writeSSHSession: vi.fn().mockResolvedValue(undefined),
    resizeSSHSession,
  } as unknown as Backend

  render(<TerminalPane
    backend={backend}
    output=""
    preferences={preferences}
    session={{ ...disconnectedSession, status: 'connecting' }}
  />)
  act(() => terminalMock.resizeHandler?.({ cols: 188, rows: 54 }))

  expect(resizeSSHSession).toHaveBeenCalledWith('session-1', 188, 54)
})

test('OSC 7 上报有效远程目录时只通知一次实际变化', () => {
  const onCurrentDirectoryChange = vi.fn()
  const backend = {
    writeSSHSession: vi.fn().mockResolvedValue(undefined),
    resizeSSHSession: vi.fn().mockResolvedValue(undefined),
  } as unknown as Backend

  render(<TerminalPane
    backend={backend}
    onCurrentDirectoryChange={onCurrentDirectoryChange}
    output=""
    preferences={preferences}
    session={activeSession}
  />)

  const reportDirectory = terminalMock.oscHandlers.get(7)
  expect(reportDirectory).toBeDefined()
  act(() => {
    reportDirectory?.('file://prod-web-01/srv/releases/current%20build')
    reportDirectory?.('file://prod-web-01/srv/releases/current%20build')
  })

  expect(onCurrentDirectoryChange).toHaveBeenCalledOnce()
  expect(onCurrentDirectoryChange).toHaveBeenCalledWith('/srv/releases/current build')
})

test('OSC 7 拒绝非文件 URI、相对路径和控制字符', () => {
  const onCurrentDirectoryChange = vi.fn()
  const backend = {
    writeSSHSession: vi.fn().mockResolvedValue(undefined),
    resizeSSHSession: vi.fn().mockResolvedValue(undefined),
  } as unknown as Backend

  render(<TerminalPane
    backend={backend}
    onCurrentDirectoryChange={onCurrentDirectoryChange}
    output=""
    preferences={preferences}
    session={activeSession}
  />)

  const reportDirectory = terminalMock.oscHandlers.get(7)
  act(() => {
    reportDirectory?.('https://prod-web-01/srv/app')
    reportDirectory?.('file://prod-web-01/../relative')
    reportDirectory?.('file://prod-web-01/srv/app%0aother')
  })

  expect(onCurrentDirectoryChange).not.toHaveBeenCalled()
})

test('重放终端历史时不把协议响应写回 SSH，完成后恢复正常输入', () => {
  const writeSSHSession = vi.fn().mockResolvedValue(undefined)
  const backend = {
    writeSSHSession,
    resizeSSHSession: vi.fn().mockResolvedValue(undefined),
  } as unknown as Backend
  terminalMock.writeResponse = '\x1b[>0;276;0c'

  render(<TerminalPane
    backend={backend}
    output="\x1b[>c"
    preferences={preferences}
    session={activeSession}
  />)

  expect(writeSSHSession).not.toHaveBeenCalled()
  expect(terminalMock.writeCallbacks).toHaveLength(1)

  act(() => terminalMock.writeCallbacks[0]())
  act(() => terminalMock.dataHandler?.('pwd\r'))

  expect(writeSSHSession).toHaveBeenCalledOnce()
  expect(writeSSHSession).toHaveBeenCalledWith('session-1', 'pwd\r')
})
