import { type CSSProperties, useEffect, useLayoutEffect, useRef, useState } from 'react'
import { ArrowDownToLine, ExternalLink, Link, Pause, Play, ScrollText, SlidersHorizontal, Trash2, Unplug, X } from 'lucide-react'
import type { Preferences } from '../lib/backend'
import type { MCPBackend, MCPConnection, MCPLogLine, MCPStatus } from '../lib/mcpTypes'
import { terminalDisplayOptions, terminalScheme } from '../model/terminalTheme'
import { MCPSettings } from './MCPSettings'
import { MCPConfirm } from './MCPConfirm'
import { useNotifications } from './Notifications'
import './MCPWorkspace.css'

const message = (error: unknown) => error instanceof Error ? error.message : String(error)
const time = (value: string) => new Date(value).toLocaleTimeString([], { hour12: false })
const logTime = (value: string) => {
  const date = new Date(value)
  const pad = (part: number) => String(part).padStart(2, '0')
  return `${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`
}
// 展示缓存不拥有业务结果。按行数与字符数双限额，暂停缓存也遵守相同上限。
function bounded(lines: MCPLogLine[]) {
  let length = 0, start = lines.length
  while (start > 0 && lines.length - start < 3000 && length + lines[start - 1].text.length <= 2 * 1024 * 1024) { start--; length += lines[start].text.length }
  return lines.slice(start)
}

export function MCPWorkspace({ backend, hidden, preferences, onSavePreferences, onTerminalStyle, onStatusChange }: { backend?: MCPBackend; hidden: boolean; preferences: Preferences; onSavePreferences?: (preferences: Preferences) => void; onTerminalStyle: () => void; onStatusChange?: (status: MCPStatus) => void }) {
  const { showWarning } = useNotifications()
  const autoStartNotified = useRef(false)
  const [view, setView] = useState<'connections' | 'logs' | 'config'>('logs')
  const initializedBackend = useRef<MCPBackend | null>(null)
  const [status, setStatus] = useState<MCPStatus | null>(null)
  const [connections, setConnections] = useState<MCPConnection[]>([])
  const [selected, setSelected] = useState('')
  const [filter, setFilter] = useState('')
  const [connectionError, setConnectionError] = useState('')
  const [connectionReadError, setConnectionReadError] = useState('')
  const [closing, setClosing] = useState(false)
  const [confirmation, setConfirmation] = useState<MCPConnection | null>(null)
  const [level, setLevel] = useState(2)
  const levelRef = useRef(2), pausedRef = useRef(false), cursor = useRef(0), generation = useRef(0), clearGeneration = useRef(0)
  const [paused, setPaused] = useState(false), [pendingCount, setPendingCount] = useState(0)
  const pending = useRef<MCPLogLine[]>([])
  const [lines, setLines] = useState<MCPLogLine[]>([])
  const [logError, setLogError] = useState(''), [gap, setGap] = useState(false)
  const [following, setFollowing] = useState(true)
  const followingRef = useRef(true), viewport = useRef<HTMLDivElement>(null)
  const connectionVersion = useRef(0)
  const [connectionRefresh, setConnectionRefresh] = useState(0)
  useEffect(() => {
    if (!backend) return
    let disposed = false, timer: number | undefined
    const version = ++generation.current
    cursor.current = 0; pending.current = []; setLines([]); setPendingCount(0)
    async function poll() {
      const cleared = clearGeneration.current
      let more = false
      try {
        const page = await backend!.logs(cursor.current, 200)
        if (disposed || generation.current !== version) return
        cursor.current = page.nextCursor; more = page.hasMore; setLogError('')
        const autoStartFailure = page.lines.find(line => line.event === 'auto_start_failed')
        if (autoStartFailure && !autoStartNotified.current) {
          autoStartNotified.current = true
          showWarning(autoStartFailure.text)
        }
        if (cleared === clearGeneration.current) {
          if (page.gap) setGap(true)
          const added = page.lines.filter(line => line.detail <= levelRef.current)
          if (pausedRef.current) { pending.current = bounded([...pending.current, ...added]); setPendingCount(pending.current.length) }
          else if (added.length) setLines(old => bounded([...old, ...added]))
        }
      } catch (error) { if (!disposed) setLogError(message(error)) }
      finally { if (!disposed) timer = window.setTimeout(() => void poll(), more ? 0 : 1000) }
    }
    void poll()
    return () => { disposed = true; generation.current++; window.clearTimeout(timer) }
  }, [backend, showWarning])

  useEffect(() => {
    if (!backend || hidden) return
    let disposed = false, pendingRequest = false
    const version = ++connectionVersion.current
    async function refresh() {
      if (pendingRequest) return
      pendingRequest = true
      try {
        const all = await backend!.connections()
        if (!disposed && connectionVersion.current === version) { setConnections(all.filter(c => c.status !== 'closed')); setConnectionReadError('') }
      } catch (error) { if (!disposed && connectionVersion.current === version) setConnectionReadError(message(error)) }
      finally { pendingRequest = false }
    }
    void refresh()
    const timer = window.setInterval(() => void refresh(), 2000)
    return () => { disposed = true; connectionVersion.current++; window.clearInterval(timer) }
  }, [backend, hidden, status?.instance?.id, status?.state, connectionRefresh])

  const current = connections.find(c => c.connection_id === selected) || connections[0]
  const filtered = lines.filter(line => !filter || line.connectionID === filter)
  useLayoutEffect(() => {
    if (!hidden && view === 'logs' && followingRef.current && !pausedRef.current && viewport.current) viewport.current.scrollTop = viewport.current.scrollHeight
  }, [lines, hidden, view, paused, filter, preferences.terminalFontSize, preferences.terminalLineHeight])
  function acceptStatus(value: MCPStatus) {
    // 只根据首次读到的状态选择入口，后续轮询不打断用户浏览或编辑。
    if (initializedBackend.current !== backend) {
      initializedBackend.current = backend || null
      if (!value.tokenConfigured && value.state !== 'running' && value.state !== 'external') setView('config')
    }
    setStatus(value); onStatusChange?.(value)
  }
  async function closeConnection(connection: MCPConnection, confirmed = false) {
    if (!backend || closing) return
    if (connection.active_execution_id && !confirmed) { setConfirmation(connection); return }
    setClosing(true); setConnectionError(''); const version = ++connectionVersion.current
    try {
      const result = await backend.closeConnection(connection.connection_id, connection.active_execution_id ? 'drain' : 'if_idle')
      if (version === connectionVersion.current) setConnections(old => result.status === 'closed' ? old.filter(c => c.connection_id !== result.connection_id) : old.map(c => c.connection_id === result.connection_id ? result : c))
    } catch (error) { if (version === connectionVersion.current) setConnectionError(message(error)) }
    finally { setClosing(false); setConnectionRefresh(n => n + 1) }
  }
  function pause() {
    const next = !pausedRef.current; pausedRef.current = next; setPaused(next)
    if (!next) { const batch = pending.current; pending.current = []; setPendingCount(0); setLines(old => bounded([...old, ...batch])) }
  }
  function clear() { clearGeneration.current++; setLines([]); pending.current = []; setPendingCount(0); setGap(false); followingRef.current = true; setFollowing(true) }
  function follow() { followingRef.current = true; setFollowing(true); if (viewport.current) viewport.current.scrollTop = viewport.current.scrollHeight }
  const display = terminalDisplayOptions(preferences), palette = terminalScheme(preferences.terminalColorScheme)
  const terminalStyle = { '--mcp-terminal-bg': display.theme.background, '--mcp-terminal-fg': display.theme.foreground, '--mcp-terminal-red': display.theme.red, '--mcp-terminal-green': display.theme.green, '--mcp-terminal-yellow': display.theme.yellow, '--mcp-terminal-cyan': display.theme.cyan, '--mcp-terminal-blue': display.theme.blue, '--mcp-terminal-selection': display.theme.selectionBackground, fontFamily: display.fontFamily, fontSize: display.fontSize, lineHeight: display.lineHeight } as CSSProperties
  return <section className="full-pane mcp-workspace" hidden={hidden} aria-label="Agent MCP 工作区">
    <MCPSettings backend={backend} active={true} configurationVisible={view === 'config'} onStatusChange={acceptStatus} confirmServiceActions autoStart={preferences.mcpAutoStart} onAutoStartChange={value => onSavePreferences?.({ ...preferences, mcpAutoStart: value })}>
      <div className="mcp-tabs" role="tablist" aria-label="MCP 模块">
        {([['logs', '日志', ScrollText], ['connections', '连接', Link], ['config', '配置', SlidersHorizontal]] as const).map(([id, label, Icon]) => <button key={id} type="button" role="tab" id={`mcp-tab-${id}`} aria-selected={view === id} aria-controls={`mcp-panel-${id}`} onClick={() => setView(id)}><Icon />{label}{id === 'connections' && <small>{connections.length}</small>}</button>)}
      </div>
      <section id="mcp-panel-connections" role="tabpanel" aria-labelledby="mcp-tab-connections" hidden={view !== 'connections'} className="mcp-connections">
        {(connectionError || connectionReadError) && <p role="alert" className="mcp-error">{connectionError || connectionReadError}</p>}
        {status?.state === 'external' ? <p className="setting-help">连接与日志由其他宿主持有，请在启动服务的进程查看。</p> : !connections.length ? <div className="mcp-empty"><Unplug /><p>{status?.state === 'running' ? '暂无 Agent 连接' : '启动 MCP 服务后，Agent 连接会显示在这里'}</p></div> : <>
          <div className="mcp-connection-table"><table><thead><tr><th>目标 / 连接 ID</th><th>Profile / 账号</th><th>SSH 状态</th><th>当前任务</th><th>最近活动</th></tr></thead><tbody>{connections.map(c => <tr key={c.connection_id} className={current?.connection_id === c.connection_id ? 'selected' : ''}><td><button type="button" className="mcp-text-link" onClick={() => setSelected(c.connection_id)}>{c.target.alias || c.target.asset_id}</button><code title={c.connection_id}>{c.connection_id}</code></td><td>{c.target.profile}<small>{c.target.account_id}</small></td><td>{c.status === 'draining' ? '等待关闭' : c.status === 'connecting' ? '连接中' : c.transport_connected ? 'SSH 已连接' : 'SSH 已释放'}</td><td>{c.active_execution_id ? '执行中' : '空闲'}</td><td>{time(c.last_used_at)}</td></tr>)}</tbody></table></div>
          {current && <div className="mcp-connection-detail"><div className="mcp-detail-heading"><strong>{current.target.alias || current.target.asset_id}</strong><span /><button type="button" className="mcp-text-link" onClick={() => { setFilter(current.connection_id); setView('logs'); follow() }}><ScrollText />查看日志</button><button type="button" className="button secondary small" disabled={closing || current.status === 'draining'} onClick={() => void closeConnection(current)}>{closing ? '关闭中…' : current.status === 'draining' ? '等待任务结束…' : '关闭连接'}</button></div><code>{current.connection_id}</code><dl><div><dt>连接上下文</dt><dd>{current.target.profile} / {current.target.organization || '—'}</dd></div><div><dt>账号</dt><dd>{current.target.account_id}</dd></div><div><dt>建立时间</dt><dd>{new Date(current.created_at).toLocaleString()}</dd></div></dl>{current.active_execution_id ? <p>当前执行 <code>{current.active_execution_id}</code></p> : !current.transport_connected ? <p className="setting-help">SSH 已闲置释放，下次执行时自动重连；逻辑连接 ID 仍然有效。</p> : <p className="setting-help">当前没有执行中的任务</p>}{current.last_error && <p className="mcp-error">{current.last_error.code}：{current.last_error.message}</p>}{Boolean(current.recent_unknown_execution_ids?.length) && <p className="mcp-error">此连接存在结果未知的任务，请核对远端状态。</p>}</div>}
        </>}
      </section>
      <section id="mcp-panel-logs" role="tabpanel" aria-labelledby="mcp-tab-logs" hidden={view !== 'logs'} className="mcp-logs">
        <div className="mcp-log-controls"><span>输出级别</span><div className="mcp-levels">{['精简', '标准', '详细'].map((label, i) => <button type="button" key={label} aria-pressed={level === i + 1} onClick={() => { levelRef.current = i + 1; setLevel(i + 1) }}>{label}</button>)}</div><span className="setting-help">仅影响后续日志，已显示的内容保持不变</span><span className="mcp-spacer" /><button type="button" className="button secondary small" onClick={pause}>{paused ? <Play /> : <Pause />}{paused ? '恢复显示' : '暂停显示'}</button><button type="button" className="button secondary small" onClick={clear}><Trash2 />清空显示</button></div>
        {filter && <div className="mcp-log-filter">连接：<code>{filter}</code><button type="button" className="icon-button" aria-label="取消连接筛选" onClick={() => setFilter('')}><X /></button></div>}
        {logError && <p role="alert" className="mcp-error">日志读取失败：{logError}</p>}
        {gap && <p role="status" className="setting-help">部分历史日志已回收，当前显示仍保留的记录。</p>}
        <div className="mcp-log-terminal" aria-label="MCP 日志终端" style={terminalStyle}>
          <div className="mcp-console-head"><span className={`mcp-collect-dot ${status?.state === 'running' ? 'running' : ''}`} /><span>{status?.state === 'running' ? '正在采集' : status?.state === 'external' ? '由其他宿主持有' : '采集已停止'}</span>{paused && <span role="status">显示已暂停{pendingCount > 0 ? ` · ${pendingCount} 条待显示` : ''}</span>}<span className="mcp-spacer" /><button type="button" className="mcp-palette-link" onClick={onTerminalStyle}>{palette.name} · 终端样式<ExternalLink /></button></div>
          <div className="mcp-log-viewport" ref={viewport} onScroll={event => { const el = event.currentTarget; followingRef.current = el.scrollHeight - el.clientHeight - el.scrollTop < 12; setFollowing(followingRef.current) }}>
            <div role="log" aria-live="off" aria-label="MCP 操作日志">{filtered.map(line => <div key={line.sequence} className="mcp-log-line"><span className="mcp-log-time">{logTime(line.time)}</span>{'  '}<span className={`mcp-severity-${line.severity.toLowerCase()}`}>{line.severity.padEnd(5)}</span>{' '}{line.connectionID && <><span className="mcp-log-connection">[{line.connectionID}]</span>{' '}</>}<span className={line.event === 'command_input' ? 'mcp-log-command' : ''}>{line.text}</span></div>)}{!filtered.length && <div className="mcp-log-empty">{filter ? '该连接暂无已显示的日志' : '等待新日志…'}</div>}</div>
          </div>
          <div className="mcp-console-footer"><span>只读 · {filtered.length} 行</span><span className="mcp-spacer" /><button type="button" aria-pressed={following} onClick={follow}><ArrowDownToLine />{following ? '跟随最新' : '回到最新'}</button></div>
        </div>
      </section>
    </MCPSettings>
    {confirmation && <MCPConfirm title="等待任务结束后关闭连接？" description="关闭等待期间不再接受新任务。当前执行结束后释放 SSH，并使逻辑连接 ID 失效。" action="等待并关闭" onCancel={() => setConfirmation(null)} onConfirm={() => { const connection = confirmation; setConfirmation(null); void closeConnection(connection, true) }} />}
  </section>
}
