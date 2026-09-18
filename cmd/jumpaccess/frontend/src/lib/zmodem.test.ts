import { expect, test, vi } from 'vitest'
import { ZmodemController, encodeBytes, decodeBytes, downloadCommand } from './zmodem'
import type { ZmodemBackend } from './zmodemTypes'
import { Sentry, type Session } from 'zmodem.js'

function backend(): ZmodemBackend {
  return {
    probeSSHTransferCommands: vi.fn().mockResolvedValue({ checked: true, upload: true, download: true }),
    writeSSHBinary: vi.fn().mockResolvedValue(undefined),
    chooseZmodemUploadFiles: vi.fn().mockResolvedValue([]),
    chooseZmodemDownloadDirectory: vi.fn().mockResolvedValue(''),
    createZmodemDownload: vi.fn(), readZmodemFile: vi.fn(), writeZmodemFile: vi.fn(), closeZmodemFile: vi.fn(),
    endZmodemTransfer: vi.fn().mockResolvedValue(undefined),
  }
}

test.each([false, true])('取消下载实际中止远端，丢弃在途数据并冻结进度；发送队列先前失败=%s', async (failedWrite) => {
  const api = backend()
  const output: string[] = []
  let incoming = Promise.resolve()
  let remote: Session | undefined
  let rejectWrite!: (reason: Error) => void
  let releaseDisk: (() => void) | undefined
  vi.mocked(api.chooseZmodemDownloadDirectory).mockResolvedValue('grant')
  vi.mocked(api.createZmodemDownload).mockResolvedValue({ id: 'file', name: 'large.bin', path: '/downloads/large.bin', size: 8_000_000 })
  const controller = new ZmodemController('ssh', api, text => output.push(text), () => {})
  const peer = new Sentry({
    to_terminal: () => {}, on_retract: () => {},
    sender: bytes => { const copy = Uint8Array.from(bytes); incoming = incoming.then(() => controller.consume(copy)) },
    on_detect: detection => { remote = detection.confirm() },
  })
  let delayWrite = false
  vi.mocked(api.writeSSHBinary).mockImplementation(async (_id, encoded) => {
    if (delayWrite) { delayWrite = false; await new Promise<void>((_resolve, reject) => { rejectWrite = reject }) }
    try { peer.consume(decodeBytes(encoded)) } catch (reason) { if (!remote?.aborted()) throw reason }
  })
  await controller.consume(new TextEncoder().encode('**\x18B00000000000000\r\n\x11'))
  await vi.waitFor(() => expect(remote).toBeDefined())
  const offer = remote!.send_offer({ name: 'large.bin', size: 8_000_000 })
  if (failedWrite) {
    // 延迟接收端的 ZRPOS，模拟点击取消时已有一次桥接写入尚未完成。
    delayWrite = true
    await vi.waitFor(() => expect(rejectWrite).toBeDefined())
  } else {
    const transfer = await offer
    transfer!.send(new TextEncoder().encode('first block'))
    await incoming
    vi.mocked(api.writeZmodemFile).mockImplementationOnce(() => new Promise<void>(resolve => { releaseDisk = resolve }))
    transfer!.send(new TextEncoder().encode('pending disk block'))
    await vi.waitFor(() => expect(releaseDisk).toBeDefined())
  }
  vi.useFakeTimers()
  try {
    const progress = controller.state.transferred
    controller.cancel()
    if (failedWrite) rejectWrite(new Error('bridge write failed'))
    releaseDisk?.()
    await vi.advanceTimersByTimeAsync(0)
    expect(remote!.aborted()).toBe(true)
    expect(controller.state.transferred).toBe(progress)
    const before = output.join('')
    const garbage = new TextEncoder().encode('BINARY-DATA-MUST-NOT-RENDER\x00\xff')
    await controller.consume(garbage)
    await vi.advanceTimersByTimeAsync(200)
    await controller.consume(garbage)
    expect(output.join('')).toBe(before)
    expect(controller.state.transferred).toBe(progress)
    expect(controller.state.busy).toBe(true)
    expect(controller.state.phase).toBe('cancelling')
    await vi.advanceTimersByTimeAsync(1000)
    expect(controller.state.busy).toBe(false)
    expect(controller.state.phase).toBe('cancelled')
    expect(output.join('')).not.toContain('Press Enter to refresh')
    expect(output.join('')).not.toContain('Transfer stopped.')
    if (!failedWrite) expect(output.join('')).toContain('Transfer cancelled')
    expect(api.closeZmodemFile).not.toHaveBeenCalledWith('file', true)
    expect(api.endZmodemTransfer).toHaveBeenCalledWith('ssh')
    await controller.consume(new TextEncoder().encode('shell$ '))
    expect(output.join('')).toContain('shell$ ')
  } finally { controller.dispose(); vi.useRealTimers() }
})

test('远端持续发送时不恢复终端或提前宣布取消完成，超时提示重新连接', async () => {
  vi.useFakeTimers()
  const api = backend()
  const output: string[] = []
  const controller = new ZmodemController('ssh', api, text => output.push(text), () => {})
  try {
    await controller.command('sz -- large.bin\r', vi.fn())
    controller.cancel()
    await vi.advanceTimersByTimeAsync(0)
    for (let i = 0; i < 30; i++) {
      await controller.consume(new TextEncoder().encode('BINARY'))
      await vi.advanceTimersByTimeAsync(400)
    }
    expect(controller.state.busy).toBe(true)
    expect(controller.state.message).toContain('请重新连接 SSH')
    expect(output.join('')).not.toContain('BINARY')
    await vi.advanceTimersByTimeAsync(500)
    expect(controller.state.busy).toBe(false)
  } finally { controller.dispose(); vi.useRealTimers() }
})

test('取消信号自身发送失败时持续隔离，不把失败当作已取消', async () => {
  vi.useFakeTimers()
  const api = backend()
  const output: string[] = []
  const controller = new ZmodemController('ssh', api, text => output.push(text), () => {})
  try {
    vi.mocked(api.writeSSHBinary).mockRejectedValue(new Error('SSH write failed'))
    await controller.command('sz -- large.bin\r', vi.fn())
    controller.cancel()
    await vi.advanceTimersByTimeAsync(11_000)
    await controller.consume(new TextEncoder().encode('BINARY'))
    expect(controller.state.busy).toBe(true)
    expect(controller.state.message).toContain('请重新连接 SSH')
    expect(output.join('')).not.toContain('BINARY')
  } finally { controller.dispose(); vi.useRealTimers() }
})

test('binary bridge preserves all octets and download command quotes shell syntax', () => {
  const data = Uint8Array.from({ length: 256 }, (_, i) => i)
  expect(decodeBytes(encodeBytes(data))).toEqual(data)
  expect(downloadCommand("/tmp/a'$(touch BAD)" )).toBe("sz -- '/tmp/a'\"'\"'$(touch BAD)'\r")
  expect(() => downloadCommand('a\nb')).toThrow()
})

test('fragmented UTF-8 and ZMODEM handshake do not leak protocol into terminal', async () => {
  const api = backend()
  let choose!: (value: string) => void
  vi.mocked(api.chooseZmodemDownloadDirectory).mockReturnValue(new Promise(resolve => { choose = resolve }))
  const output: string[] = []
  const controller = new ZmodemController('ssh', api, value => output.push(value), () => {})
  const text = new TextEncoder().encode('你好')
  await controller.consume(text.slice(0, 2))
  await controller.consume(text.slice(2))
  const header = new TextEncoder().encode('**\x18B00000000000000\r\n\x11')
  for (const byte of header) await controller.consume(Uint8Array.of(byte))
  expect(output.join('')).toBe('你好')
  expect(api.chooseZmodemDownloadDirectory).toHaveBeenCalledTimes(1)
  expect(api.writeSSHBinary).not.toHaveBeenCalled()
  choose('')
  await vi.waitFor(() => expect(controller.state.busy).toBe(false))
  expect(api.writeSSHBinary).toHaveBeenCalled()
  controller.dispose()
})

test('late file selection after disconnect is cleaned without writing to SSH', async () => {
  const api = backend()
  let choose!: (value: string) => void
  vi.mocked(api.chooseZmodemDownloadDirectory).mockReturnValue(new Promise(resolve => { choose = resolve }))
  const controller = new ZmodemController('ssh', api, () => {}, () => {})
  await controller.consume(new TextEncoder().encode('**\x18B00000000000000\r\n\x11'))
  controller.dispose()
  choose('grant')
  await Promise.resolve()
  expect(api.writeSSHBinary).not.toHaveBeenCalled()
  expect(api.endZmodemTransfer).toHaveBeenCalledWith('ssh')
})

test('未通过 CRC 校验的相似文本不弹窗且不丢失', async () => {
  const api = backend()
  const output: string[] = []
  const controller = new ZmodemController('ssh', api, text => output.push(text), () => {})
  const invalid = '**\x18B0000000000ffff\r\n'
  await controller.consume(new TextEncoder().encode(invalid))
  expect(output.join('')).toBe(invalid)
  expect(api.chooseZmodemDownloadDirectory).not.toHaveBeenCalled()
  controller.dispose()
})

test('握手后已返回 Shell 时，迟到的文件选择不能发起协议应答', async () => {
  const api = backend()
  let select!: (value: string) => void
  vi.mocked(api.chooseZmodemDownloadDirectory).mockReturnValue(new Promise(resolve => { select = resolve }))
  const controller = new ZmodemController('ssh', api, () => {}, () => {})
  await controller.consume(new TextEncoder().encode('**\x18B00000000000000\r\n\x11'))
  await controller.consume(new TextEncoder().encode('shell$ '))
  select('grant')
  await vi.waitFor(() => expect(controller.state.busy).toBe(false))
  expect(api.writeSSHBinary).not.toHaveBeenCalled()
  expect(controller.state.message).toContain('请求已失效')
  controller.dispose()
})

test('等待远端开始超时后恢复输入，并允许再次传输', async () => {
  vi.useFakeTimers()
  try {
    const api = backend()
    const controller = new ZmodemController('ssh', api, () => {}, () => {})
    const write = vi.fn().mockResolvedValue(undefined)
    await controller.command('rz\r', write)
    expect(controller.state.busy).toBe(true)
    await vi.advanceTimersByTimeAsync(60_501)
    expect(controller.state.busy).toBe(false)
    expect(controller.state.message).toContain('超时')
    await controller.command('rz\r', write)
    expect(write).toHaveBeenCalledTimes(2)
    controller.dispose()
  } finally { vi.useRealTimers() }
})

test.each([false, true])('真实 ZMODEM 双端协议上传并恢复终端，远端全部跳过=%s', async (skipRemote) => {
  const api = backend()
  const data = Uint8Array.from({ length: 70_000 }, (_, i) => i % 256)
  vi.mocked(api.chooseZmodemUploadFiles).mockResolvedValue([{ id: 'file', name: 'binary.dat', path: '/Users/local/上传/binary.dat', size: data.length }])
  let offset = 0
  vi.mocked(api.readZmodemFile).mockImplementation(async () => {
    const chunk = data.slice(offset, offset + 65536)
    offset += chunk.length
    return encodeBytes(chunk)
  })
  const received: number[] = []
  const offeredNames: string[] = []
  let incoming = Promise.resolve()
  const output: string[] = []
  const completed = vi.fn()
  const controller = new ZmodemController('ssh', api, text => output.push(text), () => {}, completed)
  const peer = new Sentry({
    to_terminal: () => {}, on_retract: () => {},
    sender: bytes => { const copy = Uint8Array.from(bytes); incoming = incoming.then(() => controller.consume(copy)) },
    on_detect: detection => {
      const session = detection.confirm()
      session.on('offer', offer => {
        offeredNames.push(offer.get_details().name)
        if (skipRemote) offer.skip()
        else void offer.accept({ on_input: chunk => received.push(...chunk) })
      })
      session.start()
    },
  })
  vi.mocked(api.writeSSHBinary).mockImplementation(async (_id, encoded) => { peer.consume(decodeBytes(encoded)) })
  peer.consume(new TextEncoder().encode('**\x18B00000000000000\r\n\x11'))
  await vi.waitFor(() => expect(controller.state.message).toBe(skipRemote ? '上传结束，远端跳过 1 个文件' : '上传完成'), { timeout: 5000 })
  if (skipRemote) {
    expect(completed).not.toHaveBeenCalled()
    expect(received).toHaveLength(0)
    controller.dispose()
    return
  }
  expect(Uint8Array.from(received)).toEqual(data)
  await controller.consume(new TextEncoder().encode('shell$ '))
  expect(output.join('')).toContain('Upload /Users/local/上传/binary.dat')
  expect(offeredNames).toEqual(['binary.dat'])
  expect(completed).toHaveBeenCalledExactlyOnceWith('上传完成：binary.dat')
  expect(output.join('')).toContain('100%')
  expect(output.join('')).toMatch(/Complete\r\nshell\$ $/)
  controller.dispose()
})

test('空批次下载不产生成功通知', async () => {
  const api = backend()
  vi.mocked(api.chooseZmodemDownloadDirectory).mockResolvedValue('grant')
  const completed = vi.fn()
  const controller = new ZmodemController('ssh', api, () => {}, () => {}, completed)
  let incoming = Promise.resolve()
  let remote: Session | undefined
  const peer = new Sentry({
    to_terminal: () => {}, on_retract: () => {},
    sender: bytes => { const copy = Uint8Array.from(bytes); incoming = incoming.then(() => controller.consume(copy)) },
    on_detect: detection => { remote = detection.confirm() },
  })
  vi.mocked(api.writeSSHBinary).mockImplementation(async (_id, encoded) => { peer.consume(decodeBytes(encoded)) })
  await controller.consume(new TextEncoder().encode('**\x18B00000000000000\r\n\x11'))
  await vi.waitFor(() => expect(remote).toBeDefined())
  await remote!.close()
  await vi.waitFor(() => expect(controller.state.message).toBe('没有可下载的文件'))
  expect(completed).not.toHaveBeenCalled()
  controller.dispose()
})

test('真实 ZMODEM 双端协议逐块下载并等待落盘', async () => {
  const api = backend()
  const data = Uint8Array.from({ length: 70_000 }, (_, i) => i % 256)
  const received: number[] = []
  vi.mocked(api.chooseZmodemDownloadDirectory).mockResolvedValue('grant')
  vi.mocked(api.createZmodemDownload).mockResolvedValue({ id: 'file', name: 'binary (1).dat', path: String.raw`G:\下载目录\binary (1).dat`, size: data.length })
  vi.mocked(api.writeZmodemFile).mockImplementation(async (_id, encoded) => { received.push(...decodeBytes(encoded)) })
  let save!: () => void
  vi.mocked(api.closeZmodemFile).mockReturnValue(new Promise<void>(resolve => { save = resolve }))
  let incoming = Promise.resolve()
  let remote: Session | undefined
  const output: string[] = []
  const completed = vi.fn()
  const controller = new ZmodemController('ssh', api, text => output.push(text), () => {}, completed)
  const peer = new Sentry({
    to_terminal: () => {}, on_retract: () => {},
    sender: bytes => { const copy = Uint8Array.from(bytes); incoming = incoming.then(() => controller.consume(copy)) },
    on_detect: detection => { remote = detection.confirm() },
  })
  vi.mocked(api.writeSSHBinary).mockImplementation(async (_id, encoded) => { peer.consume(decodeBytes(encoded)) })
  await controller.consume(new TextEncoder().encode('**\x18B00000000000000\r\n\x11'))
  await vi.waitFor(() => expect(remote).toBeDefined())
  const transfer = await remote!.send_offer({ name: 'binary.dat', size: data.length })
  transfer!.send(data)
  await transfer!.end()
  await remote!.close()
  await incoming
  await vi.waitFor(() => expect(api.closeZmodemFile).toHaveBeenCalledWith('file', true))
  expect(controller.state.phase).toBe('saving')
  expect(output.join('')).toContain(String.raw`Download to G:\下载目录\binary (1).dat`)
  expect(output.join('')).toContain('Saving')
  expect(output.join('')).not.toContain('100%')
  expect(completed).not.toHaveBeenCalled()
  await controller.consume(new TextEncoder().encode('shell$ '))
  expect(output.join('')).not.toContain('shell$ ')
  save()
  await vi.waitFor(() => expect(controller.state.message).toBe('下载完成'), { timeout: 5000 })
  expect(controller.state.phase).toBe('completed')
  expect(controller.state.finishedAt).toEqual(expect.any(Number))
  expect(completed).toHaveBeenCalledExactlyOnceWith('下载完成：binary (1).dat')
  expect(output.join('')).toMatch(/Complete\r\nshell\$ $/)
  expect(Uint8Array.from(received)).toEqual(data)
  expect(api.closeZmodemFile).toHaveBeenCalledWith('file', true)
  controller.dispose()
})

test.each(['complete', 'cancel', 'dispose', 'failure'] as const)('连续下载按批次通知，%s 后的保存结果正确处理', async (outcome) => {
  const api = backend()
  vi.mocked(api.chooseZmodemDownloadDirectory).mockResolvedValue('grant')
  vi.mocked(api.createZmodemDownload).mockImplementation(async (_id, _grant, name, size) => ({ id: name, name, path: `/downloads/${name}`, size }))
  let saveFirst!: () => void
  let saveSecond!: () => void
  let failSecond!: (reason: Error) => void
  vi.mocked(api.closeZmodemFile)
    .mockReturnValueOnce(new Promise<void>(resolve => { saveFirst = resolve }))
    .mockReturnValueOnce(new Promise<void>((resolve, reject) => { saveSecond = resolve; failSecond = reject }))
  const output: string[] = []
  const completed = vi.fn()
  const controller = new ZmodemController('ssh', api, text => output.push(text), () => {}, completed)
  let incoming = Promise.resolve()
  let remote: Session | undefined
  const peer = new Sentry({
    to_terminal: () => {}, on_retract: () => {},
    sender: bytes => { const copy = Uint8Array.from(bytes); incoming = incoming.then(() => controller.consume(copy)) },
    on_detect: detection => { remote = detection.confirm() },
  })
  vi.mocked(api.writeSSHBinary).mockImplementation(async (_id, encoded) => { peer.consume(decodeBytes(encoded)) })
  await controller.consume(new TextEncoder().encode('**\x18B00000000000000\r\n\x11'))
  await vi.waitFor(() => expect(remote).toBeDefined())
  const first = await remote!.send_offer({ name: 'first', size: 0 })
  await first!.end()
  await vi.waitFor(() => expect(api.closeZmodemFile).toHaveBeenCalledWith('first', true))
  const next = remote!.send_offer({ name: 'second', size: 0 })
  await incoming
  expect(api.createZmodemDownload).toHaveBeenCalledTimes(1)
  saveFirst()
  const second = await next
  await second!.end()
  await remote!.close()
  await incoming
  await vi.waitFor(() => expect(api.closeZmodemFile).toHaveBeenCalledWith('second', true))
  expect(output.join('')).toMatch(/100%.*Complete\r\n\r\nDownload to \/downloads\/second/)
  expect(completed).not.toHaveBeenCalled()
  if (outcome === 'cancel') controller.cancel()
  if (outcome === 'dispose') controller.dispose()
  if (outcome === 'failure') failSecond(new Error('save failed'))
  else saveSecond()
  if (outcome !== 'dispose') await vi.waitFor(() => expect(controller.state.busy).toBe(false))
  else { await Promise.resolve(); await Promise.resolve() }
  if (outcome === 'complete') expect(completed).toHaveBeenCalledExactlyOnceWith('下载完成：first、second')
  else {
    expect(completed).not.toHaveBeenCalled()
    expect(output.join('').split('Download to /downloads/second')[1]).not.toContain('100%')
  }
  controller.dispose()
})
