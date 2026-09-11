import { expect, test, vi } from 'vitest'
import type { Terminal } from '@xterm/xterm'
import { TerminalFitAddon } from './TerminalFitAddon'

vi.mock('@xterm/addon-fit', () => ({ FitAddon: class {
  activate() {}
  proposeDimensions() { return { cols: 98, rows: 20 } }
} }))

test('隐藏整个滚动条后回收预留宽度，重新开启时恢复原有列数', () => {
  const host = document.createElement('div')
  host.style.width = '800px'
  const element = document.createElement('div')
  element.style.padding = '0px'
  const screen = document.createElement('div')
  screen.className = 'xterm-screen'
  screen.getBoundingClientRect = () => ({ width: 784 }) as DOMRect
  element.append(screen)
  host.append(element)
  document.body.append(host)
  const terminal = { element, cols: 98, options: { scrollback: 10000 } } as unknown as Terminal
  const addon = new TerminalFitAddon()
  addon.activate(terminal)
  try {
    host.dataset.terminalShowScrollbar = 'true'
    expect(addon.proposeDimensions()).toEqual({ cols: 98, rows: 20 })
    host.dataset.terminalShowScrollbar = 'false'
    expect(addon.proposeDimensions()).toEqual({ cols: 100, rows: 20 })
    expect(terminal.options.scrollback).toBe(10000)
    host.dataset.terminalShowScrollbar = 'true'
    expect(addon.proposeDimensions()).toEqual({ cols: 98, rows: 20 })
  } finally { host.remove() }
})
