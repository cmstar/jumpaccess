import { useEffect, useId, useLayoutEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { Check, ChevronDown } from 'lucide-react'
import { ansiColorKeys, terminalScheme, terminalSchemes, type TerminalScheme } from '../model/terminalTheme'

const groupedSchemes = ['dark', 'light'].flatMap((kind) => terminalSchemes.filter((scheme) => scheme.kind === kind))

function SchemeSwatches({ scheme }: { scheme: TerminalScheme }) {
  return <span aria-hidden="true" className="scheme-swatches" style={{ background: scheme.theme.background }}>
    {ansiColorKeys.map((key) => <i key={key} style={{ background: scheme.theme[key] }} />)}
  </span>
}

export function TerminalSchemeSelect({ value, onChange }: { value: string; onChange: (value: string) => void }) {
  const id = useId()
  const trigger = useRef<HTMLButtonElement>(null)
  const popup = useRef<HTMLDivElement>(null)
  const [open, setOpen] = useState(false)
  const [active, setActive] = useState(0)
  const [position, setPosition] = useState({ left: 0, top: 0, width: 320, maxHeight: 320 })
  const selected = terminalScheme(value)

  useLayoutEffect(() => {
    if (!open) return
    function positionPopup() {
      const rect = trigger.current?.getBoundingClientRect()
      if (!rect) return
      const below = window.innerHeight - rect.bottom - 12
      const above = rect.top - 12
      const upward = below < 240 && above > below
      const height = Math.max(80, Math.min(360, upward ? above : below))
      const width = Math.min(Math.max(rect.width, 300), window.innerWidth - 24)
      setPosition({
        left: Math.max(12, Math.min(rect.left, window.innerWidth - width - 12)),
        top: upward ? rect.top - height - 6 : rect.bottom + 6,
        width, maxHeight: height,
      })
    }
    function dismiss(event: PointerEvent) {
      if (!trigger.current?.contains(event.target as Node) && !popup.current?.contains(event.target as Node)) setOpen(false)
    }
    positionPopup()
    window.addEventListener('resize', positionPopup)
    document.addEventListener('scroll', positionPopup, true)
    document.addEventListener('pointerdown', dismiss)
    return () => {
      window.removeEventListener('resize', positionPopup)
      document.removeEventListener('scroll', positionPopup, true)
      document.removeEventListener('pointerdown', dismiss)
    }
  }, [open])

  useEffect(() => {
    const list = popup.current
    const option = list?.querySelector<HTMLElement>(`[data-index="${active}"]`)
    if (!open || !list || !option) return
    // 只滚动下拉列表，避免浏览选项时把整个设置面板一起滚走。
    if (option.offsetTop < list.scrollTop + 30) list.scrollTop = Math.max(0, option.offsetTop - 30)
    else if (option.offsetTop + option.offsetHeight > list.scrollTop + list.clientHeight) {
      list.scrollTop = option.offsetTop + option.offsetHeight - list.clientHeight
    }
  }, [active, open, position.maxHeight])

  function show() {
    setActive(Math.max(0, groupedSchemes.findIndex((scheme) => scheme.id === value)))
    setOpen(true)
  }

  function choose(next: string) {
    setOpen(false)
    if (next !== value) onChange(next)
    trigger.current?.focus()
  }

  return <div className="terminal-style-row">
    <label id={`${id}-label`}>配色方案</label>
    <button
      aria-activedescendant={open ? `${id}-option-${active}` : undefined}
      aria-controls={open ? `${id}-list` : undefined}
      aria-expanded={open}
      aria-haspopup="listbox"
      aria-labelledby={`${id}-label ${id}-value`}
      className="terminal-scheme-trigger"
      onBlur={() => setOpen(false)}
      onClick={() => open ? setOpen(false) : show()}
      onKeyDown={(event) => {
        if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
          event.preventDefault()
          if (!open) show()
          else setActive((index) => (index + (event.key === 'ArrowDown' ? 1 : -1) + groupedSchemes.length) % groupedSchemes.length)
        } else if (event.key === 'Home' || event.key === 'End') {
          event.preventDefault()
          setOpen(true)
          setActive(event.key === 'Home' ? 0 : groupedSchemes.length - 1)
        } else if (event.key === 'Enter' || event.key === ' ') {
          event.preventDefault()
          if (open) choose(groupedSchemes[active].id)
          else show()
        } else if (event.key === 'Escape') {
          event.preventDefault()
          setOpen(false)
        } else if (event.key === 'Tab') setOpen(false)
      }}
      ref={trigger}
      role="combobox"
      type="button"
    ><SchemeSwatches scheme={selected} /><span id={`${id}-value`}>{selected.name}</span><ChevronDown /></button>
    {open ? createPortal(<div aria-label="终端配色方案" className="terminal-scheme-options" id={`${id}-list`} ref={popup} role="listbox" style={position}>
      {(['dark', 'light'] as const).map((kind) => <div aria-label={kind === 'dark' ? '深色' : '浅色'} key={kind} role="group">
        <div className="scheme-group-label">{kind === 'dark' ? '深色' : '浅色'}</div>
        {groupedSchemes.filter((scheme) => scheme.kind === kind).map((scheme) => {
          const index = groupedSchemes.indexOf(scheme)
          return <button
            aria-selected={scheme.id === value}
            className={index === active ? 'active' : ''}
            data-index={index}
            id={`${id}-option-${index}`}
            key={scheme.id}
            onClick={() => choose(scheme.id)}
            onMouseDown={(event) => event.preventDefault()}
            onMouseEnter={() => setActive(index)}
            role="option"
            tabIndex={-1}
            type="button"
          ><SchemeSwatches scheme={scheme} /><span>{scheme.name}</span>{scheme.id === value ? <Check aria-hidden="true" /> : null}</button>
        })}
      </div>)}
    </div>, document.body) : null}
  </div>
}
