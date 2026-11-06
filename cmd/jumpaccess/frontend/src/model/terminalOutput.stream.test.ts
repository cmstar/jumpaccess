import { expect, test, vi } from 'vitest'
import { TerminalOutput } from './terminalOutput'

test('只有本地连接提示不算 SSH 输出，待解析和已解析的会话内容都算', async () => {
  const output = new TerminalOutput()
  output.writeStatus('Connection closed.')
  output.writeStatus('Reconnecting ...')
  output.write('')
  expect(output.hasSSHOutput).toBe(false)
  output.subscribe((_data, parsed) => parsed())
  await output.drain()
  expect(output.hasSSHOutput).toBe(false)
  const early = new TerminalOutput()
  early.write('welcome')
  expect(early.hasSSHOutput).toBe(true)
  early.forwardTo(output)
  await output.drain()
  output.writeStatus('Connection closed.')
  expect(output.hasSSHOutput).toBe(true)
})

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

test('早到输出整体转入原队列，新输出不能插队，实际解析后才确认', async () => {
  const existing = new TerminalOutput()
  const early = new TerminalOutput()
  const writes: string[] = []
  const callbacks: Array<() => void> = []
  existing.subscribe((data, parsed) => { writes.push(data); callbacks.push(parsed) })
  existing.write('old')
  early.write('early-1')
  early.write('early-2')
  const acknowledged = vi.fn()
  const waiting = early.drain().then(acknowledged)
  early.forwardTo(existing)
  existing.write('new')
  expect(writes).toEqual(['old'])
  for (let i = 0; i < 3; i++) {
    callbacks.shift()!()
    await Promise.resolve()
    expect(acknowledged).not.toHaveBeenCalled()
  }
  expect(writes).toEqual(['old', 'early-1', 'early-2', 'new'])
  callbacks.shift()!()
  await waiting
  early.dispose()
  existing.write('still-open')
  expect(writes.at(-1)).toBe('still-open')
})

test('连接提示等待远端输出解析完成，关闭 Tab 解除转入输出的等待', async () => {
  const output = new TerminalOutput()
  const early = new TerminalOutput()
  let parsed!: () => void
  const status = vi.fn()
  output.subscribe((_data, done) => { parsed = done }, status)
  output.write('remote')
  output.writeStatus('closed')
  expect(status).not.toHaveBeenCalled()
  parsed()
  expect(status).toHaveBeenCalledWith('closed', expect.any(Function))
  early.write('early')
  const waiting = early.drain()
  early.forwardTo(output)
  output.dispose()
  await waiting
})
