import { defaultTerminalBackground } from '../model/terminalBackground'
import { render, screen } from '@testing-library/react'
import { beforeEach, expect, test, vi } from 'vitest'
import type { ITerminalOptions } from '@xterm/xterm'
import type { Preferences } from '../lib/backend'
import { TerminalPreview } from './TerminalPreview'

const mock = vi.hoisted(() => ({ options: [] as ITerminalOptions[], write: vi.fn(), focus: vi.fn(), fit: vi.fn(), dispose: vi.fn() }))

vi.mock('@xterm/xterm', () => ({
  Terminal: class {
    constructor(public options: ITerminalOptions) { mock.options.push(options) }
    open() {}
    loadAddon() {}
    write = mock.write
    focus = mock.focus
    dispose = mock.dispose
  },
}))
vi.mock('@xterm/addon-fit', () => ({ FitAddon: class { fit = mock.fit } }))

const preferences: Preferences = {
  terminalBackground: { ...defaultTerminalBackground },
  version: 6, theme: 'light', terminalColorScheme: 'nord', terminalFontFamily: 'monospace', terminalFontSize: 12,
  terminalLineHeight: 1, terminalCursorStyle: 'block', terminalCursorBlink: true,
  terminalScrollbarVisibility: 'active',
  terminalShowStatusBar: true,
  terminalRightClickAction: 'paste', terminalWarnOnMultiLinePaste: true, terminalCopyOnEnter: true, downloadMode: 'ask', downloadDirectory: '', confirmCloseActiveSession: true, showTabCloseButtons: true, newTabPosition: 'end',
}

beforeEach(() => { mock.options.length = 0; vi.clearAllMocks() })

test.each(['终端预览', '背景图预览'])('%s 保留超过一屏的示例历史，以便展示和操作滚动条', label => {
  render(<TerminalPreview preferences={preferences} label={label} />)
  expect(mock.options[0].scrollback).toBeGreaterThan(0)
  // 默认 xterm 为 24 行；即使隐藏面板尚未完成尺寸测量，也应有可滚动历史。
  expect(mock.write.mock.calls[0][0].split('\r\n').length).toBeGreaterThan(24)
  expect(mock.options[0].disableStdin).toBe(true)
  expect(mock.focus).not.toHaveBeenCalled()
})

test('预览同步滚动条开关且不重建终端', () => {
  const view = (terminalScrollbarVisibility: 'always' | 'active' | 'hidden') => <TerminalPreview preferences={{ ...preferences, terminalScrollbarVisibility }} />
  const { rerender } = render(view('active'))
  const host = screen.getByRole('region', { name: '终端预览' }).querySelector('.terminal-preview-host')!
  expect(host).toHaveAttribute('data-terminal-scrollbar-visibility', 'active')
  mock.fit.mockClear()
  rerender(view('hidden'))
  expect(host).toHaveAttribute('data-terminal-scrollbar-visibility', 'hidden')
  expect(mock.fit).toHaveBeenCalled()
  rerender(view('active'))
  expect(host).toHaveAttribute('data-terminal-scrollbar-visibility', 'active')
  expect(mock.options).toHaveLength(1)
  expect(mock.write).toHaveBeenCalledTimes(1)
})

test.each(['block', 'bar', 'underline'] as const)('预览初始使用 %s 光标并尊重关闭闪烁', (style) => {
  render(<TerminalPreview preferences={{ ...preferences, terminalCursorStyle: style, terminalCursorBlink: false }} />)
  expect(mock.options[0]).toMatchObject({ cursorStyle: style, cursorInactiveStyle: style, cursorBlink: false, disableStdin: true })
  expect(mock.write).toHaveBeenCalledWith(expect.stringMatching(/^\x1b\[\?1049h\x1b\[\?1049l/))
  expect(mock.focus).not.toHaveBeenCalled()
})

test('预览独立更新行高、光标与闪烁，不重建实例、不重放文本、不抢焦点', () => {
  const { rerender, unmount } = render(<TerminalPreview preferences={preferences} />)
  const host = screen.getByRole('region', { name: '终端预览' }).querySelector('.terminal-preview-host')!
  const changed = { ...preferences, terminalLineHeight: 1.5 }
  mock.fit.mockClear()
  rerender(<TerminalPreview preferences={changed} />)
  expect(mock.options[0].lineHeight).toBe(1.5)
  expect(host).toHaveStyle({ height: '216px' })
  expect(mock.fit).toHaveBeenCalled()
  rerender(<TerminalPreview preferences={{ ...changed, terminalCursorStyle: 'bar' }} />)
  expect(mock.options[0]).toMatchObject({ cursorStyle: 'bar', cursorInactiveStyle: 'bar' })
  rerender(<TerminalPreview preferences={{ ...changed, terminalCursorStyle: 'bar', terminalCursorBlink: false }} />)
  expect(mock.options[0].cursorBlink).toBe(false)
  expect(mock.options).toHaveLength(1)
  expect(mock.write).toHaveBeenCalledTimes(1)
  expect(mock.focus).not.toHaveBeenCalled()
  unmount()
  expect(mock.dispose).toHaveBeenCalledTimes(1)
})

test('底部方块在预览中使用映射样式，切回普通下划线移除定制标记', () => {
  const { rerender } = render(<TerminalPreview preferences={{ ...preferences, terminalCursorStyle: 'quarter_block' }} />)
  const host = screen.getByRole('region', { name: '终端预览' }).querySelector('.terminal-preview-host')!
  expect(mock.options[0]).toMatchObject({ cursorStyle: 'underline', cursorInactiveStyle: 'underline', cursorBlink: true })
  expect(host).toHaveAttribute('data-terminal-cursor-style', 'quarter_block')
  rerender(<TerminalPreview preferences={{ ...preferences, terminalCursorStyle: 'underline' }} />)
  expect(host).toHaveAttribute('data-terminal-cursor-style', 'underline')
  expect(mock.options).toHaveLength(1)
})
