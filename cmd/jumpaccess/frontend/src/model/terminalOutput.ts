type OutputWriter = (data: string, parsed: () => void) => void

// 只持有尚未被 xterm 解析的输出；已解析历史由各 Tab 自己的终端按行管理。
// 事件桥在 drain 完成后确认本块，避免后台 Tab 或首次挂载期间无限积压。
export class TerminalOutput {
  private pending: string[] = []
  private writer?: OutputWriter
  private writing?: object
  private pumping = false
  private disposed = false
  private waiters = new Set<() => void>()
  hasOutput = false

  write(data: string) {
    if (this.disposed || !data) return
    this.hasOutput = true
    this.pending.push(data)
    this.pump()
  }

  subscribe(writer: OutputWriter): () => void {
    if (this.disposed) return () => {}
    this.writer = writer
    this.pump()
    return () => {
      if (this.writer !== writer) return
      this.writer = undefined
      this.writing = undefined
    }
  }

  drain(): Promise<void> {
    return this.pending.length === 0 ? Promise.resolve() : new Promise(resolve => this.waiters.add(resolve))
  }

  dispose() {
    this.disposed = true
    this.writer = undefined
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
        this.writer(this.pending[0], () => {
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
