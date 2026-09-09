import { expect, test, vi } from 'vitest'
import { ZmodemProgress } from './zmodemProgress'

test('进度原地刷新、节流，完成后才显示 100%，随后恢复远端提示符', () => {
  vi.useFakeTimers()
  try {
    const output: string[] = []
    const progress = new ZmodemProgress(text => output.push(text))
    progress.start('download', '文件.bin', 1024)
    expect(output.join('')).toContain('Download 文件.bin')
    progress.update(512)
    const count = output.length
    progress.update(600)
    expect(output).toHaveLength(count)
    vi.advanceTimersByTime(100)
    progress.update(700)
    expect(output.at(-1)).toContain('68%')
    progress.update(1024)
    expect(output.join('')).not.toContain('100%')
    expect(output.at(-1)).toContain('Saving')
    progress.terminal('shell$ ')
    expect(output.join('')).not.toContain('shell$ ')
    progress.end('Complete', true)
    expect(output.join('')).toContain('100%')
    expect(output.join('')).toMatch(/Complete\r\nshell\$ $/)
  } finally { vi.useRealTimers() }
})

test('取消、空文件与文件名控制字符不破坏终端', () => {
  const output: string[] = []
  const progress = new ZmodemProgress(text => output.push(text))
  progress.start('upload', 'bad\x1b[2J\r\nname', 0)
  expect(output.join('')).not.toContain('\x1b[2J')
  expect(output.join('')).not.toContain('100%')
  progress.end('Cancelled')
  expect(output.join('')).not.toContain('100%')
  expect(output.at(-1)).toMatch(/Cancelled\r\n$/)
  progress.start('download', 'empty', 0)
  progress.end('Complete', true)
  expect(output.at(-1)).toContain('100%')
})

test('远端普通输出缓存有上限，超限后保留输出且停止重画进度', () => {
  const output: string[] = []
  const progress = new ZmodemProgress(text => output.push(text))
  progress.start('download', 'file', 1024)
  const remote = 'x'.repeat(70_000)
  progress.terminal(remote)
  expect(output.join('')).toContain(remote)
  const count = output.length
  progress.update(512)
  progress.end('Complete', true)
  expect(output).toHaveLength(count)
})
