import { lazy, Suspense, useEffect, useRef, useState } from 'react'
import { ImageIcon } from 'lucide-react'
import type { Backend, Preferences } from '../lib/backend'
import type { TerminalBackground } from '../model/terminalBackground'
import { TerminalBackgroundAppearance, useTerminalBackground } from './TerminalBackground'

const TerminalPreview = lazy(() => import('./TerminalPreview').then(module => ({ default: module.TerminalPreview })))

export function TerminalBackgroundSettings({ backend, preferences, onChange }: { backend: Backend; preferences: Preferences; onChange: (value: TerminalBackground) => void }) {
  const [draft, setDraft] = useState(preferences.terminalBackground)
  const [choosing, setChoosing] = useState(false)
  const [dialogError, setDialogError] = useState('')
  const latest = useRef(draft)
  const saved = useRef(draft)
  const { error, loading } = useTerminalBackground()
  useEffect(() => { setDraft(preferences.terminalBackground); latest.current = saved.current = preferences.terminalBackground }, [preferences.terminalBackground])

  function commit() {
    if (latest.current === saved.current) return
    saved.current = latest.current
    onChange(latest.current)
  }
  function update(patch: Partial<TerminalBackground>, save = true) {
    latest.current = { ...latest.current, ...patch }
    setDraft(latest.current)
    if (save) commit()
  }
  async function choose() {
    setChoosing(true)
    setDialogError('')
    try {
      const path = await backend.chooseTerminalBackground(latest.current.filePath)
      if (path) update({ filePath: path, enabled: true })
    } catch (reason) { setDialogError(reason instanceof Error ? reason.message : String(reason)) }
    finally { setChoosing(false) }
  }
  function toggle(label: string, key: 'enabled' | 'tileFitLongEdge' | 'tileOnlyWholeTiles', help?: string) {
    return <div className="setting-row"><span><strong>{label}</strong>{help ? <small>{help}</small> : null}</span><button type="button" aria-label={label} role="switch" aria-checked={draft[key]} className={draft[key] ? 'switch on' : 'switch'} onClick={() => update({ [key]: !draft[key] })}><span /></button></div>
  }
  function slider(label: string, key: 'transparencyPercent' | 'positionXPercent' | 'positionYPercent', max: number) {
    const id = `terminal-background-${key}`
    return <div className="terminal-style-row"><label htmlFor={id}>{label}</label><div className="background-slider"><input id={id} type="range" min="0" max={max} step="1" value={draft[key]} onChange={event => update({ [key]: Number(event.target.value) }, false)} onPointerUp={commit} onPointerCancel={commit} onKeyUp={commit} onBlur={commit} /><output htmlFor={id}>{draft[key]}%</output></div></div>
  }
  return <section className="settings-card" id="settings-terminal-background">
    <div className="settings-card-title"><ImageIcon /><div><h2>终端背景图</h2><p>仅覆盖 SSH 终端内容及周围留白，不包含标题栏、Tab 栏和工具栏。</p></div></div>
    <TerminalBackgroundAppearance settings={draft}><Suspense fallback={<div className="terminal-preview-loading">正在加载背景图预览…</div>}><TerminalPreview preferences={preferences} label="背景图预览" /></Suspense></TerminalBackgroundAppearance>
    {toggle('启用背景图', 'enabled', '关闭后保留已选择的图片和参数。')}
    <div className="terminal-style-row"><label htmlFor="terminal-background-path">背景图文件</label><div className="background-file-control"><input id="terminal-background-path" value={draft.filePath} placeholder="未选择图片" readOnly title={draft.filePath} /><button type="button" className="button secondary small" disabled={choosing} onClick={() => void choose()}>选择图片</button><button type="button" className="button ghost small" disabled={!draft.filePath || choosing} onClick={() => { setDialogError(''); update({ filePath: '', enabled: false }) }}>清除</button></div></div>
    <p className="setting-help">支持 PNG、JPG、WEBP、GIF，最大 20 MiB、4000 万像素。图片不可用时使用终端纯色背景。</p>
    {dialogError || error ? <p className="background-error" role="alert">{dialogError || error}</p> : loading ? <p role="status" className="setting-help">正在加载背景图…</p> : null}
    <div className="terminal-style-fields">
      {slider('图片透明度', 'transparencyPercent', 95)}
      <p className="setting-help">0% 最显眼，95% 几乎不可见；默认 70%。文字透明度保持不变。</p>
      <div className="terminal-style-row"><label htmlFor="terminal-background-fit">显示模式</label><select id="terminal-background-fit" value={draft.fitMode} onChange={event => update({ fitMode: event.target.value as TerminalBackground['fitMode'] })}><option value="cover">自适应铺满</option><option value="contain">完整显示</option><option value="stretch">拉伸</option><option value="tile">平铺</option></select></div>
      {draft.fitMode === 'cover' ? <>{slider('水平位置', 'positionXPercent', 100)}{slider('垂直位置', 'positionYPercent', 100)}<p className="setting-help">50% 为居中；0% 靠左或顶部，100% 靠右或底部。</p></> : null}
      {draft.fitMode === 'tile' ? <>{toggle('长边适配终端区域', 'tileFitLongEdge', '横图按终端宽度、竖图按高度、正方形按短边缩放。')}{toggle('仅显示完整图块', 'tileOnlyWholeTiles', '完整图块整体居中；区域不足时缩小显示一张完整图片。')}</> : null}
    </div>
  </section>
}
