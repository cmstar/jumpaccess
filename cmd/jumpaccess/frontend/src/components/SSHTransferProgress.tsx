import { useEffect, useState } from 'react'
import { Download, Upload, X } from 'lucide-react'
import type { ZmodemState } from '../lib/zmodemTypes'

function bytes(value: number) {
  if (value < 1024) return `${value} B`
  const units = ['KB', 'MB', 'GB', 'TB']
  let amount = value / 1024
  let unit = 0
  while (amount >= 1024 && unit < units.length - 1) { amount /= 1024; unit++ }
  return `${Number(amount.toFixed(1))} ${units[unit]}`
}

export function SSHTransferProgress({ state, onCancel }: { state?: ZmodemState; onCancel: () => void }) {
  const [dismissed, setDismissed] = useState<ZmodemState>()
  const [, refresh] = useState(0)
  const autoHide = state?.phase === 'completed' || state?.phase === 'cancelled'
  const expiresAt = autoHide && state.finishedAt !== undefined ? state.finishedAt + 4000 : undefined
  useEffect(() => {
    if (expiresAt === undefined) return
    const timer = setTimeout(() => refresh(value => value + 1), Math.max(0, expiresAt - Date.now()))
    return () => clearTimeout(timer)
  }, [expiresAt])
  if (!state || (!state.busy && !state.message) || dismissed === state || (expiresAt !== undefined && Date.now() >= expiresAt)) return null

  const total = state.size ?? 0
  const transferred = state.transferred ?? 0
  const completed = state.phase === 'completed'
  const percent = completed ? 100 : total > 0 ? Math.min(99, Math.max(0, Math.floor(transferred / total * 100))) : 0
  const determinate = !!state.name && (total > 0 || completed)
  const finalizing = ['saving', 'confirming', 'finishing', 'cancelling'].includes(state.phase ?? '')
  const message = state.phase === 'saving' ? '正在保存'
    : state.phase === 'confirming' ? '等待远端确认'
      : state.phase === 'finishing' ? '正在结束传输'
        : state.message || '等待远程传输开始'
  return <section aria-label="SSH 文件传输" className={`ssh-transfer-progress${state.phase === 'failed' ? ' failed' : ''}`}>
    <span className="ssh-transfer-direction">{state.direction === 'download' ? <Download /> : <Upload />}{state.direction === 'download' ? '下载' : state.direction === 'upload' ? '上传' : '传输'}</span>
    <div className="ssh-transfer-details"><span role="status" title={message}>{message}</span>{state.name ? <span className="ssh-transfer-name" title={state.name}>{state.name}</span> : null}</div>
    {state.busy || state.name ? <div className="ssh-transfer-meter"><progress aria-label="当前文件传输进度" max={100} value={determinate ? percent : undefined} />{state.name ? <span>{determinate ? `${percent}% · ` : ''}{bytes(transferred)} / {bytes(total)}</span> : null}</div> : null}
    {state.busy ? <button className="button ghost small" disabled={finalizing} title={state.phase === 'cancelling' ? '正在取消传输并清理在途数据' : finalizing ? '正在确认或保存文件，暂不可取消' : '取消当前传输'} onClick={onCancel} type="button">取消传输</button> : <button aria-label="关闭传输提示" className="icon-button" onClick={() => setDismissed(state)} type="button"><X /></button>}
  </section>
}
