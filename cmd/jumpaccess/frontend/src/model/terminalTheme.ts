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

// 预览和会话使用同一组渲染参数；UI 的应用主题不参与终端配色。
export function terminalDisplayOptions(preferences: Pick<Preferences, 'terminalColorScheme' | 'terminalFontFamily' | 'terminalFontSize' | 'terminalLineHeight' | 'terminalCursorStyle' | 'terminalCursorBlink'>) {
  return {
    fontFamily: preferences.terminalFontFamily,
    fontSize: preferences.terminalFontSize,
    lineHeight: preferences.terminalLineHeight,
    // 底部方块借用原生下划线的颜色与闪烁，由宿主的样式标记调整厚度。
    cursorStyle: preferences.terminalCursorStyle === 'quarter_block' ? 'underline' : preferences.terminalCursorStyle,
    cursorBlink: preferences.terminalCursorBlink,
    theme: { ...terminalScheme(preferences.terminalColorScheme).theme },
  } satisfies ITerminalOptions
}
