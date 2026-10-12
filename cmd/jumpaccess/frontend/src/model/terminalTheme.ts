import type { ITerminalOptions, ITheme } from '@xterm/xterm'
import type { Preferences } from '../lib/backend'
import catalog from '../../../../../internal/guiconfig/terminal-schemes.json'

export interface TerminalScheme {
  id: string
  name: string
  kind: 'dark' | 'light'
  source: string
  license: string
  theme: ITheme & { background: string; foreground: string }
}

export const terminalSchemes = catalog as TerminalScheme[]
export const ansiColorKeys = [
  'black', 'red', 'green', 'yellow', 'blue', 'magenta', 'cyan', 'white',
  'brightBlack', 'brightRed', 'brightGreen', 'brightYellow', 'brightBlue', 'brightMagenta', 'brightCyan', 'brightWhite',
] as const

export function terminalScheme(id: string): TerminalScheme {
  return terminalSchemes.find((scheme) => scheme.id === id) ?? terminalSchemes[0]
}

function terminalFontFamily(value: string): string {
  // 保留已有 CSS 写法，包括引号、通用字体族和回退列表；离屏检查不触发布局。
  const style = document.createElement('span').style
  style.fontFamily = value
  if (style.fontFamily) return value
  // 系统枚举返回原始名称，数字开头或含特殊字符时需转为 CSS 字符串。
  return `"${value.replace(/["\\]/g, '\\$&')}"`
}

// 预览和会话使用同一组渲染参数；UI 的应用主题不参与终端配色。
export function terminalDisplayOptions(preferences: Pick<Preferences, 'terminalColorScheme' | 'terminalFontFamily' | 'terminalFontSize' | 'terminalLineHeight' | 'terminalCursorStyle' | 'terminalCursorBlink'>, backgroundVisible = false) {
  const theme = { ...terminalScheme(preferences.terminalColorScheme).theme }
  const scrollbarThumb = `${theme.foreground.slice(0, 7)}80`
  theme.scrollbarSliderBackground = scrollbarThumb
  theme.scrollbarSliderHoverBackground = scrollbarThumb
  theme.scrollbarSliderActiveBackground = scrollbarThumb
  if (backgroundVisible) theme.background = `${theme.background.slice(0, 7)}00`
  return {
    // 创建时启用，后续开关背景只更新 theme，不重建终端或会话。
    allowTransparency: true,
    fontFamily: terminalFontFamily(preferences.terminalFontFamily),
    fontSize: preferences.terminalFontSize,
    lineHeight: preferences.terminalLineHeight,
    // 底部方块借用原生下划线的颜色与闪烁，由宿主的样式标记调整厚度。
    cursorStyle: preferences.terminalCursorStyle === 'quarter_block' ? 'underline' : preferences.terminalCursorStyle,
    cursorBlink: preferences.terminalCursorBlink,
    theme,
  } satisfies ITerminalOptions
}
