import { useEffect, useRef, useState, type CSSProperties } from 'react'
import { File, Folder, FolderPlus, Link2, Pencil, Trash2, Upload, Download, FolderUp, X, RotateCcw, Unplug, Home, ArrowUp, RefreshCcw } from 'lucide-react'
import type { Backend, SFTPDirectory, SFTPEntry, SFTPTransfer, SFTPConflictChoice } from '../lib/backend'
import type { SFTPTab } from '../model/tabs'
import './SFTPPane.css'

export interface SFTPPaneProps {
  backend: Backend
  active?: boolean
  tab: SFTPTab
  onReconnect: () => void
  onDisconnect: () => void
}

function conflictKey(task: SFTPTransfer): string { return `${task.id}\0${task.conflict?.source || task.source}\0${task.conflict?.destination || task.destination}` }
function message(reason: unknown): string { return reason instanceof Error ? reason.message : String(reason) }
function sizeLabel(size: number) { if (size < 1024) return `${size} B`; if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`; return `${(size / 1024 / 1024).toFixed(1)} MB` }

export function SFTPPane({ backend, tab, active = true, onReconnect, onDisconnect }: SFTPPaneProps) {
  const [directory, setDirectory] = useState<SFTPDirectory>({ path: '', entries: [] })
  const [transfers, setTransfers] = useState<SFTPTransfer[]>([])
  const [conflictBatch, setConflictBatch] = useState(false)
  const [resolvingConflict, setResolvingConflict] = useState(false)
  const [dismissedConflict, setDismissedConflict] = useState('')
  const [choosing, setChoosing] = useState(false)
  const [selected, setSelected] = useState<string[]>([])
  const [mutation, setMutation] = useState<{ kind: 'create' | 'rename' | 'delete'; name: string; paths: string[]; parent: string } | null>(null)
  const [mutating, setMutating] = useState(false)
  const [mutationError, setMutationError] = useState('')
  const [path, setPath] = useState('')
  const [showHidden, setShowHidden] = useState(false)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const navigation = useRef(0)
  const transferVersions = useRef(new Map<string, number>())
  const sessionHistory = useRef(new Set<string>())
  if (tab.sessionID) sessionHistory.current.add(tab.sessionID)
  const currentSession = useRef(tab.sessionID)
  currentSession.current = tab.sessionID
  const connected = tab.connectionStatus === 'active' && !!tab.sessionID
  const canUpload = tab.permissions?.upload !== false
  const canDownload = tab.permissions?.download !== false
  const canDelete = tab.permissions?.delete !== false
  const entries = directory.entries.filter((entry) => showHidden || !entry.name.startsWith('.')).sort((a, b) => (a.type === 'directory' ? 0 : 1) - (b.type === 'directory' ? 0 : 1) || a.name.localeCompare(b.name))

  async function navigate(destination: string, home = false, preserveSelection = false) {
    if (!tab.sessionID || !connected) return
    const revision = ++navigation.current
    setLoading(true)
    setError('')
    try {
      const result = home ? await backend.homeSFTPDirectory(tab.sessionID) : await backend.readSFTPDirectory(tab.sessionID, destination)
      if (revision !== navigation.current) return
      setDirectory({ ...result, entries: result.entries ?? [] })
      setPath(result.path)
      setSelected((current) => preserveSelection ? current.filter((path) => result.entries.some((entry) => entry.path === path)) : [])
    } catch (reason) {
      if (revision === navigation.current) { setError(message(reason)); setPath(directory.path) }
    } finally { if (revision === navigation.current) setLoading(false) }
  }

  const refreshDirectory = useRef(() => undefined as void)
  refreshDirectory.current = () => { if (directory.path) void navigate(directory.path, false, true) }

  useEffect(() => {
    if (connected) void navigate(tab.directory || '.')
    return () => { navigation.current++ }
  }, [backend, tab.sessionID, connected])

  function beginMutation(kind: 'create' | 'rename' | 'delete') {
    setMutationError('')
    setMutation({ kind, paths: selected, parent: directory.path, name: kind === 'rename' ? directory.entries.find((entry) => entry.path === selected[0])?.name || '' : '' })
  }

  async function submitMutation() {
    if (!mutation || !tab.sessionID || mutating || !connected || (mutation.kind === 'delete' ? !canDelete : !canUpload)) return
    setMutating(true); setMutationError('')
    try {
      if (mutation.kind === 'create') await backend.makeSFTPDirectory(tab.sessionID, `${mutation.parent.replace(/\/$/, '')}/${mutation.name.trim()}`)
      if (mutation.kind === 'rename') await backend.renameSFTPEntry(tab.sessionID, mutation.paths[0], mutation.name.trim())
      if (mutation.kind === 'delete') await backend.removeSFTPEntries(tab.sessionID, mutation.paths)
      setMutation(null)
      await navigate(directory.path)
    } catch (reason) { setMutationError(message(reason)) }
    finally { setMutating(false) }
  }

  function mergeTransfers(incoming: SFTPTransfer[], fillOnly = false) {
    setTransfers((current) => {
      const merged = new Map(current.map((item) => [item.id, item]))
      incoming.forEach((item) => { if (!fillOnly || !merged.has(item.id)) merged.set(item.id, item) })
      return [...merged.values()]
    })
  }

  useEffect(() => backend.onSFTPTransfer((event) => {
    if (!sessionHistory.current.has(event.sessionId)) return
    transferVersions.current.set(event.id, (transferVersions.current.get(event.id) || 0) + 1)
    mergeTransfers([event])
    if (event.sessionId === currentSession.current && event.status === 'completed' && event.direction === 'upload') refreshDirectory.current()
  }), [backend])

  useEffect(() => {
    const sessionId = tab.sessionID
    if (!sessionId) return
    let cancelled = false
    const versions = new Map(transferVersions.current)
    void backend.listSFTPTransfers(sessionId).then((items) => {
      if (!cancelled) mergeTransfers((items ?? []).filter((item) => (versions.get(item.id) || 0) === (transferVersions.current.get(item.id) || 0)))
    }).catch((reason) => !cancelled && setError(message(reason)))
    return () => { cancelled = true }
  }, [backend, tab.sessionID])

  useEffect(() => {
    const runtime = window.runtime
    if (!active || !connected || !canUpload || !tab.sessionID || !directory.path || !runtime?.OnFileDrop) return
    const sessionId = tab.sessionID
    const destination = directory.path
    runtime.OnFileDrop((_x, _y, paths) => {
      if (!paths.length) return
      void transferAction(async () => mergeTransfers(await backend.startSFTPTransfer({ sessionId, direction: 'upload', sources: paths, destination }), true))
    }, true)
    return () => runtime.OnFileDropOff?.()
  }, [backend, active, connected, canUpload, tab.sessionID, directory.path])

  async function transferAction(action: () => Promise<unknown>) {
    setError('')
    try { await action() } catch (reason) { setError(message(reason)) }
  }

  const conflict = transfers.find((item) => item.status === 'conflict' && conflictKey(item) !== dismissedConflict && item.sessionId === tab.sessionID)
  useEffect(() => {
    if (dismissedConflict && !transfers.some((item) => item.status === 'conflict' && conflictKey(item) === dismissedConflict)) setDismissedConflict('')
  }, [transfers, dismissedConflict])

  async function resolveConflict(choice: SFTPConflictChoice) {
    if (!conflict || resolvingConflict) return
    setResolvingConflict(true)
    try {
      await backend.resolveSFTPConflict(conflict.id, choice, conflictBatch)
      setDismissedConflict(conflictKey(conflict)); setConflictBatch(false)
    } catch (reason) { setError(message(reason)) }
    finally { setResolvingConflict(false) }
  }

  async function chooseTransfer(kind: 'upload' | 'folder' | 'download') {
    if (!connected || !tab.sessionID || choosing || (kind === 'download' ? !canDownload : !canUpload)) return
    const sessionId = tab.sessionID
    const remoteDirectory = directory.path
    const remoteSources = [...selected]
    setChoosing(true); setError('')
    try {
      let sources: string[]
      let destination: string
      if (kind === 'download') {
        destination = await backend.chooseSFTPDownloadDirectory()
        sources = remoteSources
      } else {
        const selectedDirectory = kind === 'folder' ? await backend.chooseSFTPUploadDirectory() : ''
        sources = kind === 'folder' ? (selectedDirectory ? [selectedDirectory] : []) : await backend.chooseSFTPUploadFiles()
        destination = remoteDirectory
      }
      if (!destination || !sources.length) return
      const added = await backend.startSFTPTransfer({ sessionId, direction: kind === 'download' ? 'download' : 'upload', sources, destination })
      mergeTransfers(added ?? [], true)
    } catch (reason) { setError(message(reason)) }
    finally { setChoosing(false) }
  }

  async function retryTransfer(item: SFTPTransfer) {
    const version = transferVersions.current.get(item.id) || 0
    await transferAction(async () => {
      const retried = await backend.retrySFTPTransfer(item.id)
      if ((transferVersions.current.get(item.id) || 0) === version) mergeTransfers([retried])
    })
  }

  async function clearCompleted() {
    const completed = transfers.filter((item) => ['completed', 'cancelled', 'skipped'].includes(item.status))
    const cleared = new Set(completed.map((item) => item.id))
    const sessions = [...new Set(completed.map((item) => item.sessionId))]
    await transferAction(async () => {
      await Promise.all(sessions.map((id) => backend.clearCompletedSFTPTransfers(id)))
      setTransfers((current) => current.filter((item) => !cleared.has(item.id)))
    })
  }

  const validName = !!mutation?.name.trim() && !/[\\/]/.test(mutation.name) && mutation.name !== '.' && mutation.name !== '..'
  const mutationTitle = mutation?.kind === 'create' ? '新建文件夹' : mutation?.kind === 'rename' ? '重命名' : '删除文件'

  const transferLabels = { queued: '等待中', running: '传输中', conflict: '等待处理同名文件', completed: '已完成', failed: '失败', cancelled: '已取消', skipped: '已跳过' }

  return <section className="sftp-panel" style={{ '--wails-drop-target': active && connected && canUpload ? 'drop' : 'none' } as CSSProperties}>
    <div className="terminal-toolbar"><div className="terminal-toolbar-info"><strong className="terminal-toolbar-name">{tab.descriptor.alias || tab.descriptor.assetName || tab.descriptor.target}</strong><small className="terminal-toolbar-meta" title={`Account: ${tab.descriptor.account}`}>{tab.descriptor.alias ? tab.descriptor.assetName : tab.descriptor.assetID}</small><span className={`status-dot ${connected ? '' : 'offline'}`} /><small>{connected ? '已连接' : ['connecting', 'reconnecting'].includes(tab.connectionStatus) ? '连接中…' : '未连接'}</small></div><div className="terminal-toolbar-actions">{connected ? <button aria-label="断开 SFTP 连接" className="icon-button danger" onClick={onDisconnect} title="断开连接"><Unplug /></button> : <button className="button secondary small" disabled={['connecting', 'reconnecting'].includes(tab.connectionStatus)} onClick={onReconnect}><RotateCcw />重新连接</button>}</div></div>
    {tab.error ? <div className="sftp-error" role="alert">{tab.error}</div> : null}
    <div className="sftp-actions"><button className="button primary small" disabled={!connected || !canUpload || choosing || loading} onClick={() => void chooseTransfer('upload')}><Upload />上传文件</button><button className="button secondary small" disabled={!connected || !canUpload || choosing || loading} onClick={() => void chooseTransfer('folder')}><FolderUp />上传文件夹</button><button className="button secondary small" disabled={!connected || !canDownload || choosing || selected.length === 0} onClick={() => void chooseTransfer('download')}><Download />下载</button><span className="terminal-action-separator" /><button className="button secondary small" disabled={!connected || !canUpload || loading} onClick={() => beginMutation('create')} type="button"><FolderPlus />新建文件夹</button><button className="button secondary small" disabled={!connected || !canUpload || selected.length !== 1} onClick={() => beginMutation('rename')}><Pencil />重命名</button><button className="button ghost small danger" disabled={!connected || !canDelete || selected.length === 0} onClick={() => beginMutation('delete')}><Trash2 />删除</button></div>
<div className="sftp-pathbar"><button aria-label="上级目录" title="上级目录" className="icon-button" disabled={!connected || loading || directory.path === '/'} onClick={() => void navigate(directory.path.replace(/\/?[^/]+\/?$/, '') || '/')}><ArrowUp /></button><button aria-label="Home 目录" title="Home 目录" className="icon-button" disabled={!connected || loading} onClick={() => void navigate('', true)}><Home /></button><button aria-label="刷新目录" title="刷新目录" className="icon-button" disabled={!connected || loading} onClick={() => void navigate(directory.path)}><RefreshCcw /></button>
      <form onSubmit={(event) => { event.preventDefault(); void navigate(path) }}><Folder /><input aria-label="远程路径" value={path} onChange={(event) => setPath(event.target.value)} disabled={!connected} spellCheck={false} /></form>
      <label className="sftp-hidden-toggle"><input type="checkbox" checked={showHidden} onChange={(event) => setShowHidden(event.target.checked)} />显示隐藏文件</label>
    </div>
    {error ? <div className="sftp-error" role="alert">{error}</div> : null}
    <div className="sftp-files"><table aria-label="远程文件"><thead><tr><th className="sftp-select"><input aria-label="全选文件" type="checkbox" checked={entries.length > 0 && entries.every((entry) => selected.includes(entry.path))} onChange={(event) => setSelected(event.target.checked ? entries.map((entry) => entry.path) : [])} /></th><th>名称</th><th>大小</th><th>修改时间</th><th>权限</th></tr></thead><tbody>{entries.map((entry: SFTPEntry) => <tr key={entry.path} className={selected.includes(entry.path) ? 'selected' : ''}><td className="sftp-select"><input aria-label={`选择 ${entry.name}`} type="checkbox" checked={selected.includes(entry.path)} onChange={(event) => setSelected((current) => event.target.checked ? [...current, entry.path] : current.filter((path) => path !== entry.path))} /></td><td><button aria-label={`打开 ${entry.name}`} className="sftp-entry-name" onDoubleClick={() => entry.type !== 'file' && void navigate(entry.path)} onKeyDown={(event) => { if (event.key === 'Enter' && entry.type !== 'file') void navigate(entry.path) }} type="button">{entry.type === 'directory' ? <Folder /> : entry.type === 'symlink' ? <Link2 /> : <File />}<span>{entry.name}</span></button></td><td>{entry.type === 'directory' ? '—' : sizeLabel(entry.size)}</td><td>{new Date(entry.modifiedAt).toLocaleString()}</td><td className="mono">{entry.permissions}</td></tr>)}</tbody></table>{loading ? <div className="sftp-empty">正在读取目录…</div> : entries.length === 0 ? <div className="sftp-empty">此目录为空</div> : null}</div>
    {transfers.length ? <section aria-label="传输队列" className="sftp-transfers"><header><strong>传输队列</strong><span>{transfers.length} 个任务</span><button className="button ghost small" onClick={() => void clearCompleted()}>清除已完成</button></header>{transfers.map((item) => <div className={`sftp-transfer ${item.status}`} key={item.id}>{item.direction === 'upload' ? <Upload /> : <Download />}<div><strong>{item.name}</strong><small title={item.destination}>{item.destination}</small>{item.error ? <span className="sftp-transfer-error">{item.error}</span> : null}</div><span>{transferLabels[item.status]}</span><span>{sizeLabel(item.transferred)} / {sizeLabel(item.total)}</span><progress aria-label={`${item.name} 传输进度`} max={Math.max(1, item.total)} value={item.transferred} />{['queued', 'running', 'conflict'].includes(item.status) ? <button aria-label={`取消 ${item.name}`} className="icon-button" onClick={() => void transferAction(() => backend.cancelSFTPTransfer(item.id))}><X /></button> : ['failed', 'cancelled'].includes(item.status) ? <button aria-label={`重试 ${item.name}`} title="从头重新传输" className="icon-button" disabled={!connected || item.sessionId !== tab.sessionID || (item.direction === 'upload' ? !canUpload : !canDownload)} onClick={() => void retryTransfer(item)}><RotateCcw /></button> : null}</div>)}</section> : null}
    {conflict ? <div className="modal-backdrop"><section role="dialog" aria-label="文件已存在" aria-modal="true" className="modal sftp-dialog"><h2>文件已存在</h2><p>{conflict.name}</p><small className="sftp-conflict-path">{conflict.conflict?.destination || conflict.destination}</small><label className="sftp-hidden-toggle"><input type="checkbox" checked={conflictBatch} onChange={(event) => setConflictBatch(event.target.checked)} />应用到本批次</label><div className="dialog-actions"><button className="button secondary" disabled={resolvingConflict} onClick={() => void resolveConflict('skip')}>跳过</button><button className="button secondary" disabled={resolvingConflict} onClick={() => void resolveConflict('overwrite')}>覆盖</button><button className="button primary" disabled={resolvingConflict} onClick={() => void resolveConflict('keep-both')}>保留两者</button></div></section></div> : null}
    {mutation ? <div className="modal-backdrop"><section aria-label={mutationTitle} aria-modal="true" className="modal sftp-dialog" role="dialog"><h2>{mutationTitle}</h2><form onSubmit={(event) => { event.preventDefault(); void submitMutation() }}>{mutation.kind === 'delete' ? <><p>确认删除 {mutation.paths.length} 个项目？目录中的内容也会删除，此操作无法撤销。</p><ul>{mutation.paths.map((path) => <li key={path}>{path.split('/').pop()}</li>)}</ul></> : <label>{mutation.kind === 'create' ? '文件夹名称' : '新名称'}<input autoFocus value={mutation.name} onChange={(event) => setMutation({ ...mutation, name: event.target.value })} /></label>}{mutationError ? <p role="alert">{mutationError}</p> : null}<div className="dialog-actions"><button className="button secondary" disabled={mutating} onClick={() => setMutation(null)} type="button">取消</button><button className={`button primary${mutation.kind === 'delete' ? ' danger' : ''}`} disabled={!connected || mutating || (mutation.kind === 'delete' ? !canDelete : !canUpload || !validName)} type="submit">{mutation.kind === 'create' ? '创建' : mutation.kind === 'rename' ? '保存' : '确认删除'}</button></div></form></section></div> : null}
    <div className="sftp-statusbar"><span>{entries.length} 个项目{selected.length ? ` · 已选择 ${selected.length} 项` : ''}</span><span>{directory.path}</span></div>
  </section>
}
