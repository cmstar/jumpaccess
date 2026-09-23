import { useCallback, useEffect, useRef, useState } from 'react'
import type { Backend } from '../lib/backend'

export function useTerminalFullscreen(backend: Backend, tab: { id: string; kind: string } | undefined, onError: (message: string) => void, onEnter: () => void) {
  const [fullscreen, setFullscreen] = useState(false)
  const current = useRef({ tab, onError, onEnter })
  current.current = { tab, onError, onEnter }
  const active = useRef(false)
  const owner = useRef('')
  const revision = useRef(0)
  const pending = useRef<Promise<boolean> | null>(null)
  const mounted = useRef(true)
  useEffect(() => { mounted.current = true; return () => { mounted.current = false } }, [])

  const change = useCallback((enabled: boolean): Promise<boolean> => {
    if (pending.current) return pending.current
    revision.current += 1
    const requestedTab = current.current.tab?.id ?? ''
    const operation = (async () => {
      try {
        await backend.setWindowFullscreen(enabled)
        active.current = enabled
        owner.current = enabled ? requestedTab : ''
        if (mounted.current) {
          setFullscreen(enabled)
          if (enabled) current.current.onEnter()
          requestAnimationFrame(() => {
            if (!mounted.current || document.querySelector('[role="dialog"]')) return
            document.querySelector<HTMLTextAreaElement>('.terminal-host .xterm-helper-textarea')?.focus()
          })
        }
        return true
      } catch (reason) {
        if (mounted.current) current.current.onError(reason instanceof Error ? reason.message : String(reason))
        return false
      } finally {
        pending.current = null
      }
    })()
    pending.current = operation
    return operation
  }, [backend])

  const runWindowed = useCallback(async (action: () => void | Promise<void>) => {
    if (pending.current && !await pending.current) return
    if (active.current && !await change(false)) return
    await action()
  }, [change])

  useEffect(() => {
    const held = new Set<string>()
    const isWindows = !navigator.platform.toLowerCase().includes('mac')
    const handle = (event: KeyboardEvent) => {
      const key = event.code || event.key
      const shortcut = !event.ctrlKey && !event.metaKey && !event.shiftKey && (
        (event.key === 'F11' && !event.altKey) || (isWindows && event.key === 'Enter' && event.altKey)
      )
      const canEnter = current.current.tab?.kind === 'ssh'
      if (!held.has(key)) {
        if (!shortcut || (!active.current && !canEnter) || event.isComposing || event.keyCode === 229) return
        // 模态窗口内不进入全屏；已经全屏时仍保留退出入口。
        if (!active.current && document.querySelector('[role="dialog"]')) return
      }
      event.preventDefault()
      event.stopImmediatePropagation()
      if (event.type === 'keyup') { held.delete(key); return }
      if (event.type !== 'keydown') return
      held.add(key)
      if (!event.repeat && !pending.current) void change(!active.current)
    }
    const reset = () => held.clear()
    for (const type of ['keydown', 'keypress', 'keyup'] as const) window.addEventListener(type, handle, true)
    window.addEventListener('blur', reset)
    return () => {
      for (const type of ['keydown', 'keypress', 'keyup'] as const) window.removeEventListener(type, handle, true)
      window.removeEventListener('blur', reset)
    }
  }, [change])

  useEffect(() => {
    if (fullscreen && (tab?.kind !== 'ssh' || tab.id !== owner.current)) void change(false)
  }, [fullscreen, tab?.id, tab?.kind, change])

  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | undefined
    let disposed = false
    const synchronize = () => {
      clearTimeout(timer)
      timer = setTimeout(() => {
        if (!active.current || pending.current) return
        if (!window.runtime?.WindowIsFullscreen) return
        const observedRevision = revision.current
        void Promise.all([window.runtime.WindowIsFullscreen(), window.runtime.WindowIsMinimised?.() ?? false]).then(([value, minimized]) => {
          // 系统退出全屏也必须恢复隐藏的应用界面与窗口边界。
          if (!disposed && observedRevision === revision.current && !pending.current && active.current && !value && !minimized) void change(false)
        }).catch(() => undefined)
      }, 150)
    }
    window.addEventListener('resize', synchronize)
    window.addEventListener('focus', synchronize)
    return () => {
      disposed = true
      clearTimeout(timer)
      window.removeEventListener('resize', synchronize)
      window.removeEventListener('focus', synchronize)
    }
  }, [change])

  return { fullscreen, exit: () => change(false), runWindowed }
}
