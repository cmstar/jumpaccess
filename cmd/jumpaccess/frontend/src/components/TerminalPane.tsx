import { useEffect, useRef } from 'react'
import { FitAddon } from '@xterm/addon-fit'
import { Terminal } from '@xterm/xterm'
import '@xterm/xterm/css/xterm.css'

import type { Backend, Preferences, SessionState } from '../lib/backend'
import { synchronizeTerminalViewportBackground } from './terminalViewport'

interface TerminalPaneProps {
  backend: Backend
  onCurrentDirectoryChange?: (directory: string) => void
  onReconnect?: () => void
  output: string
  preferences: Preferences
  session: SessionState
}

const osc7PayloadLimit = 4096

function currentDirectoryFromOSC7(payload: string): string | undefined {
  if (payload.length === 0 || payload.length > osc7PayloadLimit || !payload.startsWith('file://')) return undefined
  const pathStart = payload.indexOf('/', 'file://'.length)
  if (pathStart < 0) return undefined
  const rawPath = payload.slice(pathStart)
  if (rawPath.includes('?') || rawPath.includes('#')) return undefined
  try {
    const directory = decodeURIComponent(rawPath)
    if (!directory.startsWith('/') || directory.split('/').includes('..') || /[\u0000-\u001f\u007f]/u.test(directory)) return undefined
    return directory
  } catch {
    return undefined
  }
}

export function TerminalPane({ backend, onCurrentDirectoryChange, onReconnect, output, preferences, session }: TerminalPaneProps) {
  const hostRef = useRef<HTMLDivElement>(null)
  const terminalRef = useRef<Terminal | null>(null)
  const writtenRef = useRef(0)
  const historyReplayRef = useRef(false)
  const currentDirectoryRef = useRef('')
  const currentDirectoryChangeRef = useRef(onCurrentDirectoryChange)
  const reconnectRef = useRef(onReconnect)
  const statusRef = useRef(session.status)
  currentDirectoryChangeRef.current = onCurrentDirectoryChange
  reconnectRef.current = onReconnect
  statusRef.current = session.status

  useEffect(() => {
    const host = hostRef.current
    if (!host) return
    const dark = document.documentElement.classList.contains('dark')
    const theme = dark
      ? { background: '#101817', foreground: '#d8e7e1', cursor: '#63d9ae', selectionBackground: '#315d4d' }
      : { background: '#f7faf8', foreground: '#263d35', cursor: '#16825e', selectionBackground: '#bde9d8' }
    const terminal = new Terminal({
      allowProposedApi: false,
      convertEol: false,
      cursorBlink: true,
      fontFamily: preferences.terminalFontFamily,
      fontSize: preferences.terminalFontSize,
      scrollback: 10_000,
      theme,
    })
    const fit = new FitAddon()
    terminal.loadAddon(fit)
    terminal.open(host)
    synchronizeTerminalViewportBackground(host, theme.background)
    terminalRef.current = terminal
    writtenRef.current = 0
    historyReplayRef.current = true
    currentDirectoryRef.current = ''
    const osc7 = terminal.parser.registerOscHandler(7, (payload) => {
      if (statusRef.current !== 'active') return false
      const directory = currentDirectoryFromOSC7(payload)
      if (!directory) return false
      if (directory !== currentDirectoryRef.current) {
        currentDirectoryRef.current = directory
        currentDirectoryChangeRef.current?.(directory)
      }
      return true
    })
    const fitAndReport = () => {
      try {
        fit.fit()
        if (session.id) {
          void backend.resizeSSHSession(session.id, terminal.cols, terminal.rows)
        }
      } catch {
        // WebView may report a zero-sized host while switching views; the next resize retries.
      }
    }
    terminal.attachCustomKeyEventHandler((event) => {
      if (statusRef.current === 'active') return true
      const disconnected = statusRef.current === 'closed' || statusRef.current === 'failed'
      if (disconnected && event.type === 'keydown' && event.key === 'Enter' && !event.ctrlKey && !event.altKey && !event.shiftKey && !event.metaKey) {
        event.preventDefault()
        reconnectRef.current?.()
      }
      return false
    })
    const input = terminal.onData((data) => {
      if (statusRef.current === 'active' && !historyReplayRef.current) void backend.writeSSHSession(session.id, data)
    })
    const resized = terminal.onResize(({ cols, rows }) => {
      if (session.id) void backend.resizeSSHSession(session.id, cols, rows)
    })
    const observer = new ResizeObserver(fitAndReport)
    observer.observe(host)
    requestAnimationFrame(() => {
      fitAndReport()
      terminal.focus()
    })
    return () => {
      observer.disconnect()
      osc7.dispose()
      input.dispose()
      resized.dispose()
      terminal.dispose()
      terminalRef.current = null
      historyReplayRef.current = false
    }
  }, [backend, preferences.terminalFontFamily, preferences.terminalFontSize, preferences.theme, session.id])

  useEffect(() => {
    const terminal = terminalRef.current
    if (!terminal) return
    if (output.length < writtenRef.current) {
      terminal.reset()
      writtenRef.current = 0
      historyReplayRef.current = true
    }
    const next = output.slice(writtenRef.current)
    if (next) {
      terminal.write(next, () => {
        if (terminalRef.current === terminal) historyReplayRef.current = false
      })
    } else {
      historyReplayRef.current = false
    }
    writtenRef.current = output.length
  }, [output])

  return <div className="terminal-host" ref={hostRef} aria-label={`${session.title} SSH 终端`} />
}
