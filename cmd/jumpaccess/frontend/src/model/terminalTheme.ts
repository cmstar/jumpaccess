import type { ITheme } from '@xterm/xterm'
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
export function terminalDisplayOptions(preferences: { terminalColorScheme: string; terminalFontFamily: string; terminalFontSize: number }) {
  return {
    fontFamily: preferences.terminalFontFamily,
    fontSize: preferences.terminalFontSize,
    theme: { ...terminalScheme(preferences.terminalColorScheme).theme },
  }
}
