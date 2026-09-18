import { act, fireEvent, render, screen } from '@testing-library/react'
import { expect, test, vi } from 'vitest'
import { SSHTransferProgress } from './SSHTransferProgress'
import type { ZmodemState } from '../lib/zmodemTypes'

const base: ZmodemState = { checked: true, upload: true, download: true, busy: false }

test('单行截断的状态和文件名可通过悬停查看完整内容', () => {
  const message = '传输失败：远程服务器返回了一条很长的错误详情'
  const name = '一份包含完整说明且名称较长的下载文件.zip'
  render(<SSHTransferProgress state={{ ...base, phase: 'failed', message, name, size: 1024, transferred: 512 }} onCancel={() => {}} />)
  expect(screen.getByRole('status')).toHaveAttribute('title', message)
  expect(screen.getByText(name)).toHaveAttribute('title', name)
  expect(screen.getByRole('progressbar').nextElementSibling).toHaveTextContent('50% · 512 B / 1 KB')
})

test('空闲隐藏，等待时显示不确定进度并可取消', () => {
  const cancel = vi.fn()
  const { rerender } = render(<SSHTransferProgress state={base} onCancel={cancel} />)
  expect(screen.queryByRole('region')).toBeNull()
  rerender(<SSHTransferProgress state={{ ...base, busy: true, direction: 'upload', phase: 'waiting', message: '等待远程传输开始' }} onCancel={cancel} />)
  expect(screen.getByText('上传')).toBeInTheDocument()
  expect(screen.getByRole('progressbar')).not.toHaveAttribute('value')
  fireEvent.click(screen.getByRole('button', { name: '取消传输' }))
  expect(cancel).toHaveBeenCalledOnce()
})

test('按当前文件计数，保存与确认前不显示 100%，最终处理禁用取消', () => {
  const state: ZmodemState = { ...base, busy: true, direction: 'download', phase: 'transferring', name: 'a.zip', size: 1024, transferred: 512 }
  const { rerender } = render(<SSHTransferProgress state={state} onCancel={() => {}} />)
  expect(screen.getByRole('progressbar')).toHaveAttribute('value', '50')
  expect(screen.getByText(/512 B \/ 1 KB/)).toBeInTheDocument()
  for (const phase of ['saving', 'confirming', 'finishing'] as const) {
    rerender(<SSHTransferProgress state={{ ...state, phase, transferred: 1024 }} onCancel={() => {}} />)
    expect(screen.getByRole('progressbar')).toHaveAttribute('value', '99')
    expect(screen.queryByText(/100%/)).toBeNull()
    expect(screen.getByRole('button', { name: '取消传输' })).toBeDisabled()
  }
})

test('空文件完成后显示 100%，完成结果自动收起，失败保留到关闭且新传输重新显示', () => {
  vi.useFakeTimers()
  try {
    const state: ZmodemState = { ...base, phase: 'completed', finishedAt: Date.now(), name: 'empty.txt', size: 0, transferred: 0, message: '下载完成' }
    const { rerender } = render(<SSHTransferProgress state={state} onCancel={() => {}} />)
    expect(screen.getByRole('progressbar')).toHaveAttribute('value', '100')
    expect(screen.queryByRole('button', { name: '取消传输' })).toBeNull()
    act(() => { vi.advanceTimersByTime(4000) })
    expect(screen.queryByRole('region')).toBeNull()
    rerender(<SSHTransferProgress state={{ ...state, phase: 'failed', message: '传输失败：磁盘已满' }} onCancel={() => {}} />)
    act(() => { vi.advanceTimersByTime(10000) })
    expect(screen.getByText('传输失败：磁盘已满')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '关闭传输提示' }))
    expect(screen.queryByRole('region')).toBeNull()
    rerender(<SSHTransferProgress state={{ ...base, busy: true, phase: 'waiting' }} onCancel={() => {}} />)
    expect(screen.getByRole('region')).toBeInTheDocument()
  } finally { vi.useRealTimers() }
})

test('切换 Tab 后恢复活动进度，过期的取消结果不再出现', () => {
  const state: ZmodemState = { ...base, busy: true, direction: 'upload', phase: 'transferring', name: 'a.zip', size: 100, transferred: 25 }
  const first = render(<SSHTransferProgress state={state} onCancel={() => {}} />)
  first.unmount()
  const second = render(<SSHTransferProgress state={state} onCancel={() => {}} />)
  expect(screen.getByRole('progressbar')).toHaveAttribute('value', '25')
  second.rerender(<SSHTransferProgress state={{ ...state, busy: false, phase: 'cancelled', finishedAt: Date.now() - 5000, message: '传输已取消' }} onCancel={() => {}} />)
  expect(screen.queryByRole('region')).toBeNull()
})
