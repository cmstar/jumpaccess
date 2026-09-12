import { act, fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'
import { NotificationProvider, useNotifications } from './Notifications'

afterEach(() => vi.useRealTimers())

function Actions() {
  const { showInfo, showWarning, showError } = useNotifications()
  return <><button onClick={() => showInfo('同步成功')}>通知</button><button onClick={() => showWarning('请选择组织')}>警告</button><button onClick={() => showError('同步失败')}>错误</button></>
}

test('普通消息 3 秒、警告 5 秒后消失，错误保留到手动关闭；新消息不覆盖旧消息', () => {
  vi.useFakeTimers()
  render(<NotificationProvider><Actions /></NotificationProvider>)
  for (const name of ['通知', '警告', '错误']) fireEvent.click(screen.getByRole('button', { name }))
  expect(screen.getAllByRole('status')).toHaveLength(2)
  act(() => vi.advanceTimersByTime(3000))
  expect(screen.queryByText('同步成功')).not.toBeInTheDocument()
  expect(screen.getByText('请选择组织')).toBeInTheDocument()
  act(() => vi.advanceTimersByTime(2000))
  expect(screen.queryByText('请选择组织')).not.toBeInTheDocument()
  act(() => vi.advanceTimersByTime(60000))
  fireEvent.click(within(screen.getByRole('alert')).getByRole('button', { name: '关闭错误提示' }))
  expect(screen.queryByRole('alert')).not.toBeInTheDocument()
})

test('重复提示合并，悬停或键盘聚焦时暂停自动关闭，卸载清理计时器', () => {
  vi.useFakeTimers()
  const view = render(<NotificationProvider><Actions /></NotificationProvider>)
  fireEvent.click(screen.getByRole('button', { name: '通知' }))
  fireEvent.click(screen.getByRole('button', { name: '通知' }))
  expect(screen.getAllByRole('status')).toHaveLength(1)
  fireEvent.mouseEnter(screen.getByRole('status'))
  act(() => vi.advanceTimersByTime(10000))
  expect(screen.getByRole('status')).toBeInTheDocument()
  fireEvent.mouseLeave(screen.getByRole('status'))
  fireEvent.focus(within(screen.getByRole('status')).getByRole('button'))
  act(() => vi.advanceTimersByTime(10000))
  expect(screen.getByRole('status')).toBeInTheDocument()
  fireEvent.blur(within(screen.getByRole('status')).getByRole('button'))
  act(() => vi.advanceTimersByTime(3000))
  expect(screen.queryByRole('status')).not.toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '警告' }))
  view.unmount()
  expect(vi.getTimerCount()).toBe(0)
})
