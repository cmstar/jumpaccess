import { fireEvent, render, screen } from '@testing-library/react'
import { expect, test, vi } from 'vitest'
import { ZmodemToolbar } from './ZmodemToolbar'

test.each([
  { name: '检测尚未开始', active: true, state: undefined },
  { name: '未安装', active: true, state: { checked: true, upload: false, download: false, busy: false } },
  { name: '无法检测', active: true, state: { checked: false, upload: false, download: false, busy: false } },
  { name: '已断连', active: false, state: { checked: true, upload: true, download: true, busy: false } },
  { name: '传输中', active: true, state: { checked: true, upload: true, download: true, busy: true } },
])('$name 时按钮保持可见、禁用并说明不可用', ({ active, state }) => {
  const onCommand = vi.fn()
  render(<ZmodemToolbar active={active} state={state} onCommand={onCommand} />)
  const upload = screen.getByLabelText('上传文件（ZMODEM）')
  const download = screen.getByLabelText('下载文件（ZMODEM）')
  expect(upload).toBeDisabled()
  expect(download).toBeDisabled()
  expect(upload).toHaveAttribute('title', '上传文件到当前目录（rz / ZMODEM 当前不可用）')
  expect(download).toHaveAttribute('title', '下载文件（sz / ZMODEM 当前不可用）')
  fireEvent.click(upload)
  fireEvent.click(download)
  expect(onCommand).not.toHaveBeenCalled()
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
})

test('按方向启用按钮并显示准确提示，下载路径使用安全的字面量参数', () => {
  const onCommand = vi.fn()
  const { rerender } = render(<ZmodemToolbar active state={{ checked: true, upload: true, download: false, busy: false }} onCommand={onCommand} />)
  expect(screen.getByLabelText('下载文件（ZMODEM）')).toBeDisabled()
  expect(screen.getByLabelText('上传文件（ZMODEM）')).toHaveAttribute('title', '上传文件到当前目录（rz / ZMODEM）')
  fireEvent.click(screen.getByLabelText('上传文件（ZMODEM）'))
  expect(onCommand).toHaveBeenCalledWith('rz\r')
  rerender(<ZmodemToolbar active state={{ checked: true, upload: true, download: true, busy: true }} onCommand={onCommand} />)
  expect(screen.getByLabelText('上传文件（ZMODEM）')).toBeDisabled()
  rerender(<ZmodemToolbar active state={{ checked: true, upload: true, download: true, busy: false }} onCommand={onCommand} />)
  expect(screen.getByLabelText('下载文件（ZMODEM）')).toHaveAttribute('title', '下载文件（sz / ZMODEM）')
  fireEvent.click(screen.getByLabelText('下载文件（ZMODEM）'))
  fireEvent.change(screen.getByLabelText('远程文件路径'), { target: { value: '/tmp/a b' } })
  fireEvent.click(screen.getByRole('button', { name: '下载' }))
  expect(onCommand).toHaveBeenLastCalledWith("sz -- '/tmp/a b'\r")
})
