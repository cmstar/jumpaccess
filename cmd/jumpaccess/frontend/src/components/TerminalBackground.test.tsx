import { act, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'
import type { Backend } from '../lib/backend'
import { defaultTerminalBackground } from '../model/terminalBackground'
import { TerminalBackgroundProvider, TerminalBackgroundSurface, useTerminalBackground } from './TerminalBackground'

afterEach(() => vi.unstubAllGlobals())
function Status() { const state = useTerminalBackground(); return <output>{state.error || state.image?.path || '纯色'}</output> }

test('多个终端共用图片，调整透明度不重读，读取失败及禁用立即回退，迟到图片不会复活', async () => {
  const images: Array<{ onload: null | (() => void); onerror: null | (() => void) }> = []
  vi.stubGlobal('Image', class { onload = null; onerror = null; naturalWidth = 800; naturalHeight = 600; src = ''; constructor() { images.push(this) } })
  let finish!: (url: string) => void
  const read = vi.fn().mockResolvedValueOnce('data:image/png;base64,test').mockRejectedValueOnce(new Error('文件不存在')).mockImplementationOnce(() => new Promise<string>(resolve => { finish = resolve }))
  const backend = { readTerminalBackground: read } as unknown as Backend
  const settings = { ...defaultTerminalBackground, enabled: true, filePath: 'a.png' }
  const view = (value = settings) => <TerminalBackgroundProvider backend={backend} settings={value}><Status /><TerminalBackgroundSurface className="first"><span>文字</span></TerminalBackgroundSurface><TerminalBackgroundSurface className="second">终端</TerminalBackgroundSurface></TerminalBackgroundProvider>
  const { rerender, container } = render(view())
  await waitFor(() => expect(images[0].onload).not.toBeNull())
  act(() => images[0].onload?.())
  expect(screen.getByText('a.png')).toBeInTheDocument()
  expect(container.querySelectorAll('[data-background-visible="true"]')).toHaveLength(2)
  expect(container.querySelector('.terminal-background-image')).toHaveStyle({ opacity: '0.30000000000000004' })
  expect(screen.getByText('文字').style.opacity).toBe('')
  rerender(view({ ...settings, transparencyPercent: 80 }))
  expect(read).toHaveBeenCalledTimes(1)
  rerender(view({ ...settings, filePath: 'missing.png' }))
  await screen.findByText(/文件不存在/)
  expect(container.querySelectorAll('.terminal-background-image')).toHaveLength(0)
  rerender(view({ ...settings, filePath: 'late.png' }))
  rerender(view({ ...settings, enabled: false }))
  await act(async () => finish('data:image/png;base64,late'))
  expect(screen.getByText('纯色')).toBeInTheDocument()
  expect(container.querySelectorAll('.terminal-background-image')).toHaveLength(0)
})
