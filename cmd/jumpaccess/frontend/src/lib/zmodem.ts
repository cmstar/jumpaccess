import { Sentry, type Detection, type Session, type Transfer } from 'zmodem.js'
import type { TransferFile, ZmodemBackend, ZmodemState } from './zmodemTypes'

export function encodeBytes(bytes: Uint8Array | number[]): string {
  let text = ''
  for (let i = 0; i < bytes.length; i += 8192) text += String.fromCharCode(...bytes.slice(i, i + 8192))
  return btoa(text)
}
export function decodeBytes(text: string): Uint8Array { return Uint8Array.from(atob(text), c => c.charCodeAt(0)) }
export function downloadCommand(path: string): string {
  if (!path || /[\x00-\x1f\x7f]/.test(path)) throw new Error('请输入不含换行的远程文件路径')
  return `sz -- '${path.replace(/'/g, `'"'"'`)}'\r`
}

// 控制器属于 live session，不属于可卸载的 TerminalPane；历史回放不进入协议解析器。
export class ZmodemController {
  state: ZmodemState = { checked: false, upload: false, download: false, busy: false }
  private sentry: Sentry
  private decoder = new TextDecoder()
  private idle = ''
  private skipXon = false
  private suppressTerminal = false
  private detection?: Detection
  private selecting = false
  private finishing = false
  private selectionJob: Promise<unknown> = Promise.resolve()
  private disposed = false
  private generation = 0
  private timer?: ReturnType<typeof setTimeout>
  private prefixTimer?: ReturnType<typeof setTimeout>
  private sendQueue: Promise<void> = Promise.resolve()
  private diskQueue: Promise<void> = Promise.resolve()
  private receiveJob: Promise<void> = Promise.resolve()
  private offeredFiles = 0
  private detectedHeader = false
  private bytesWithoutProgress = 0
  private lastProgressAt = 0

  constructor(readonly id: string, private backend: ZmodemBackend, private output: (text: string) => void, private changed: (state: ZmodemState) => void) {
    this.sentry = this.newSentry()
  }
  private update(patch: Partial<ZmodemState>) {
    if (this.disposed) return
    this.state = { ...this.state, ...patch }
    this.changed(this.state)
  }
  async probe() {
    try {
      const result = await this.backend.probeSSHTransferCommands(this.id)
      this.update({ checked: result.checked, upload: result.upload || this.state.upload, download: result.download || this.state.download })
    } catch { /* 网关可以不支持 exec，手工 rz/sz 仍然可用。 */ }
  }
  async command(command: string, write: (command: string) => Promise<void>) {
    if (this.state.busy || this.disposed) return
    this.update({ busy: true, message: '等待远程传输开始', name: '', size: 0, transferred: 0 })
    this.touch()
    try { await write(command) } catch (reason) { this.fail(reason) }
  }
  private newSentry() {
    return new Sentry({
      to_terminal: bytes => { if (!this.suppressTerminal) this.terminal(Uint8Array.from(bytes)) },
      sender: bytes => {
        const generation = this.generation
        const encoded = encodeBytes(bytes)
        this.sendQueue = this.sendQueue.then(async () => {
          if (!this.disposed && generation === this.generation) await this.backend.writeSSHBinary(this.id, encoded)
        })
        void this.sendQueue.catch(reason => this.fail(reason))
      },
      on_detect: detection => {
        this.detectedHeader = true
        this.detection = detection
        if (!this.selecting && !this.finishing) void this.selectAndStart()
      },
      on_retract: () => { this.detection = undefined },
    })
  }
  private terminal(bytes: Uint8Array) {
    // 待确认握手后出现普通输出，说明请求已过时；不能再向 Shell 发协议应答。
    if (bytes.length && this.detection) { this.detection = undefined; this.sentry = this.newSentry() }
    const text = this.decoder.decode(bytes, { stream: true })
    if (text) this.output(text)
  }

  // 先收齐完整起始帧，避免分包造成握手乱码或重复弹窗。
  async consume(bytes: Uint8Array) {
    if (this.disposed) return
    if (this.state.busy && !this.selecting) this.touch()
    try {
      if (this.sentry.get_confirmed_session()) {
        this.bytesWithoutProgress += bytes.length
        if (this.bytesWithoutProgress > 1024 * 1024) throw new Error('远程传输数据格式异常')
        this.sentry.consume(bytes)
      }
      else {
        clearTimeout(this.prefixTimer)
        let text = ''
        for (let i = 0; i < bytes.length; i += 8192) text += String.fromCharCode(...bytes.slice(i, i + 8192))
        if (this.skipXon && text.length) { if (text[0] === '\x11') text = text.slice(1); this.skipXon = false }
        this.idle += text
        const prefix = '**\x18B'
        while (this.idle) {
          const start = this.idle.indexOf(prefix)
          if (start < 0) {
            let retain = 0
            for (let n = 1; n < prefix.length; n++) if (this.idle.endsWith(prefix.slice(0, n))) retain = n
            const plain = this.idle.slice(0, this.idle.length - retain)
            this.terminal(Uint8Array.from(plain, c => c.charCodeAt(0)))
            this.idle = this.idle.slice(this.idle.length - retain)
            break
          }
          if (start) this.terminal(Uint8Array.from(this.idle.slice(0, start), c => c.charCodeAt(0)))
          this.idle = this.idle.slice(start)
          if (this.idle.length < 20) break
          const header = this.idle.slice(0, 20)
          if (!/^\*\*\x18B0[01][0-9a-f]{12}\r[\n\x8a]$/i.test(header)) {
            this.terminal(Uint8Array.of(this.idle.charCodeAt(0)))
            this.idle = this.idle.slice(1)
            continue
          }
          this.idle = this.idle.slice(20)
          if (this.idle.startsWith('\x11')) this.idle = this.idle.slice(1)
          else this.skipXon = !this.idle.length
          this.suppressTerminal = true
          this.detectedHeader = false
          try { this.sentry.consume(Uint8Array.from(header, c => c.charCodeAt(0))) }
          finally { this.suppressTerminal = false }
          if (!this.detectedHeader) this.terminal(Uint8Array.from(header, c => c.charCodeAt(0)))
        }
        if (this.idle) this.prefixTimer = setTimeout(() => {
          this.terminal(Uint8Array.from(this.idle, c => c.charCodeAt(0)))
          this.idle = ''
        }, 1000)
      }
      await this.diskQueue
    } catch (reason) { this.fail(reason) }
  }
  private touch() {
    clearTimeout(this.timer)
    this.timer = setTimeout(() => this.cancel('传输等待超时，已取消'), this.selecting ? 300_000 : 60_000)
  }
  private progress(transferred: number) {
    this.bytesWithoutProgress = 0
    this.state = { ...this.state, transferred }
    const now = Date.now()
    if (now - this.lastProgressAt >= 100) { this.lastProgressAt = now; this.update({ transferred }) }
  }
  private async selectAndStart() {
    const generation = this.generation
    const direction = this.detection?.get_session_role() === 'send' ? 'upload' : 'download'
    this.selecting = true
    this.update({ busy: true, direction, [direction]: true, message: direction === 'upload' ? '选择上传文件' : '选择下载位置', name: '', transferred: 0, size: 0 })
    this.touch()
    try {
      // 用户先通过原生选择器确认传输，再允许协议向远端发送任何应答。
      const files = direction === 'upload' ? await (this.selectionJob = this.backend.chooseZmodemUploadFiles(this.id)) : []
      const grant = direction === 'download' ? await (this.selectionJob = this.backend.chooseZmodemDownloadDirectory(this.id)) : ''
      if (this.disposed || generation !== this.generation) { await this.backend.endZmodemTransfer(this.id); return }
      if ((direction === 'upload' && !files.length) || (direction === 'download' && !grant)) { this.cancel(); return }
      const detection = this.detection
      if (!detection?.is_valid()) { this.cancel('远程传输请求已失效，请重新执行命令'); return }
      this.selecting = false
      this.touch()
      const session = detection.confirm()
      this.detection = undefined
      this.update({ message: '正在传输' })
      if (direction === 'upload') await this.upload(session, files, generation)
      else {
        this.offeredFiles = 0
        session.on('offer', offer => {
          this.offeredFiles++
          this.receiveJob = this.download(offer, grant, generation)
          void this.receiveJob.catch(reason => this.fail(reason))
        })
        session.on('session_end', () => {
          void this.receiveJob.then(() => {
            if (generation === this.generation && !this.disposed) void this.finish(this.offeredFiles ? '下载完成' : '没有可下载的文件')
          }).catch(reason => this.fail(reason))
        })
        session.start()
      }
    } catch (reason) { if (generation === this.generation && !this.disposed) this.fail(reason) }
  }
  private async upload(session: Session, files: TransferFile[], generation: number) {
    let skipped = 0
    for (const file of files) {
      if (this.disposed || generation !== this.generation) return
      this.update({ name: file.name, size: file.size, transferred: 0 })
      const transfer = await session.send_offer({ name: file.name, size: file.size })
      if (!transfer) { skipped++; await this.backend.closeZmodemFile(file.id, false); continue }
      let transferred = 0
      while (transferred < file.size) {
        const bytes = decodeBytes(await this.backend.readZmodemFile(file.id))
        if (this.disposed || generation !== this.generation) return
        if (!bytes.length || transferred + bytes.length > file.size) throw new Error('上传文件在传输期间发生变化')
        transfer.send(bytes)
        await this.sendQueue
        transferred += bytes.length
        this.touch()
        this.progress(transferred)
      }
      await transfer.end()
      await this.backend.closeZmodemFile(file.id, true)
    }
    await session.close()
    if (generation === this.generation) await this.finish(skipped ? `上传结束，远端跳过 ${skipped} 个文件` : '上传完成')
  }
  private async download(offer: Transfer, grant: string, generation: number) {
    const details = offer.get_details()
    if (!Number.isSafeInteger(details.size) || details.size < 0) throw new Error('远程文件大小无效')
    const file = await this.backend.createZmodemDownload(this.id, grant, details.name, details.size)
    if (this.disposed || generation !== this.generation) { await this.backend.closeZmodemFile(file.id, false); return }
    this.update({ name: file.name, size: file.size, transferred: 0 })
    let transferred = 0
    await offer.accept({ on_input: data => {
      this.bytesWithoutProgress = 0
      // 不使用库默认的整文件缓存；每个协议块顺序写入原生文件句柄。
      const encoded = encodeBytes(data)
      this.diskQueue = this.diskQueue.then(async () => {
        if (this.disposed || generation !== this.generation) return
        await this.backend.writeZmodemFile(file.id, encoded)
        transferred += data.length
        this.progress(transferred)
      })
      void this.diskQueue.catch(reason => this.fail(reason))
    } })
    await this.diskQueue
    if (generation === this.generation) await this.backend.closeZmodemFile(file.id, true)
  }
  private async finish(message: string) {
    if (this.finishing) return
    this.finishing = true
    clearTimeout(this.timer)
    await this.selectionJob.catch(() => {})
    await this.sendQueue.catch(() => {})
    await this.diskQueue.catch(() => {})
    await this.backend.endZmodemTransfer(this.id)
    this.sendQueue = Promise.resolve()
    this.diskQueue = Promise.resolve()
    this.bytesWithoutProgress = 0
    this.selecting = false
    this.finishing = false
    this.update({ busy: false, message })
  }
  cancel(message = '传输已取消') {
    if (this.disposed || this.finishing) return
    this.update({ message })
    this.generation++
    const session = this.sentry.get_confirmed_session()
    try {
      if (session) session.abort()
      else if (this.detection?.is_valid()) this.detection.deny()
    } catch { /* 断连后的协议可能已经结束。 */ }
    this.detection = undefined
    this.sentry = this.newSentry()
    this.idle = ''
    this.skipXon = false
    this.selecting = false
    clearTimeout(this.timer)
    void this.finish(message).catch(() => this.update({ busy: false, message }))
  }
  private fail(reason: unknown) {
    if (!this.state.busy || this.disposed || this.finishing) return
    this.cancel(`传输失败：${reason instanceof Error ? reason.message : String(reason)}`)
  }
  dispose() {
    if (this.disposed) return
    this.disposed = true
    this.generation++
    clearTimeout(this.timer)
    clearTimeout(this.prefixTimer)
    void this.backend.endZmodemTransfer(this.id).catch(() => {})
  }
}
