import { useEffect, useRef } from 'react'

export function MCPConfirm({ title, description, action, onConfirm, onCancel }: { title: string; description: string; action: string; onConfirm: () => void; onCancel: () => void }) {
  const dialog = useRef<HTMLElement>(null)
  useEffect(() => {
    const previous = document.activeElement as HTMLElement | null
    dialog.current?.querySelector<HTMLButtonElement>('button')?.focus()
    return () => previous?.focus()
  }, [])
  return <div className="modal-backdrop"><section ref={dialog} className="modal" role="dialog" aria-modal="true" aria-label={title} onKeyDown={event => {
    if (event.key === 'Escape') onCancel()
    if (event.key !== 'Tab') return
    const buttons = dialog.current?.querySelectorAll<HTMLButtonElement>('button')
    if (!buttons?.length) return
    if (event.shiftKey && document.activeElement === buttons[0]) { event.preventDefault(); buttons[buttons.length - 1].focus() }
    if (!event.shiftKey && document.activeElement === buttons[buttons.length - 1]) { event.preventDefault(); buttons[0].focus() }
  }}><h2>{title}</h2><p>{description}</p><div className="dialog-actions"><button type="button" className="button secondary" onClick={onCancel}>取消</button><button type="button" className="button primary" onClick={onConfirm}>{action}</button></div></section></div>
}
