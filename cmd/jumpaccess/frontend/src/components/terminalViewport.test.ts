import { describe, expect, it } from 'vitest'

import { synchronizeTerminalViewportBackground } from './terminalViewport'

describe('synchronizeTerminalViewportBackground', () => {
  it.each(['#f7faf8', '#101817'])('covers xterm viewport gaps with %s', (background) => {
    const host = document.createElement('div')
    const viewport = document.createElement('div')
    const expected = document.createElement('div')
    viewport.className = 'xterm-viewport'
    host.append(viewport)
    expected.style.backgroundColor = background

    synchronizeTerminalViewportBackground(host, background)

    expect(host.style.backgroundColor).toBe(expected.style.backgroundColor)
    expect(viewport.style.backgroundColor).toBe(expected.style.backgroundColor)
  })

  it('启用背景图时滚动条槽底仍使用方案原本的不透明背景色', () => {
    const host = document.createElement('div')
    synchronizeTerminalViewportBackground(host, '#2e344000')
    expect(host.style.getPropertyValue('--terminal-scrollbar-track')).toBe('#2e3440')
  })
})
