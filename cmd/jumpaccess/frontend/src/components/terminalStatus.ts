import type { Terminal } from '@xterm/xterm'

// 只恢复模式，不使用 RIS / reset()，避免清掉屏幕和滚动历史。
const restoreModes = '\x18\x1b[?1049l\x1b[?47l\x1b[!p\x1b[?9;1000;1002;1003;1006;1016l\x1b]8;;\x1b\\'

export function writeTerminalStatus(terminal: Terminal, data: string, parsed: () => void) {
  const normal = terminal.buffer.normal
  let lastRow = normal.cursorY
  for (let row = terminal.rows - 1; row > lastRow; row--) {
    if (normal.getLine(normal.baseY + row)?.translateToString(true)) {
      lastRow = row
      break
    }
  }

  // 全屏程序的备用屏幕没有滚动历史，退出前将最后可见画面作为文本追加。
  // 快照仅包含当前屏幕，仍由同一个 xterm 按用户设置的行数裁剪。
  const snapshot: string[] = []
  if (terminal.buffer.active.type === 'alternate') {
    for (let row = 0; row < terminal.rows; row++) {
      snapshot.push(terminal.buffer.active.getLine(row)?.translateToString(true) ?? '')
    }
    while (snapshot.length && !snapshot.at(-1)) snapshot.pop()
  }
  const savedScreen = snapshot.length ? `\r\n${snapshot.join('\r\n')}` : ''
  terminal.write(`${restoreModes}\x1b[${lastRow + 1};1H${savedScreen}${data}`, parsed)
}
