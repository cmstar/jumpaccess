import { useEffect, useRef } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import type { Preferences } from '../lib/backend'
import { terminalDisplayOptions, terminalScheme } from '../model/terminalTheme'
import { synchronizeTerminalViewportBackground } from './terminalViewport'

const sample = [
  '\x1b[32muser@jumpaccess\x1b[0m:\x1b[34m~/demo\x1b[0m $ ls',
  '\x1b[34mDocuments/\x1b[0m  \x1b[32mrun.sh\x1b[0m  README.md  \x1b[36mlatest → release\x1b[0m',
  '',
  '\x1b[32m[OK]\x1b[0m Connected   \x1b[33m[WARN]\x1b[0m Retry   \x1b[31m[ERROR]\x1b[0m Permission denied',
  '\x1b[1mBold text\x1b[0m  中文显示  \x1b[4munderline\x1b[0m',
  '',
  Array.from({ length: 8 }, (_, i) => `\x1b[${40 + i}m  `).join('') + '\x1b[0m  ANSI',
  Array.from({ length: 8 }, (_, i) => `\x1b[${100 + i}m  `).join('') + '\x1b[0m  Bright',
  '\x1b[32muser@jumpaccess\x1b[0m:~ $ ',
].join('\r\n')

export function TerminalPreview({ preferences }: { preferences: Preferences }) {
  const host = useRef<HTMLDivElement>(null)
  const terminal = useRef<Terminal | null>(null)
  const fit = useRef<FitAddon | null>(null)
  const initial = useRef(preferences)
  const scheme = terminalScheme(preferences.terminalColorScheme)

  useEffect(() => {
    if (!host.current) return
    const instance = new Terminal({
      ...terminalDisplayOptions(initial.current),
      disableStdin: true, cursorBlink: true, cursorInactiveStyle: 'block', scrollback: 0,
    })
    const addon = new FitAddon()
    terminal.current = instance
    fit.current = addon
    instance.loadAddon(addon)
    instance.open(host.current)
    const resize = () => { try { addon.fit() } catch { /* 隐藏面板恢复尺寸后重试。 */ } }
    resize()
    instance.write(sample)
    const observer = new ResizeObserver(resize)
    observer.observe(host.current)
    return () => {
      observer.disconnect()
      instance.dispose()
      terminal.current = null
      fit.current = null
    }
  }, [])

  useEffect(() => {
    if (!terminal.current || !host.current) return
    Object.assign(terminal.current.options, terminalDisplayOptions(preferences))
    synchronizeTerminalViewportBackground(host.current, scheme.theme.background)
    try { fit.current?.fit() } catch { /* 等待可用尺寸。 */ }
  }, [preferences.terminalColorScheme, preferences.terminalFontFamily, preferences.terminalFontSize, scheme])

  return <div aria-label="终端预览" className="terminal-preview" role="region">
    <div className="terminal-preview-caption"><span>终端预览</span><span aria-live="polite">{scheme.name} · {preferences.terminalFontSize} px</span></div>
    <div className="terminal-preview-host" ref={host} style={{ height: Math.max(180, preferences.terminalFontSize * 12), background: scheme.theme.background }} />
  </div>
}
