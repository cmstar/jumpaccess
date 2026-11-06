type OutputWriter = (data: string, parsed: () => void) => void

// 只持有尚未被 xterm 解析的输出；已解析历史由各 Tab 自己的终端按行管理。
// 事件桥在 drain 完成后确认本块，避免后台 Tab 或首次挂载期间无限积压。
export class TerminalOutput {
  private pending: Array<{ data: string; status: boolean }> = []
  private writer?: OutputWriter
  private statusWriter?: OutputWriter
  private forwardedTo?: TerminalOutput
  private writing?: object
  private pumping = false
  private disposed = false
  private waiters = new Set<() => void>()
  hasOutput = false
  hasSSHOutput = false

  write(data: string) {
    this.enqueue(data, false)
  }

  // 本地连接提示与远端数据共用队列，先解析完旧输出再恢复终端模式。
  writeStatus(data: string) {
    this.enqueue(data, true)
  }

  private enqueue(data: string, status: boolean) {
    if (this.disposed || !data) return
    this.hasOutput = true
    if (!status) this.hasSSHOutput = true
    if (this.forwardedTo) {
      this.forwardedTo.enqueue(data, status)
      return
    }
    this.pending.push({ data, status })
    this.pump()
  }

  subscribe(writer: OutputWriter, statusWriter: OutputWriter = writer): () => void {
    if (this.disposed) return () => {}
    this.writer = writer
    this.statusWriter = statusWriter
    this.pump()
    return () => {
      if (this.writer !== writer) return
      this.writer = undefined
      this.statusWriter = undefined
      this.writing = undefined
    }
  }

  drain(): Promise<void> {
    if (this.forwardedTo) return this.forwardedTo.drain()
    return this.pending.length === 0 ? Promise.resolve() : new Promise(resolve => this.waiters.add(resolve))
  }

  // 仅用于尚未挂载的早到输出，一次性转移所有块，避免后到数据插队。
  forwardTo(target: TerminalOutput) {
    if (this.disposed) return
    if (this.writer || this.forwardedTo || target === this) throw new Error('Cannot forward an attached terminal output')
    this.forwardedTo = target
    const pending = this.pending
    this.pending = []
    for (const chunk of pending) target.enqueue(chunk.data, chunk.status)
    void target.drain().then(() => this.finish())
  }

  dispose() {
    this.disposed = true
    this.writer = undefined
    this.statusWriter = undefined
    this.forwardedTo = undefined
    this.writing = undefined
    this.pending = []
    this.finish()
  }

  private finish() {
    if (this.pending.length > 0) return
    for (const resolve of this.waiters) resolve()
    this.waiters.clear()
  }

  private pump() {
    if (this.pumping) return
    this.pumping = true
    try {
      while (this.writer && !this.writing && this.pending.length > 0) {
        const token = {}
        this.writing = token
        const { data, status } = this.pending[0]
        const writer = status ? this.statusWriter! : this.writer
        writer(data, () => {
          if (this.writing !== token) return
          this.pending.shift()
          this.writing = undefined
          this.pump()
          this.finish()
        })
      }
    } finally {
      this.pumping = false
    }
  }
}
