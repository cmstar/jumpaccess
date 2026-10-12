import { expect, test, vi } from 'vitest'
import { ZmodemProgress } from './zmodemProgress'

test('进度原地刷新、节流，完成后才显示 100%，随后恢复远端提示符', () => {
  vi.useFakeTimers()
  try {
    const output: string[] = []
    const progress = new ZmodemProgress(text => output.push(text))
    progress.start('download', '/Users/local/Downloads/文件.bin', 1024)
    expect(output.join('')).toContain('Download to /Users/local/Downloads/文件.bin')
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
    expect(output.join('')).toMatch(/Complete · Elapsed .* · Avg .*\r\nshell\$ $/)
  } finally { vi.useRealTimers() }
})

test('取消、空文件与完整路径中的控制字符不破坏终端', () => {
  const output: string[] = []
  const progress = new ZmodemProgress(text => output.push(text))
  progress.start('upload', '/local/bad\x1b[2J\r\nname/file.bin', 0)
  expect(output.join('')).toContain('Upload /local/bad[2Jname/file.bin')
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

test.each(['upload', 'download'] as const)('%s 完成时统计单文件耗时和平均速度，包含确认或保存时间', (direction) => {
  vi.useFakeTimers()
  try {
    const output: string[] = []
    const progress = new ZmodemProgress(text => output.push(text))
    // 选择文件所花的时间不计入传输。
    vi.advanceTimersByTime(60_000)
    progress.start(direction, 'file.bin', 1024 * 1024)
    vi.advanceTimersByTime(1500)
    progress.update(1024 * 1024)
    expect(output.at(-1)).toContain(direction === 'download' ? 'Saving' : 'Waiting for confirmation')
    expect(output.join('')).not.toContain('Elapsed')
    // 系统时钟校正不影响耗时；最后的确认或保存仍计时。
    vi.setSystemTime(new Date('2000-01-01'))
    vi.advanceTimersByTime(500)
    progress.terminal('shell$ ')
    progress.end('Complete', true)
    expect(output.at(-1)).toBe('\r\x1b[2K100% · 1.00 MiB / 1.00 MiB · Complete · Elapsed 2.00 s · Avg 512.00 KiB/s\r\nshell$ ')
    const count = output.length
    vi.advanceTimersByTime(1000)
    progress.update(1024 * 1024)
    progress.end('Complete', true)
    expect(output).toHaveLength(count)
  } finally { vi.useRealTimers() }
})

test('下一文件重新统计，文件间等待时间不计入', () => {
  vi.useFakeTimers()
  try {
    const output: string[] = []
    const progress = new ZmodemProgress(text => output.push(text))
    progress.start('upload', 'file.bin', 1024)
    vi.advanceTimersByTime(2000)
    progress.update(1024)
    progress.end('Complete', true)
    expect(output.at(-1)).toContain('Elapsed 2.00 s · Avg 512 B/s')
    vi.advanceTimersByTime(60_000)
    progress.start('upload', 'file.bin', 2048)
    vi.advanceTimersByTime(500)
    progress.update(2048)
    progress.end('Complete', true)
    expect(output.at(-1)).toContain('Elapsed 0.50 s · Avg 4.00 KiB/s')
  } finally { vi.useRealTimers() }
})

test.each([
  { size: 0, elapsed: 0, expected: 'Elapsed <0.01 s · Avg 0 B/s' },
  { size: 1024, elapsed: 0, expected: 'Elapsed <0.01 s · Avg —' },
  { size: 1, elapsed: 1, expected: 'Elapsed <0.01 s · Avg 1000 B/s' },
  { size: 1024, elapsed: 10, expected: 'Elapsed 0.01 s · Avg 100.00 KiB/s' },
  { size: 0, elapsed: 59_999, expected: 'Elapsed 1 m 0.00 s · Avg 0 B/s' },
  { size: 0, elapsed: 62_500, expected: 'Elapsed 1 m 2.50 s · Avg 0 B/s' },
  { size: 0, elapsed: 3_662_500, expected: 'Elapsed 1 h 1 m 2.50 s · Avg 0 B/s' },
])('空文件、极短和长耗时的统计可读且速度有限：$expected', ({ size, elapsed, expected }) => {
  let now = 1000
  const clock = vi.spyOn(performance, 'now').mockImplementation(() => now)
  try {
    const output: string[] = []
    const progress = new ZmodemProgress(text => output.push(text))
    progress.start('download', 'file.bin', size)
    now += elapsed
    progress.update(size)
    progress.end('Complete', true)
    expect(output.at(-1)).toContain(expected)
    expect(output.at(-1)).not.toMatch(/NaN|Infinity/)
  } finally { clock.mockRestore() }
})

test.each(['Cancelled', 'Transfer failed', 'Transfer interrupted', 'Skipped by remote'])('%s 不显示成功统计', (status) => {
  const output: string[] = []
  const progress = new ZmodemProgress(text => output.push(text))
  progress.start('upload', 'file.bin', 1024)
  progress.update(1024)
  progress.end(status)
  expect(output.at(-1)).toContain(status)
  expect(output.join('')).not.toMatch(/100%|Elapsed|Avg/)
})
