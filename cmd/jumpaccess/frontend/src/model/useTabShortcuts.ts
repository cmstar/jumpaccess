import { useEffect, useRef } from 'react'

export function useTabShortcuts(onCycle: (direction: 1 | -1) => void) {
  const cycle = useRef(onCycle)
  cycle.current = onCycle

  useEffect(() => {
    let held = false
    const handle = (event: KeyboardEvent) => {
      if (event.key !== 'Tab') return
      // 新的独立按键清理丢失 keyup 的状态；已消费按键的残余事件不能进入终端。
      if (event.type === 'keydown' && !event.repeat) held = false
      const shortcut = event.ctrlKey && !event.metaKey && !event.altKey
      if (!held && (!shortcut || event.isComposing || event.keyCode === 229)) return
      event.preventDefault()
      event.stopImmediatePropagation()
      if (event.type === 'keyup') { held = false; return }
      if (event.type !== 'keydown') return
      held = true
      if (!shortcut || event.isComposing || event.keyCode === 229 || document.querySelector('[role="dialog"]')) return
      cycle.current(event.shiftKey ? -1 : 1)
    }
    const reset = () => { held = false }
    for (const type of ['keydown', 'keypress', 'keyup'] as const) window.addEventListener(type, handle, true)
    window.addEventListener('blur', reset)
    return () => {
      for (const type of ['keydown', 'keypress', 'keyup'] as const) window.removeEventListener(type, handle, true)
      window.removeEventListener('blur', reset)
    }
  }, [])
}
