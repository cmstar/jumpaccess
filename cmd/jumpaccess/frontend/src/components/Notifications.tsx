import { createContext, type ReactNode, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { CircleAlert, Info, TriangleAlert, X } from 'lucide-react'
import './Notifications.css'

type Tone = 'info' | 'warning' | 'error'
interface Notice { id: number; tone: Tone; message: string }
interface Notifications {
  showInfo: (message: string) => void
  showWarning: (message: string) => void
  showError: (message: string) => void
}
const Context = createContext<Notifications | null>(null)

export function NotificationProvider({ children }: { children: ReactNode }) {
  const [notices, setNotices] = useState<Notice[]>([])
  const sequence = useRef(0)
  const dismiss = useCallback((id: number) => setNotices(current => current.filter(item => item.id !== id)), [])
  const push = useCallback((tone: Tone, message: string) => {
    if (!message.trim()) return
    const notice = { id: ++sequence.current, tone, message }
    // 相同提示只保留一条，并重新开始计时；不同错误不互相覆盖。
    setNotices(current => [...current.filter(item => item.tone !== tone || item.message !== message), notice])
  }, [])
  const value = useMemo(() => ({
    showInfo: (message: string) => push('info', message),
    showWarning: (message: string) => push('warning', message),
    showError: (message: string) => push('error', message),
  }), [push])
  return <Context.Provider value={value}>{children}{createPortal(
    <div className="notification-viewport" aria-label="应用提示">
      {notices.map(notice => <NotificationCard key={notice.id} notice={notice} dismiss={dismiss} />)}
    </div>, document.body,
  )}</Context.Provider>
}

export function useNotifications(): Notifications {
  const value = useContext(Context)
  if (!value) throw new Error('useNotifications 需要 NotificationProvider。')
  return value
}

function NotificationCard({ notice, dismiss }: { notice: Notice; dismiss: (id: number) => void }) {
  const [hovered, setHovered] = useState(false)
  const [focused, setFocused] = useState(false)
  useEffect(() => {
    if (notice.tone === 'error' || hovered || focused) return
    const timer = window.setTimeout(() => dismiss(notice.id), notice.tone === 'info' ? 3000 : 5000)
    return () => window.clearTimeout(timer)
  }, [notice.id, notice.tone, hovered, focused, dismiss])
  const Icon = notice.tone === 'info' ? Info : notice.tone === 'warning' ? TriangleAlert : CircleAlert
  return <div className={`notification notification-${notice.tone}`} role={notice.tone === 'error' ? 'alert' : 'status'} aria-atomic="true"
    onMouseEnter={() => setHovered(true)} onMouseLeave={() => setHovered(false)}
    onFocus={() => setFocused(true)} onBlur={event => { if (!event.currentTarget.contains(event.relatedTarget)) setFocused(false) }}>
    <Icon aria-hidden="true" /><span className="notification-message">{notice.message}</span>
    <button type="button" aria-label={notice.tone === 'error' ? '关闭错误提示' : '关闭提示'} title="关闭提示" onClick={() => dismiss(notice.id)}><X aria-hidden="true" /></button>
  </div>
}
