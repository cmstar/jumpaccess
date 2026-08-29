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
})
