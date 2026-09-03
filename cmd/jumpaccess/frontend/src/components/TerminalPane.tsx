import { useEffect, useRef, useState } from 'react'
import { FitAddon } from '@xterm/addon-fit'
import { Terminal } from '@xterm/xterm'
import { ClipboardCopy, ClipboardPaste } from 'lucide-react'
import '@xterm/xterm/css/xterm.css'

import type { Backend, Preferences, SessionState } from '../lib/backend'
import { synchronizeTerminalViewportBackground } from './terminalViewport'

interface TerminalPaneProps {
  backend: Backend
  onActionsChange?: (actions: TerminalActions | null) => void
  onCurrentDirectoryChange?: (directory: string) => void
  onReconnect?: () => void
  output: string
  preferences: Preferences
  session: SessionState
}

export interface TerminalActions {
  canCopy: boolean
  copy: () => Promise<void>
  paste: () => Promise<void>
}

const osc7PayloadLimit = 4096
const contextMenuWidth = 168
const contextMenuHeight = 84

interface ContextMenuPosition {
  left: number
  top: number
}

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

export function TerminalPane({ backend, onActionsChange, onCurrentDirectoryChange, onReconnect, output, preferences, session }: TerminalPaneProps) {
  const paneRef = useRef<HTMLDivElement>(null)
  const hostRef = useRef<HTMLDivElement>(null)
  const menuRef = useRef<HTMLDivElement>(null)
  const terminalRef = useRef<Terminal | null>(null)
  const writtenRef = useRef(0)
  const historyReplayRef = useRef(false)
  const currentDirectoryRef = useRef('')
  const currentDirectoryChangeRef = useRef(onCurrentDirectoryChange)
  const actionsChangeRef = useRef(onActionsChange)
  const reconnectRef = useRef(onReconnect)
  const rightClickActionRef = useRef(preferences.terminalRightClickAction)
  const statusRef = useRef(session.status)
  const [contextMenu, setContextMenu] = useState<ContextMenuPosition | null>(null)
  currentDirectoryChangeRef.current = onCurrentDirectoryChange
  actionsChangeRef.current = onActionsChange
  reconnectRef.current = onReconnect
  rightClickActionRef.current = preferences.terminalRightClickAction
  statusRef.current = session.status

  async function pasteFromClipboard() {
    const terminal = terminalRef.current
    if (!terminal || statusRef.current !== 'active') return
    try {
      const text = await navigator.clipboard?.readText()
      if (text && terminalRef.current === terminal && statusRef.current === 'active') terminal.paste(text)
    } catch {
      // Clipboard access can be denied by the host WebView; leave the terminal unchanged.
    } finally {
      terminal.focus()
    }
  }

  async function copySelection() {
    const terminal = terminalRef.current
    const selection = terminal?.getSelection() ?? ''
    setContextMenu(null)
    if (!terminal || !selection) return
    try {
      await navigator.clipboard?.writeText(selection)
    } catch {
      // Clipboard access can be denied by the host WebView; leave the selection intact.
    } finally {
      terminal.focus()
    }
  }

  function pasteFromContextMenu() {
    setContextMenu(null)
    void pasteFromClipboard()
  }

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
    const reportActions = () => actionsChangeRef.current?.({
      canCopy: terminal.hasSelection(),
      copy: copySelection,
      paste: pasteFromClipboard,
    })
    const selectionChanged = terminal.onSelectionChange(reportActions)
    reportActions()
    const onContextMenu = (event: MouseEvent) => {
      event.preventDefault()
      if (rightClickActionRef.current === 'paste') {
        setContextMenu(null)
        void pasteFromClipboard()
        return
      }
      const bounds = paneRef.current?.getBoundingClientRect()
      const left = Math.max(4, Math.min(event.clientX - (bounds?.left ?? 0), (bounds?.width ?? contextMenuWidth + 8) - contextMenuWidth - 4))
      const top = Math.max(4, Math.min(event.clientY - (bounds?.top ?? 0), (bounds?.height ?? contextMenuHeight + 8) - contextMenuHeight - 4))
      setContextMenu({ left, top })
    }
    host.addEventListener('contextmenu', onContextMenu)
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
      if (event.type === 'keydown' && event.key === 'Insert' && !event.altKey && !event.metaKey) {
        if (event.ctrlKey && !event.shiftKey) {
          event.preventDefault()
          void copySelection()
          return false
        }
        if (event.shiftKey && !event.ctrlKey) {
          event.preventDefault()
          void pasteFromClipboard()
          return false
        }
      }
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
      host.removeEventListener('contextmenu', onContextMenu)
      selectionChanged.dispose()
      osc7.dispose()
      input.dispose()
      resized.dispose()
      terminal.dispose()
      terminalRef.current = null
      actionsChangeRef.current?.(null)
      historyReplayRef.current = false
    }
  }, [backend, preferences.terminalFontFamily, preferences.terminalFontSize, preferences.theme, session.id])

  useEffect(() => {
    setContextMenu(null)
  }, [preferences.terminalRightClickAction, session.id])

  useEffect(() => {
    if (!contextMenu) return
    menuRef.current?.querySelector<HTMLButtonElement>('button:not(:disabled)')?.focus()
    const dismiss = (event: PointerEvent) => {
      if (!menuRef.current?.contains(event.target as Node)) setContextMenu(null)
    }
    const dismissOnEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setContextMenu(null)
        terminalRef.current?.focus()
      }
    }
    const dismissOnBlur = () => setContextMenu(null)
    document.addEventListener('pointerdown', dismiss, true)
    document.addEventListener('keydown', dismissOnEscape)
    window.addEventListener('blur', dismissOnBlur)
    return () => {
      document.removeEventListener('pointerdown', dismiss, true)
      document.removeEventListener('keydown', dismissOnEscape)
      window.removeEventListener('blur', dismissOnBlur)
    }
  }, [contextMenu])

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

  const hasSelection = terminalRef.current?.hasSelection() ?? false
  const canPaste = session.status === 'active'
  return <div className="terminal-pane" ref={paneRef}>
    <div className="terminal-host" ref={hostRef} aria-label={`${session.title} SSH 终端`} />
    {contextMenu ? <div
      aria-label="终端上下文菜单"
      className="terminal-context-menu"
      onContextMenu={(event) => event.preventDefault()}
      ref={menuRef}
      role="menu"
      style={contextMenu}
    >
      <button disabled={!hasSelection} onClick={() => void copySelection()} role="menuitem" type="button"><ClipboardCopy />复制</button>
      <button disabled={!canPaste} onClick={pasteFromContextMenu} role="menuitem" type="button"><ClipboardPaste />粘贴</button>
    </div> : null}
  </div>
}
