import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, expect, test, vi } from 'vitest'

import { TerminalPane } from './TerminalPane'
import type { Backend, Preferences, SessionState } from '../lib/backend'

const terminalMock = vi.hoisted(() => ({
  dataHandler: undefined as ((data: string) => void) | undefined,
  keyHandler: undefined as ((event: KeyboardEvent) => boolean) | undefined,
  instances: 0,
  options: undefined as { fontFamily?: string; fontSize?: number } | undefined,
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
    constructor(options: { fontFamily?: string; fontSize?: number }) { terminalMock.instances += 1; terminalMock.options = options }
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
  version: 4,
  theme: 'light',
  terminalFontFamily: 'JetBrains Mono',
  terminalFontSize: 12,
  terminalRightClickAction: 'paste',
  terminalWarnOnMultiLinePaste: true,
  confirmCloseActiveSession: true,
  showTabCloseButtons: true,
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
