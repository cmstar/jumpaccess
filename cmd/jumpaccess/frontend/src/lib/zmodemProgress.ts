function safeText(text: string): string {
  // 路径及文件名也可能包含控制字符，不能把控制序列写入终端。
  return text.replace(/[\x00-\x1f\x7f-\x9f\u202a-\u202e\u2066-\u2069]/g, '')
}

function bytes(value: number): string {
  if (value < 1024) return `${value} B`
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(2)} KiB`
  if (value < 1024 * 1024 * 1024) return `${(value / (1024 * 1024)).toFixed(2)} MiB`
  return `${(value / (1024 * 1024 * 1024)).toFixed(2)} GiB`
}

// 本地进度只进入终端显示和历史，不发送到 SSH，也不进入 ZMODEM 解析器。
export class ZmodemProgress {
  private active = false
  private size = 0
  private transferred = 0
  private direction: 'upload' | 'download' = 'download'
  private lastAt = 0
  private pending = ''

  constructor(private output: (text: string) => void) {}

  start(direction: 'upload' | 'download', path: string, size: number) {
    this.end('Transfer ended')
    this.active = true
    this.direction = direction
    this.size = size
    this.transferred = 0
    this.lastAt = Date.now()
    // 本机完整路径单独一行，避免长路径导致进度反复折行。
    this.output(`\r\n${direction === 'upload' ? 'Upload' : 'Download to'} ${safeText(path)}\r\n`)
    this.render('')
  }

  update(transferred: number) {
    if (!this.active) return
    const reachedEnd = transferred >= this.size && this.transferred < this.size
    this.transferred = transferred
    const now = Date.now()
    if (reachedEnd || now - this.lastAt >= 100) {
      this.lastAt = now
      this.render(transferred >= this.size ? (this.direction === 'download' ? 'Saving' : 'Waiting for confirmation') : '')
    }
  }

  private render(status: string, complete = false, suffix = '') {
    const percent = complete ? 100 : this.size ? Math.min(99, Math.floor(this.transferred / this.size * 100)) : 0
    this.output(`\r\x1b[2K${percent}% · ${bytes(this.transferred)} / ${bytes(this.size)}${status ? ` · ${safeText(status)}` : ''}${suffix}`)
  }

  end(status: string, complete = false) {
    if (!this.active) return
    this.render(status, complete, `\r\n${this.pending}`)
    this.active = false
    this.pending = ''
  }

  terminal(text: string) {
    if (!this.active) { this.output(text); return }
    // Shell 提示符可能先于本地 Flush/Sync 返回，待进度换行后再显示。
    if (this.pending.length + text.length <= 64 * 1024) this.pending += text
    else {
      this.end('See status bar for progress')
      this.output(text)
    }
  }
}
