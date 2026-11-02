import { fireEvent, renderHook } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'
import { useTabShortcuts } from './useTabShortcuts'

afterEach(() => vi.unstubAllGlobals())

test.each(['Win32', 'MacIntel'])('%s 使用 Control 双向切换，支持连续按键和长按', platform => {
  vi.stubGlobal('navigator', { platform })
  const cycle = vi.fn()
  renderHook(() => useTabShortcuts(cycle))
  fireEvent.keyDown(window, { key: 'Tab', ctrlKey: true })
  fireEvent.keyDown(window, { key: 'Tab', ctrlKey: true, repeat: true })
  fireEvent.keyUp(window, { key: 'Tab', ctrlKey: true })
  fireEvent.keyDown(window, { key: 'Tab', ctrlKey: true, shiftKey: true })
  expect(cycle.mock.calls).toEqual([[1], [1], [-1]])
})

test('捕获终端按键的整个序列，松开 Control 后的残余重复不发送到远端', () => {
  const cycle = vi.fn()
  renderHook(() => useTabShortcuts(cycle))
  const input = document.createElement('textarea')
  document.body.append(input)
  const remote = vi.fn()
  for (const type of ['keydown', 'keypress', 'keyup']) input.addEventListener(type, remote)
  try {
    expect(fireEvent.keyDown(input, { key: 'Tab', ctrlKey: true })).toBe(false)
    fireEvent.keyPress(input, { key: 'Tab' })
    fireEvent.keyDown(input, { key: 'Tab', repeat: true })
    fireEvent.keyUp(input, { key: 'Tab' })
    expect(remote).not.toHaveBeenCalled()
    expect(cycle).toHaveBeenCalledTimes(1)
    expect(fireEvent.keyDown(input, { key: 'Tab' })).toBe(true)
    expect(fireEvent.keyDown(input, { key: 'Tab', shiftKey: true })).toBe(true)
    expect(remote).toHaveBeenCalledTimes(2)
  } finally { input.remove() }
})

test('弹窗内消费组合键但不切换；普通 Tab、其他修饰键和输入法不受影响', () => {
  const cycle = vi.fn()
  renderHook(() => useTabShortcuts(cycle))
  const modal = document.createElement('section')
  modal.setAttribute('role', 'dialog')
  document.body.append(modal)
  try {
    expect(fireEvent.keyDown(modal, { key: 'Tab', ctrlKey: true })).toBe(false)
    expect(cycle).not.toHaveBeenCalled()
    fireEvent.keyUp(modal, { key: 'Tab' })
    expect(fireEvent.keyDown(modal, { key: 'Tab' })).toBe(true)
  } finally { modal.remove() }
  for (const init of [{}, { shiftKey: true }, { metaKey: true }, { ctrlKey: true, altKey: true }, { ctrlKey: true, metaKey: true }, { ctrlKey: true, isComposing: true }, { ctrlKey: true, keyCode: 229 }]) {
    expect(fireEvent.keyDown(window, { key: 'Tab', ...init })).toBe(true)
  }
  expect(cycle).not.toHaveBeenCalled()
})

test('失焦清理按住状态，更新回调与卸载不遗留监听', () => {
  const first = vi.fn()
  const next = vi.fn()
  const hook = renderHook(({ cycle }) => useTabShortcuts(cycle), { initialProps: { cycle: first } })
  fireEvent.keyDown(window, { key: 'Tab', ctrlKey: true })
  fireEvent.blur(window)
  expect(fireEvent.keyDown(window, { key: 'Tab', repeat: true })).toBe(true)
  hook.rerender({ cycle: next })
  fireEvent.keyDown(window, { key: 'Tab', ctrlKey: true })
  expect(first).toHaveBeenCalledTimes(1)
  expect(next).toHaveBeenCalledTimes(1)
  hook.unmount()
  expect(fireEvent.keyDown(window, { key: 'Tab', ctrlKey: true })).toBe(true)
  expect(next).toHaveBeenCalledTimes(1)
})
