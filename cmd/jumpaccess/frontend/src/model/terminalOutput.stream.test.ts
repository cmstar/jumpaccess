import { expect, test, vi } from 'vitest'
import { TerminalOutput } from './terminalOutput'

test('挂载前的输出完整排队，解析完成后释放并确认，重新订阅不重放历史', async () => {
  const output = new TerminalOutput()
  const data = 'x'.repeat(1024 * 1024 + 1)
  output.write(data)
  const drained = vi.fn()
  const waiting = output.drain().then(drained)
  const callbacks: Array<() => void> = []
  const write = vi.fn((_data: string, done: () => void) => callbacks.push(done))
  const off = output.subscribe(write)
  expect(write).toHaveBeenCalledWith(data, expect.any(Function))
  await Promise.resolve()
  expect(drained).not.toHaveBeenCalled()
  output.write('next')
  callbacks.shift()!()
  expect(write).toHaveBeenLastCalledWith('next', expect.any(Function))
  callbacks.shift()!()
  await waiting
  off()
  const other = vi.fn()
  output.subscribe(other)
  expect(other).not.toHaveBeenCalled()
})

test('独立输出队列互不阻塞，关闭时释放等待者并忽略迟到解析回调', async () => {
  const a = new TerminalOutput()
  const b = new TerminalOutput()
  let complete!: () => void
  a.subscribe((_data, done) => { complete = done })
  const writeB = vi.fn((_data: string, done: () => void) => done())
  b.subscribe(writeB)
  a.write('a')
  a.write('abandoned')
  const waiting = a.drain()
  b.write('b')
  await b.drain()
  expect(writeB).toHaveBeenCalledOnce()
  a.dispose()
  await waiting
  complete()
  a.write('late')
  await a.drain()
})

test('订阅重建时仅重试尚未解析的块，旧回调不确认新订阅的输出', async () => {
  const output = new TerminalOutput()
  let oldDone!: () => void
  const off = output.subscribe((_data, done) => { oldDone = done })
  output.write('pending')
  off()
  let newDone!: () => void
  const next = vi.fn((_data: string, done: () => void) => { newDone = done })
  output.subscribe(next)
  expect(next).toHaveBeenCalledWith('pending', expect.any(Function))
  const drained = vi.fn()
  const waiting = output.drain().then(drained)
  oldDone()
  await Promise.resolve()
  expect(drained).not.toHaveBeenCalled()
  newDone()
  await waiting
})
