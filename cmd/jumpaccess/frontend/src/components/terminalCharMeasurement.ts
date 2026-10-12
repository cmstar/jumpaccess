import type { Terminal } from '@xterm/xterm'

interface MeasurementStrategy {
  measure(): { width: number; height: number }
}

interface CharSizeService {
  _measureStrategy: MeasurementStrategy
  measure(): void
}

// xterm 6.0.0 没有公开的字符度量扩展点，将私有接口依赖限制在此处。
// 升级 xterm 时必须运行使用真实 Terminal 的字体度量回归测试。
export function createTerminalDomMeasurement(terminal: Terminal) {
  const element = terminal.element
  const navigator = element?.ownerDocument.defaultView?.navigator
  if (!element || !navigator?.platform.toLowerCase().includes('mac')
    || !/AppleWebKit\//.test(navigator.userAgent)
    || /(?:Chrome|Chromium|Edg|OPR)\//.test(navigator.userAgent)) return null

  const service = (terminal as unknown as { _core?: { _charSizeService?: CharSizeService } })._core?._charSizeService
  if (!service || typeof service.measure !== 'function' || typeof service._measureStrategy?.measure !== 'function') {
    console.warn('无法启用终端 DOM 字符测量，请检查 xterm 版本兼容性。')
    return null
  }

  const original = service._measureStrategy
  const probe = element.ownerDocument.createElement('span')
  probe.className = 'xterm-char-measure-element terminal-dom-char-measure'
  probe.setAttribute('aria-hidden', 'true')
  probe.textContent = 'W'.repeat(32)
  Object.assign(probe.style, { whiteSpace: 'pre', fontKerning: 'none', letterSpacing: 'normal', lineHeight: 'normal', fontStyle: 'normal' })
  element.append(probe)
  let size = { width: 0, height: 0 }
  const strategy: MeasurementStrategy = {
    measure() {
      probe.style.fontFamily = terminal.options.fontFamily!
      probe.style.fontSize = `${terminal.options.fontSize}px`
      probe.style.fontWeight = String(terminal.options.fontWeight)
      // offset 尺寸不受祖先 transform 影响；重复字符减少整数取整造成的误差。
      const width = probe.offsetWidth / 32
      const height = probe.offsetHeight
      // 隐藏面板保留上次有效 DOM 度量；首次隐藏时保持无效，不能沿用 Canvas 尺寸。
      if (width > 0 && height > 0) size = { width, height }
      return size
    },
  }
  service._measureStrategy = strategy
  return {
    // 沿用 xterm 的尺寸变更事件，让渲染、鼠标坐标和字符网格使用同一组尺寸。
    measure: () => service.measure(),
    dispose() {
      if (service._measureStrategy === strategy) service._measureStrategy = original
      probe.remove()
    },
  }
}
