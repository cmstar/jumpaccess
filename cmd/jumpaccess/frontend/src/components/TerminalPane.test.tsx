import { act, render } from '@testing-library/react'
import { expect, test, vi } from 'vitest'

import { TerminalPane } from './TerminalPane'
import type { Backend, Preferences, SessionState } from '../lib/backend'

const terminalMock = vi.hoisted(() => ({
  dataHandler: undefined as ((data: string) => void) | undefined,
  keyHandler: undefined as ((event: KeyboardEvent) => boolean) | undefined,
  resizeHandler: undefined as ((size: { cols: number; rows: number }) => void) | undefined,
}))

vi.mock('@xterm/xterm', () => ({
  Terminal: class {
    cols = 120
    rows = 34
    loadAddon() {}
    open() {}
    write() {}
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
