import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import { Terminal } from '@xterm/xterm'
import { terminalDisplayOptions } from '../model/terminalTheme'
import { TerminalFitAddon } from './TerminalFitAddon'

// xterm 导入时探测颜色解析能力；本测试只替身布局与 Canvas 度量。
vi.hoisted(() => vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue(null))

const webkit = 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko)'
const terminals: Terminal[] = []
let hidden = false

beforeEach(() => {
  hidden = false
  vi.spyOn(window, 'matchMedia').mockReturnValue({ matches: false, addListener() {}, removeListener() {}, addEventListener() {}, removeEventListener() {} } as unknown as MediaQueryList)
  vi.spyOn(navigator, 'platform', 'get').mockReturnValue('MacIntel')
  vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue(webkit)
  // 复现反馈中的两条测量路径：Canvas 16.99px，DOM 11.16px。
  vi.stubGlobal('OffscreenCanvas', class {
    getContext() {
      return { font: '', measureText: () => ({ width: 16.99, fontBoundingBoxAscent: 18, fontBoundingBoxDescent: 4 }) }
    }
  })
  vi.spyOn(HTMLElement.prototype, 'offsetWidth', 'get').mockImplementation(function (this: HTMLElement) {
    if (hidden || !this.classList.contains('xterm-char-measure-element')) return 0
    return Math.round((this.textContent?.length ?? 0) * 11.16 * (parseFloat(this.style.fontSize) || 18) / 18)
  })
  vi.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockImplementation(function (this: HTMLElement) {
    return hidden ? 0 : Math.round(22 * (parseFloat(this.style.fontSize) || 18) / 18)
  })
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
    return { width: parseFloat(this.style.width) || 0, height: parseFloat(this.style.height) || 0, x: 0, y: 0, top: 0, left: 0, right: 0, bottom: 0, toJSON() {} }
  })
})

afterEach(() => {
  for (const terminal of terminals.splice(0)) terminal.dispose()
  document.body.replaceChildren()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

function openTerminal(font = '0xProto Nerd Font Mono') {
  const host = document.createElement('div')
  host.style.cssText = 'width:800px;height:400px;--terminal-scrollbar-width:7px'
  document.body.append(host)
  const options = terminalDisplayOptions({ terminalFontFamily: font, terminalFontSize: 18, terminalColorScheme: 'nord', terminalLineHeight: 1.25, terminalCursorStyle: 'block', terminalCursorBlink: false })
  const terminal = new Terminal({ ...options, cols: 80, rows: 12, scrollback: 100 })
  terminals.push(terminal)
  const fit = new TerminalFitAddon()
  terminal.loadAddon(fit)
  terminal.open(host)
  terminal.element!.style.padding = '0px'
  const screen = terminal.element!.querySelector<HTMLElement>('.xterm-screen')!
  const cellWidth = () => parseFloat(screen.style.width) / terminal.cols
  return { terminal, fit, host, screen, cellWidth }
}

test.each(['0xProto Nerd Font Mono', '"0xProto Nerd Font Mono"', "'0xProto Nerd Font Mono'", 'JetBrains Mono'])(
  'macOS WKWebView 用 DOM 度量 %s，修正真实 xterm 的网格和上报列数', font => {
    const { terminal, fit, cellWidth } = openTerminal(font)
    expect(cellWidth()).toBeGreaterThan(16)
    const resized = vi.fn()
    terminal.onResize(resized)
    fit.fit()
    expect(cellWidth()).toBeCloseTo(11.16, 1)
    expect(terminal.cols).toBe(71)
    expect(resized).toHaveBeenLastCalledWith(expect.objectContaining({ cols: 71, rows: 14 }))
  },
)

test('修改字体、字号和行高后继续使用 DOM 度量，保留终端输出', async () => {
  const { terminal, fit, cellWidth } = openTerminal()
  fit.fit()
  await new Promise<void>(resolve => terminal.write('history', resolve))
  terminal.options.fontFamily = 'Menlo'
  terminal.options.fontSize = 24
  terminal.options.lineHeight = 1.5
  fit.fit()
  expect(cellWidth()).toBeCloseTo(14.875, 1)
  expect(terminal.cols).toBe(53)
  expect(terminal.rows).toBe(9)
  const probe = terminal.element!.querySelector<HTMLElement>('.terminal-dom-char-measure')!
  expect(probe.style.fontFamily).toBe('Menlo')
  expect(probe.style.fontSize).toBe('24px')
  expect(probe.style.fontWeight).toBe('normal')
  expect(probe.style.lineHeight).toBe('normal')
  expect(terminal.buffer.active.getLine(0)?.translateToString(true)).toBe('history')
})

test('隐藏时不采用错误 Canvas 尺寸，恢复显示和隐藏期间改字号后重新测量', () => {
  hidden = true
  const { terminal, fit, cellWidth } = openTerminal()
  fit.fit()
  expect(terminal.cols).toBe(80)
  hidden = false
  fit.fit()
  expect(cellWidth()).toBeCloseTo(11.16, 1)
  hidden = true
  terminal.options.fontSize = 24
  hidden = false
  fit.fit()
  expect(cellWidth()).toBeCloseTo(14.875, 1)
  expect(terminal.cols).toBe(53)
})

test.each([
  ['Win32', 'Mozilla/5.0 (Windows NT 10.0) AppleWebKit/537.36 Chrome/140.0.0.0 Safari/537.36'],
  ['MacIntel', 'Mozilla/5.0 (Macintosh) AppleWebKit/537.36 Chrome/140.0.0.0 Safari/537.36'],
  ['MacIntel', 'Mozilla/5.0 (Macintosh) Gecko/20100101 Firefox/140.0'],
])('%s / %s 沿用 xterm 原生测量', (platform, userAgent) => {
  vi.spyOn(navigator, 'platform', 'get').mockReturnValue(platform)
  vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue(userAgent)
  const { fit, cellWidth } = openTerminal()
  fit.fit()
  expect(cellWidth()).toBeGreaterThan(16)
  expect(document.querySelector('.terminal-dom-char-measure')).toBeNull()
})

test('Safari 同样使用 DOM，重复 fit 只保留一个测量元素，卸载时清理且不改全局 Canvas', () => {
  vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue(`${webkit} Version/26.0 Safari/605.1.15`)
  const canvas = OffscreenCanvas
  const { terminal, fit, cellWidth } = openTerminal()
  fit.fit()
  fit.fit()
  expect(cellWidth()).toBeCloseTo(11.16, 1)
  expect(terminal.element!.querySelectorAll('.terminal-dom-char-measure')).toHaveLength(1)
  fit.dispose()
  expect(terminal.element!.querySelector('.terminal-dom-char-measure')).toBeNull()
  terminal.options.fontSize = 20
  expect(cellWidth()).toBeGreaterThan(16)
  expect(OffscreenCanvas).toBe(canvas)
})
