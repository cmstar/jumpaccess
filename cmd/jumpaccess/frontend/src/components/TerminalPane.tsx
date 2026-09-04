import { useEffect, useRef, useState } from 'react'
import { FitAddon } from '@xterm/addon-fit'
import { Terminal } from '@xterm/xterm'
import { ClipboardCopy, ClipboardPaste, TriangleAlert, X } from 'lucide-react'
import '@xterm/xterm/css/xterm.css'

import type { Backend, Preferences, SessionState } from '../lib/backend'
import { synchronizeTerminalViewportBackground } from './terminalViewport'
import { terminalDisplayOptions } from '../model/terminalTheme'

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

interface PendingPaste {
  sessionID: string
  text: string
}

function textForTerminalInput(text: string): string {
  return text.replace(/\r?\n/g, '\r')
}

function pastedLineCount(text: string): number {
  return text.split(/\r\n|\r|\n/).length
}

function hasTrailingLineBreak(text: string): boolean {
  return /[\r\n]$/.test(text)
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
  const initialDisplay = useRef(preferences)
  const fitRef = useRef<(() => void) | null>(null)
  const writtenRef = useRef(0)
  const historyReplayRef = useRef(false)
  const currentDirectoryRef = useRef('')
  const currentDirectoryChangeRef = useRef(onCurrentDirectoryChange)
  const actionsChangeRef = useRef(onActionsChange)
  const reconnectRef = useRef(onReconnect)
  const rightClickActionRef = useRef(preferences.terminalRightClickAction)
  const warnOnMultiLinePasteRef = useRef(preferences.terminalWarnOnMultiLinePaste)
  const statusRef = useRef(session.status)
  const [contextMenu, setContextMenu] = useState<ContextMenuPosition | null>(null)
  const [pendingPaste, setPendingPaste] = useState<PendingPaste | null>(null)
  const pendingPasteRef = useRef<PendingPaste | null>(null)
  const clipboardReadInFlightRef = useRef(false)
  currentDirectoryChangeRef.current = onCurrentDirectoryChange
  actionsChangeRef.current = onActionsChange
  reconnectRef.current = onReconnect
  rightClickActionRef.current = preferences.terminalRightClickAction
  warnOnMultiLinePasteRef.current = preferences.terminalWarnOnMultiLinePaste
  statusRef.current = session.status

  function closePasteWarning(refocus: boolean) {
    pendingPasteRef.current = null
    setPendingPaste(null)
    if (refocus) requestAnimationFrame(() => terminalRef.current?.focus())
  }

  function sendPastedText(text: string, expectedSessionID: string) {
    const terminal = terminalRef.current
    if (!terminal || statusRef.current !== 'active' || session.id !== expectedSessionID) return
    terminal.input(textForTerminalInput(text), true)
    terminal.focus()
  }

  async function requestPaste(providedText?: string) {
    const terminal = terminalRef.current
    const readsClipboard = providedText === undefined
    if (!terminal || statusRef.current !== 'active' || pendingPasteRef.current || (readsClipboard && clipboardReadInFlightRef.current)) return
    if (readsClipboard) clipboardReadInFlightRef.current = true
    let refocus = true
    try {
      const text = providedText ?? await navigator.clipboard?.readText()
      if (!text || terminalRef.current !== terminal || statusRef.current !== 'active') return
      if (warnOnMultiLinePasteRef.current && /[\r\n]/.test(text)) {
        const pending = { sessionID: session.id, text }
        pendingPasteRef.current = pending
        setPendingPaste(pending)
        refocus = false
        return
      }
      sendPastedText(text, session.id)
    } catch {
      // Clipboard access can be denied by the host WebView; leave the terminal unchanged.
    } finally {
      if (readsClipboard) clipboardReadInFlightRef.current = false
      if (refocus) terminal.focus()
    }
  }

  function confirmPendingPaste() {
    const pending = pendingPasteRef.current
    if (!pending) return
    pendingPasteRef.current = null
    setPendingPaste(null)
    sendPastedText(pending.text, pending.sessionID)
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
    void requestPaste()
  }

  useEffect(() => {
    const host = hostRef.current
    if (!host) return
    const display = terminalDisplayOptions(initialDisplay.current)
    const theme = display.theme
    const terminal = new Terminal({
      ...display,
      allowProposedApi: false,
      convertEol: false,
      scrollback: 10_000,
    })
    const fit = new FitAddon()
    terminal.loadAddon(fit)
    terminal.open(host)
    synchronizeTerminalViewportBackground(host, theme.background)
    terminalRef.current = terminal
    const reportActions = () => actionsChangeRef.current?.({
      canCopy: terminal.hasSelection(),
      copy: copySelection,
      paste: requestPaste,
    })
    const selectionChanged = terminal.onSelectionChange(reportActions)
    reportActions()
    const onContextMenu = (event: MouseEvent) => {
      event.preventDefault()
      if (rightClickActionRef.current === 'paste') {
        setContextMenu(null)
        void requestPaste()
        return
      }
      const bounds = paneRef.current?.getBoundingClientRect()
      const left = Math.max(4, Math.min(event.clientX - (bounds?.left ?? 0), (bounds?.width ?? contextMenuWidth + 8) - contextMenuWidth - 4))
      const top = Math.max(4, Math.min(event.clientY - (bounds?.top ?? 0), (bounds?.height ?? contextMenuHeight + 8) - contextMenuHeight - 4))
      setContextMenu({ left, top })
    }
    host.addEventListener('contextmenu', onContextMenu)
    const onPaste = (event: ClipboardEvent) => {
      const text = event.clipboardData?.getData('text/plain')
      if (!text) return
      event.preventDefault()
      event.stopPropagation()
      void requestPaste(text)
    }
    host.addEventListener('paste', onPaste, true)
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
          void requestPaste()
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
    fitRef.current = fitAndReport
    observer.observe(host)
    requestAnimationFrame(() => {
      fitAndReport()
      terminal.focus()
    })
    return () => {
      observer.disconnect()
      host.removeEventListener('contextmenu', onContextMenu)
      host.removeEventListener('paste', onPaste, true)
      selectionChanged.dispose()
      osc7.dispose()
      input.dispose()
      resized.dispose()
      terminal.dispose()
      terminalRef.current = null
      fitRef.current = null
      actionsChangeRef.current?.(null)
      pendingPasteRef.current = null
      historyReplayRef.current = false
    }
  }, [backend, session.id])

  useEffect(() => {
    const terminal = terminalRef.current
    const host = hostRef.current
    if (!terminal || !host) return
    const display = terminalDisplayOptions(preferences)
    Object.assign(terminal.options, display)
    synchronizeTerminalViewportBackground(host, display.theme.background)
    fitRef.current?.()
  }, [preferences.terminalColorScheme, preferences.terminalFontFamily, preferences.terminalFontSize, preferences.terminalLineHeight, preferences.terminalCursorStyle, preferences.terminalCursorBlink, session.id])

  useEffect(() => {
    setContextMenu(null)
  }, [preferences.terminalRightClickAction, session.id])

  useEffect(() => {
    if (session.status !== 'active' && pendingPasteRef.current) closePasteWarning(false)
  }, [session.status])

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
    <div className="terminal-host" data-terminal-cursor-style={preferences.terminalCursorStyle} ref={hostRef} aria-label={`${session.title} SSH 终端`} />
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
    {pendingPaste ? <div
      className="modal-backdrop"
      onKeyDown={(event) => { if (event.key === 'Escape') closePasteWarning(true) }}
      onMouseDown={(event) => { if (event.target === event.currentTarget) closePasteWarning(true) }}
    >
      <section aria-labelledby="terminal-paste-warning-title" aria-modal="true" className="modal terminal-paste-warning" role="dialog">
        <button aria-label="关闭" className="modal-close icon-button" onClick={() => closePasteWarning(true)} type="button"><X /></button>
        <header>
          <h2 id="terminal-paste-warning-title">多行粘贴警告</h2>
          <p>剪贴板内容包含换行，粘贴到 SSH 终端后，其中的命令可能立即执行。</p>
        </header>
        <div className="terminal-paste-warning-summary"><TriangleAlert /><span><strong>{pastedLineCount(pendingPaste.text)} 行</strong><small>{pendingPaste.text.length} 个字符{hasTrailingLineBreak(pendingPaste.text) ? ' · 末尾包含换行' : ''}</small></span></div>
        <div className="terminal-paste-preview">
          <span>剪贴板内容预览</span>
          <pre aria-label="剪贴板内容预览">{pendingPaste.text}</pre>
        </div>
        <div className="dialog-actions"><button autoFocus className="button secondary" onClick={() => closePasteWarning(true)} type="button">取消</button><button className="button primary" onClick={confirmPendingPaste} type="button"><ClipboardPaste />仍然粘贴</button></div>
      </section>
    </div> : null}
  </div>
}
