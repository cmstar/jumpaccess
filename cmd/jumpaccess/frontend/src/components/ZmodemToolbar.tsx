import { useState } from 'react'
import { Download, Upload, X } from 'lucide-react'
import type { ZmodemState } from '../lib/zmodemTypes'
import { downloadCommand } from '../lib/zmodem'

export function ZmodemToolbar({ state, active, onCommand }: { state?: ZmodemState; active: boolean; onCommand: (command: string) => void }) {
  const [download, setDownload] = useState(false)
  const [path, setPath] = useState('')
  const [error, setError] = useState('')
  if (!state?.upload && !state?.download) return null
  return <>
    <span aria-hidden="true" className="terminal-action-separator" />
    {state.upload ? <button aria-label="上传文件（ZMODEM）" className="icon-button" disabled={!active || state.busy} onClick={() => onCommand('rz\r')} title="上传文件（ZMODEM），保存到远程当前目录" type="button"><Upload /></button> : null}
    {state.download ? <button aria-label="下载文件（ZMODEM）" className="icon-button" disabled={!active || state.busy} onClick={() => { setError(''); setDownload(true) }} title="下载文件（ZMODEM）" type="button"><Download /></button> : null}
    {download ? <div className="modal-backdrop" onKeyDown={event => { if (event.key === 'Escape') setDownload(false) }}>
      <form aria-label="下载远程文件" aria-modal="true" className="modal" role="dialog" onSubmit={event => {
        event.preventDefault()
        try { const command = downloadCommand(path); onCommand(command); setDownload(false) }
        catch (reason) { setError(reason instanceof Error ? reason.message : String(reason)) }
      }}>
        <button aria-label="关闭" className="modal-close icon-button" onClick={() => setDownload(false)} type="button"><X /></button>
        <header><h2>下载远程文件</h2><p>请输入一个文件的完整路径，或相对于终端当前目录的路径。请先确保终端已返回命令提示符。</p></header>
        <div className="dialog-fields"><label>远程文件路径<input autoFocus aria-label="远程文件路径" onChange={event => setPath(event.target.value)} placeholder="/var/log/app.log" required value={path} /></label></div>
        {error ? <p className="dialog-error" role="alert">{error}</p> : null}
        <div className="dialog-actions"><button className="button secondary" onClick={() => setDownload(false)} type="button">取消</button><button className="button primary" disabled={!active || state.busy} type="submit"><Download />下载</button></div>
      </form>
    </div> : null}
  </>
}
