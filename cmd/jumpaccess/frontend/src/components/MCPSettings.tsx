import { useEffect, useRef, useState } from 'react'
import { Copy, FolderOpen, Info, Play, RefreshCw, RotateCw, Server, Square } from 'lucide-react'
import type { MCPBackend, MCPConfig, MCPStatus } from '../lib/mcpTypes'
import { useCopyText } from './useCopyText'
import './MCPSettings.css'

type Draft = Omit<MCPConfig, 'port'> & { port: string }
function toDraft(value: MCPConfig): Draft { return { ...value, port: String(value.port), tlsCert: value.tlsCert || '', tlsKey: value.tlsKey || '', token: value.token || '' } }
function equal(a: Draft, b: Draft) { return a.listenHost === b.listenHost && a.port === b.port && a.allowLAN === b.allowLAN && a.tlsCert === b.tlsCert && a.tlsKey === b.tlsKey && a.token === b.token }
function errorMessage(value: unknown) { return value instanceof Error ? value.message : String(value) }

function validate(draft: Draft): MCPConfig {
  if (!/^[0-9]+$/.test(draft.port) || Number(draft.port) < 1 || Number(draft.port) > 65535) throw new Error('端口必须为 1–65535 的整数')
  const host = draft.listenHost.trim()
  const ipv4 = /^(0|[1-9][0-9]{0,2})(\.(0|[1-9][0-9]{0,2})){3}$/.test(host) && host.split('.').every(part => Number(part) <= 255)
  let ipv6 = ''
  if (host.includes(':')) { try { ipv6 = new URL(`http://[${host}]/`).hostname } catch { /* 由下面的统一地址校验返回错误。 */ } }
  if (host !== 'localhost' && !ipv4 && !ipv6) throw new Error('监听地址必须为 IP 或 localhost')
  const loopback = host === 'localhost' || (ipv4 && host.startsWith('127.')) || ipv6 === '[::1]' || /^\[::ffff:7f[0-9a-f]{2}:[0-9a-f]{1,4}\]$/.test(ipv6)
  if (!loopback && !draft.allowLAN) throw new Error('非回环监听需要开启允许局域网访问')
  const tlsCert = draft.tlsCert!.trim(), tlsKey = draft.tlsKey!.trim()
  if (Boolean(tlsCert) !== Boolean(tlsKey)) throw new Error('TLS 证书和私钥必须同时配置，或同时留空')
  if (!draft.token) throw new Error('Bearer Token 不能为空')
  if (!/^[A-Za-z0-9_-]{42}[AEIMQUYcgkosw048]$/.test(draft.token)) throw new Error('Token 必须为 32 字节随机值的无填充 Base64url 编码')
  return { ...draft, listenHost: host, port: Number(draft.port), tlsCert, tlsKey }
}

export function MCPSettings({ backend, active }: { backend?: MCPBackend; active: boolean }) {
  const [status, setStatus] = useState<MCPStatus | null>(null)
  const [draft, setDraft] = useState<Draft | null>(null)
  const [busy, setBusy] = useState(false)
  const [serviceAction, setServiceAction] = useState<'stop' | 'restart' | null>(null)
  const [error, setError] = useState('')
  const [configError, setConfigError] = useState('')
  const [statusError, setStatusError] = useState('')
  const statusRef = useRef<MCPStatus | null>(null)
  const draftRef = useRef<Draft | null>(null)
  const dirtyRef = useRef(false), busyRef = useRef(false), epoch = useRef(0)
  const copyText = useCopyText()
  const dirty = Boolean(status && draft && !equal(draft, toDraft(status.config)))

  function updateDraft(value: Draft) {
    draftRef.current = value
    dirtyRef.current = Boolean(statusRef.current && !equal(value, toDraft(statusRef.current.config)))
    setDraft(value)
  }
  function acceptStatus(value: MCPStatus, saved = false) {
    const keepDraft = dirtyRef.current && !saved
    statusRef.current = value; setStatus(value); setStatusError('')
    updateDraft(keepDraft && draftRef.current ? draftRef.current : toDraft(value.config))
  }

  useEffect(() => () => { epoch.current++ }, [])
  useEffect(() => {
    if (!backend || !active) return
    let disposed = false, pending = false
    async function refresh() {
      if (pending || busyRef.current) return
      pending = true
      const version = epoch.current
      try {
        const value = await backend!.status()
        if (!disposed && epoch.current === version) acceptStatus(value)
      } catch (cause) {
        if (!disposed && epoch.current === version) setStatusError(errorMessage(cause))
      } finally { pending = false }
    }
    void refresh()
    const timer = window.setInterval(() => void refresh(), 3000)
    return () => { disposed = true; window.clearInterval(timer) }
  }, [backend, active])

  async function run<T>(action: () => Promise<T>, success: (value: T) => void, target: 'service' | 'config' = 'service', refreshOnFailure = false) {
    if (busyRef.current) return
    busyRef.current = true
    const version = ++epoch.current
    const reportError = target === 'config' ? setConfigError : setError
    setBusy(true); reportError('')
    try {
      const value = await action()
      if (epoch.current === version) success(value)
    } catch (cause) {
      if (epoch.current === version) reportError(errorMessage(cause))
      if (refreshOnFailure && backend) {
        try {
          const value = await backend.status()
          if (epoch.current === version) acceptStatus(value)
        } catch (refreshError) { if (epoch.current === version) setStatusError(errorMessage(refreshError)) }
      }
    } finally { if (epoch.current === version) { busyRef.current = false; setBusy(false) } }
  }
  function edit(patch: Partial<Draft>) { if (draftRef.current) { updateDraft({ ...draftRef.current, ...patch }); setConfigError('') } }
  function reset() { if (statusRef.current) { updateDraft(toDraft(statusRef.current.config)); setConfigError('') } }
  function save() {
    if (!backend || !draft || !dirtyRef.current || busyRef.current) return
    try { const value = validate(draft); void run(() => backend.saveConfig(value), result => acceptStatus(result, true), 'config') }
    catch (cause) { setConfigError(errorMessage(cause)) }
  }
  function choose(kind: 'certificate' | 'key') {
    const field = kind === 'certificate' ? 'tlsCert' : 'tlsKey'
    void run(() => backend!.chooseTLSFile(kind, draftRef.current?.[field] || ''), path => { if (path) edit({ [field]: path }) }, 'config')
  }
  function stopOrRestart(action: 'stop' | 'restart') {
    if (!backend || busyRef.current) return
    setServiceAction(action)
    void run(() => backend[action](), acceptStatus, 'service', true).finally(() => setServiceAction(null))
  }

  const running = status?.state === 'running', external = status?.state === 'external'
  const stateLabel = statusError ? '状态获取失败' : !status ? '读取状态中' : ({ running: '运行中', stopped: '已停止', external: '其他进程占用', failed: '服务异常' }[status.state])
  const actualURL = status?.instance?.url
  const needsRestart = (running || external) && (dirty || status?.restartRequired)
  const controlsLocked = busy || !status || external

  return <section className="settings-card mcp-settings" id="settings-mcp">
    <div className="settings-card-title"><Server /><div><h2>Agent MCP</h2><p>传输协议：Streamable HTTP</p></div></div>
    {!backend ? <p className="setting-help">当前预览不提供 MCP 服务。</p> : <>
      <div className="mcp-service-controls">
        <strong className={`mcp-state ${running || external ? 'running' : ''}`}><span />{stateLabel}</strong>
        <div className="mcp-actions">
          <button type="button" className={`button ${!running && !external ? 'primary' : 'secondary'} small`} disabled={controlsLocked || running || dirty || !status?.tokenConfigured || Boolean(statusError)} onClick={() => void run(() => backend.start(), acceptStatus, 'service', true)}><Play />启动</button>
          <button type="button" className="button secondary small" disabled={controlsLocked || !running} onClick={() => stopOrRestart('stop')}><Square />{serviceAction === 'stop' ? '停止中…' : '停止'}</button>
          <button type="button" className={`button ${status?.restartRequired && !dirty ? 'primary' : 'secondary'} small`} disabled={controlsLocked || !running || dirty || Boolean(statusError)} onClick={() => stopOrRestart('restart')}><RotateCw />{serviceAction === 'restart' ? '重启中…' : '重启'}</button>
        </div>
      </div>
      {running && status?.counts && <div className="mcp-counts" aria-label="MCP 共享状态"><span>逻辑连接 {status.counts.connections}</span><span>SSH 连接 {status.counts.ssh_connections}</span><span>连接中 {status.counts.connecting}</span><span>执行中 {status.counts.executions}</span>{status.counts.unknown_executions > 0 && <span>结果未知 {status.counts.unknown_executions}</span>}</div>}
      {serviceAction ? <p className="setting-help" role="status">正在请求停止并清理任务…</p> : running && (status?.counts?.executions || 0) > 0 && <p className="setting-help">停止或重启会请求停止当前共享任务；无法确认结果时，远端命令可能继续运行。</p>}
      {status?.stopWarning && <p className="mcp-error" role="alert">{status.stopWarning}</p>}
      {external && <p className="setting-help">宿主：{status?.instance?.host || '未知'}。请到启动服务的进程中停止或重启服务。</p>}
      {actualURL && <div className="mcp-address"><span>当前服务地址</span><code aria-label="MCP 实际地址">{actualURL}</code><button type="button" className="icon-button" aria-label="复制当前服务地址" title="复制当前服务地址" onClick={() => void copyText(actualURL)}><Copy /></button></div>}
      {status?.instance && ['0.0.0.0', '::'].includes(status.instance.config.listenHost) && <p className="setting-help">Agent 接入时请将通配监听地址替换为本机实际 IP。</p>}
      {needsRestart && <p className="mcp-notice" role="status"><Info />{dirty ? '配置有未保存的修改。保存并重启后生效。' : '配置已保存，重启服务后生效。'}</p>}
      {(statusError || error || status?.error) && <p role="alert" className="mcp-error">{error || statusError || status?.error}</p>}
      {statusError && <button type="button" className="button secondary small" disabled={busy} onClick={() => void run(() => backend.status(), acceptStatus)}>刷新状态</button>}
      {draft && <form className="mcp-config-box" aria-label="MCP 配置" noValidate onSubmit={event => { event.preventDefault(); save() }}>
        <div className="mcp-config-heading"><h3>连接与访问配置</h3>{configError && <p role="alert" className="mcp-error">{configError}</p>}<span className={`mcp-save-badge ${dirty ? 'dirty' : ''}`}>{dirty ? '未保存' : '已保存'}</span></div>
        <fieldset disabled={busy} className="mcp-fields">
          <div className="mcp-field"><label htmlFor="mcp-host">监听地址</label><input id="mcp-host" aria-label="MCP 监听地址" autoComplete="off" spellCheck={false} value={draft.listenHost} onChange={event => edit({ listenHost: event.target.value })} /></div>
          <div className="mcp-field"><label htmlFor="mcp-port">端口</label><input id="mcp-port" aria-label="MCP 端口" className="mcp-port" type="text" inputMode="numeric" value={draft.port} onChange={event => edit({ port: event.target.value })} /></div>
          <div className="mcp-field"><label htmlFor="mcp-lan">允许局域网访问</label><div className="mcp-inline"><button id="mcp-lan" type="button" role="switch" aria-label="允许 MCP 局域网访问" aria-checked={draft.allowLAN} className={draft.allowLAN ? 'switch on' : 'switch'} onClick={() => edit({ allowLAN: !draft.allowLAN })}><span /></button><small className="setting-help">{draft.allowLAN ? '需监听局域网地址或 0.0.0.0' : '关闭时仅允许本机访问'}</small></div></div>
          <div className="mcp-divider" />
          <div className="mcp-field"><label htmlFor="mcp-cert">TLS 证书 <small>可选</small></label><div className="settings-input-action"><input id="mcp-cert" aria-label="MCP TLS 证书路径" placeholder="留空使用 HTTP" value={draft.tlsCert} onChange={event => edit({ tlsCert: event.target.value })} /><button type="button" className="icon-button" aria-label="选择 TLS 证书文件" title="选择 TLS 证书文件" onClick={() => choose('certificate')}><FolderOpen /></button></div></div>
          <div className="mcp-field"><label htmlFor="mcp-key">TLS 私钥 <small>可选</small></label><div className="settings-input-action"><input id="mcp-key" aria-label="MCP TLS 私钥路径" placeholder="启用 TLS 时与证书一同配置" value={draft.tlsKey} onChange={event => edit({ tlsKey: event.target.value })} /><button type="button" className="icon-button" aria-label="选择 TLS 私钥文件" title="选择 TLS 私钥文件" onClick={() => choose('key')}><FolderOpen /></button></div></div>
          <p className="setting-help mcp-tls-help">证书与私钥同时留空，使用 HTTP + Token。</p>
          <div className="mcp-divider" />
          <div className="mcp-field"><label htmlFor="mcp-token">Bearer Token</label><div className="mcp-token-control"><div className="settings-input-action"><input id="mcp-token" aria-label="MCP Bearer Token" type="text" autoComplete="off" spellCheck={false} value={draft.token} onChange={event => edit({ token: event.target.value })} /><button type="button" className="icon-button" disabled={!draft.token} aria-label="复制输入框中的 Token" title="复制输入框中的 Token" onClick={() => void copyText(draft.token || '')}><Copy /></button></div><button type="button" className="button secondary small" onClick={() => void run(() => backend.generateToken(), value => edit({ token: value }), 'config')}><RefreshCw />生成</button></div></div>
        </fieldset>
        <div className="mcp-config-footer"><button type="button" className="button secondary small" disabled={busy || !dirty} onClick={reset}>重置</button><button type="submit" className="button primary small" disabled={busy || !dirty}>保存</button></div>
      </form>}
    </>}
  </section>
}
