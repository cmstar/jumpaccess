import { FitAddon } from '@xterm/addon-fit'
import type { Terminal } from '@xterm/xterm'

// addon-fit 0.11 在 scrollback 非零时始终预留滚动条宽度。
// 隐藏时按实际字符格宽度重新计算列数，保留历史缓冲区和原有 fit 生命周期。
export class TerminalFitAddon extends FitAddon {
  private targetTerminal?: Terminal

  override activate(terminal: Terminal) {
    super.activate(terminal)
    this.targetTerminal = terminal
  }

  override proposeDimensions() {
    const dimensions = super.proposeDimensions()
    const terminal = this.targetTerminal
    const element = terminal?.element
    const host = element?.parentElement
    if (!dimensions || !terminal || !element || !host || host.dataset.terminalShowScrollbar !== 'false' || terminal.options.scrollback === 0) return dimensions

    const screen = element.querySelector('.xterm-screen')
    const cellWidth = (screen?.getBoundingClientRect().width ?? 0) / terminal.cols
    const style = getComputedStyle(element)
    const width = parseFloat(getComputedStyle(host).width) - parseFloat(style.paddingLeft) - parseFloat(style.paddingRight)
    if (Number.isFinite(cellWidth) && cellWidth > 0 && Number.isFinite(width) && width > 0) {
      dimensions.cols = Math.max(2, Math.floor(width / cellWidth))
    }
    return dimensions
  }
}
