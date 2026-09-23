import { act, fireEvent, renderHook, waitFor } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'
import type { Backend } from '../lib/backend'
import { useTerminalFullscreen } from './useTerminalFullscreen'

afterEach(() => vi.unstubAllGlobals())

function setup(setWindowFullscreen = vi.fn().mockResolvedValue(undefined)) {
  const onError = vi.fn()
  const onEnter = vi.fn()
  const backend = { setWindowFullscreen } as unknown as Backend
  const hook = renderHook(({ tab }) => useTerminalFullscreen(backend, tab, onError, onEnter), { initialProps: { tab: { id: 'ssh-1', kind: 'ssh' } } })
  return { ...hook, setWindowFullscreen, onError, onEnter }
}

test('全屏切换等待完成，重复请求不重入；失败后仍能重试', async () => {
  let done!: () => void
  const set = vi.fn().mockImplementationOnce(() => new Promise<void>(resolve => { done = resolve })).mockResolvedValue(undefined)
  const hook = setup(set)
  fireEvent.keyDown(window, { key: 'F11' })
  fireEvent.keyDown(window, { key: 'F11' })
  expect(set).toHaveBeenCalledTimes(1)
  expect(hook.result.current.fullscreen).toBe(false)
  await act(async () => done())
  expect(hook.result.current.fullscreen).toBe(true)
  set.mockRejectedValueOnce(new Error('native failure'))
  await act(async () => { await hook.result.current.exit() })
  expect(hook.result.current.fullscreen).toBe(true)
  expect(hook.onError).toHaveBeenCalledWith('native failure')
  await act(async () => { await hook.result.current.exit() })
  expect(hook.result.current.fullscreen).toBe(false)
})

test('已拦截的 Alt+Enter 后续字符事件及松开 Alt 后的长按不漏到终端', async () => {
  const hook = setup()
  const input = document.createElement('textarea')
  document.body.append(input)
  const remote = vi.fn()
  input.addEventListener('keydown', remote)
  input.addEventListener('keypress', remote)
  try {
    fireEvent.keyDown(input, { key: 'Enter', code: 'Enter', altKey: true })
    await waitFor(() => expect(hook.result.current.fullscreen).toBe(true))
    fireEvent.keyPress(input, { key: 'Enter', code: 'Enter' })
    fireEvent.keyDown(input, { key: 'Enter', code: 'Enter', repeat: true })
    fireEvent.keyUp(input, { key: 'Enter', code: 'Enter' })
    expect(remote).not.toHaveBeenCalled()
    expect(hook.setWindowFullscreen).toHaveBeenCalledTimes(1)
    fireEvent.keyDown(input, { key: 'Enter', code: 'Enter' })
    expect(remote).toHaveBeenCalledTimes(1)
  } finally { input.remove() }
})

test('额外修饰键、输入法及 macOS 的 Alt+Enter 保持原行为', async () => {
  vi.stubGlobal('navigator', { platform: 'MacIntel' })
  const hook = setup()
  for (const event of [{ key: 'F11', ctrlKey: true }, { key: 'F11', shiftKey: true }, { key: 'F11', isComposing: true }, { key: 'F11', keyCode: 229 }, { key: 'Enter', altKey: true }]) {
    expect(fireEvent.keyDown(window, event)).toBe(true)
  }
  expect(hook.setWindowFullscreen).not.toHaveBeenCalled()
})

test('弹窗内不进入全屏，已有全屏可退出；切换目标自动恢复窗口', async () => {
  const hook = setup()
  const modal = document.createElement('div')
  modal.setAttribute('role', 'dialog')
  document.body.append(modal)
  fireEvent.keyDown(modal, { key: 'F11' })
  expect(hook.setWindowFullscreen).not.toHaveBeenCalled()
  modal.remove()
  fireEvent.keyDown(window, { key: 'F11' })
  await waitFor(() => expect(hook.result.current.fullscreen).toBe(true))
  hook.rerender({ tab: { id: 'ssh-2', kind: 'ssh' } })
  await waitFor(() => expect(hook.result.current.fullscreen).toBe(false))
})

test('系统退出全屏同步应用状态，最小化不误判为退出', async () => {
  const runtime = { EventsOnMultiple: vi.fn(), WindowIsFullscreen: vi.fn().mockResolvedValue(false), WindowIsMinimised: vi.fn().mockResolvedValue(true) }
  vi.stubGlobal('runtime', runtime)
  const hook = setup()
  fireEvent.keyDown(window, { key: 'F11' })
  await waitFor(() => expect(hook.result.current.fullscreen).toBe(true))
  fireEvent.resize(window)
  await waitFor(() => expect(runtime.WindowIsFullscreen).toHaveBeenCalled())
  expect(hook.result.current.fullscreen).toBe(true)
  runtime.WindowIsMinimised.mockResolvedValue(false)
  fireEvent.focus(window)
  await waitFor(() => expect(hook.result.current.fullscreen).toBe(false))
})
