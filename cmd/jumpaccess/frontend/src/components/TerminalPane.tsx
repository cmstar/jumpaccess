import { useEffect, useRef } from 'react'
import { FitAddon } from '@xterm/addon-fit'
import { Terminal } from '@xterm/xterm'
import '@xterm/xterm/css/xterm.css'

import type { Backend, Preferences, SessionState } from '../lib/backend'

interface TerminalPaneProps {
  backend: Backend
  output: string
  preferences: Preferences
  session: SessionState
}

export function TerminalPane({ backend, output, preferences, session }: TerminalPaneProps) {
  const hostRef = useRef<HTMLDivElement>(null)
  const terminalRef = useRef<Terminal | null>(null)
  const writtenRef = useRef(0)

  useEffect(() => {
    const host = hostRef.current
    if (!host) return
    const dark = document.documentElement.classList.contains('dark')
    const terminal = new Terminal({
      allowProposedApi: false,
      convertEol: false,
      cursorBlink: true,
      fontFamily: preferences.terminalFontFamily,
      fontSize: preferences.terminalFontSize,
      scrollback: 10_000,
      theme: dark
        ? { background: '#101817', foreground: '#d8e7e1', cursor: '#63d9ae', selectionBackground: '#315d4d' }
        : { background: '#f7faf8', foreground: '#263d35', cursor: '#16825e', selectionBackground: '#bde9d8' },
    })
    const fit = new FitAddon()
    terminal.loadAddon(fit)
    terminal.open(host)
    terminalRef.current = terminal
    writtenRef.current = 0
    const fitAndReport = () => {
      try {
        fit.fit()
        void backend.resizeSSHSession(session.id, terminal.cols, terminal.rows)
      } catch {
        // WebView may report a zero-sized host while switching views; the next resize retries.
      }
    }
    const input = terminal.onData((data) => void backend.writeSSHSession(session.id, data))
    const resized = terminal.onResize(({ cols, rows }) => void backend.resizeSSHSession(session.id, cols, rows))
    const observer = new ResizeObserver(fitAndReport)
    observer.observe(host)
    requestAnimationFrame(() => {
      fitAndReport()
      terminal.focus()
    })
    return () => {
      observer.disconnect()
      input.dispose()
      resized.dispose()
      terminal.dispose()
      terminalRef.current = null
    }
  }, [backend, preferences.terminalFontFamily, preferences.terminalFontSize, preferences.theme, session.id])

  useEffect(() => {
    const terminal = terminalRef.current
    if (!terminal) return
    if (output.length < writtenRef.current) {
      terminal.reset()
      writtenRef.current = 0
    }
    const next = output.slice(writtenRef.current)
    if (next) terminal.write(next)
    writtenRef.current = output.length
  }, [output])

  return <div className="terminal-host" ref={hostRef} aria-label={`${session.title} SSH 终端`} />
}
