import { act, render } from '@testing-library/react'
import { beforeEach, expect, test, vi } from 'vitest'

import { TerminalPane } from './TerminalPane'
import type { Backend, Preferences, SessionState } from '../lib/backend'

const terminalMock = vi.hoisted(() => ({
  dataHandler: undefined as ((data: string) => void) | undefined,
  keyHandler: undefined as ((event: KeyboardEvent) => boolean) | undefined,
  options: undefined as { fontFamily?: string; fontSize?: number } | undefined,
  oscHandlers: new Map<number, (data: string) => boolean | Promise<boolean>>(),
  resizeHandler: undefined as ((size: { cols: number; rows: number }) => void) | undefined,
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
    constructor(options: { fontFamily?: string; fontSize?: number }) { terminalMock.options = options }
    loadAddon() {}
    open() {}
    write(_data: string, callback?: () => void) {
      if (terminalMock.writeResponse) terminalMock.dataHandler?.(terminalMock.writeResponse)
      if (callback) terminalMock.writeCallbacks.push(callback)
    }
    reset() {}
    focus() {}
    dispose() {}
    onData(handler: (data: string) => void) {
      terminalMock.dataHandler = handler
      return { dispose() {} }
    }
    onResize(handler: (size: { cols: number; rows: number }) => void) {
      terminalMock.resizeHandler = handler
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
  version: 1,
  theme: 'light',
  terminalFontFamily: 'JetBrains Mono',
  terminalFontSize: 12,
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
  terminalMock.oscHandlers.clear()
  terminalMock.resizeHandler = undefined
  terminalMock.writeCallbacks = []
  terminalMock.writeResponse = ''
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
