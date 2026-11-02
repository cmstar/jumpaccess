import type { MCPBackend, MCPConfig, MCPConnection, MCPLogLine, MCPStatus } from './mcpTypes'

// 独立浏览器预览仅操作模拟内存，不监听端口或执行命令。
export function createMCPPreviewBackend({ configured = true }: { configured?: boolean } = {}): MCPBackend {
  // 满足界面格式校验，但显式标为演示数据，不生成或展示真实凭据。
  const sampleToken = (version: number) => `DEMO_ONLY_NOT_A_REAL_TOKEN_${version}`.padEnd(42, '_') + 'A'
  let config: MCPConfig = { listenHost: '127.0.0.1', port: 39287, allowLAN: false, token: configured ? sampleToken(0) : '', tlsCert: '', tlsKey: '' }
  let instance: MCPStatus['instance'] = null, runtimeToken = '', sequence = 0, instanceSequence = 0, tokenSequence = 0
  let connections: MCPConnection[] = []
  let lines: MCPLogLine[] = []
  const drainTimers = new Map<string, ReturnType<typeof setTimeout>>()
  const clone = <T,>(value: T): T => JSON.parse(JSON.stringify(value))
  const record = (text: string, detail = 1, connectionID = '', event = 'service', executionID = '') => {
    lines.push({ sequence: ++sequence, time: new Date().toISOString(), instance: instance?.id || '', severity: detail === 3 ? 'DEBUG' : 'INFO', detail, connectionID, executionID, event, text })
    lines = lines.slice(-3000)
  }
  const status = (): MCPStatus => clone({ state: instance ? 'running' : 'stopped', instance, config, tokenConfigured: Boolean(config.token), restartRequired: Boolean(instance && (runtimeToken !== config.token || Object.keys(instance.config).some(key => instance!.config[key as keyof MCPConfig] !== config[key as keyof MCPConfig]))), error: '', counts: instance ? { connections: connections.length, ssh_connections: connections.filter(c => c.transport_connected).length, executions: connections.filter(c => c.active_execution_id).length, connecting: 0, unknown_executions: 0, draining: false } : undefined })
  const finish = (connection: MCPConnection, cancelled = false) => {
    if (!connection.active_execution_id) return
    const id = connection.active_execution_id
    record(`命令结束  ${id}  ${cancelled ? 'status=cancelled' : 'status=completed  exit=0'}  duration=1280ms`, 2, connection.connection_id, 'execution_finished', id)
    delete connection.active_execution_id
    connection.last_used_at = new Date().toISOString()
  }
  const close = (connection: MCPConnection) => {
    finish(connection)
    connection.status = 'closed'
    connection.transport_connected = false
    connections = connections.filter(item => item !== connection)
    record('逻辑连接已关闭  reason=requested', 1, connection.connection_id, 'connection_closed')
  }
  const start = async () => {
    if (!instance) {
      if (!config.token) throw new Error('请先保存 Bearer Token')
      const { token, ...publicConfig } = config
      runtimeToken = token
      const host = config.listenHost.includes(':') ? `[${config.listenHost}]` : config.listenHost
      instance = { id: `demo-mcp-${++instanceSequence}`, host: 'gui', url: `${config.tlsCert ? 'https' : 'http'}://${host}:${config.port}/mcp`, startedAt: new Date().toISOString(), config: publicConfig }
      record(`MCP 服务已启动  ${instance.url}  host=gui`, 1, '', 'service_started')
      record('list_assets  调用完成  count=3  total=5  has_more=true  duration=28ms', 2, '', 'tool_call')
      record('list_assets  查询条件  profile="office"  organization="org-dev"  search=""  limit=3  has_cursor=false', 3, '', 'tool_conditions')
      const targets = [
        { alias: 'web', asset_id: '7f3c91bd', account_id: 'account-deploy' },
        { alias: 'builder', asset_id: '5e1a08c7', account_id: 'account-builder' },
        { alias: 'prod-db', asset_id: 'd92a2c14', account_id: 'account-dba' },
      ]
      connections = targets.map((target, i) => ({ connection_id: `${instance!.id}:c:${i + 1}`, target: { profile: 'office', organization: 'org-dev', ...target }, status: i === 2 ? 'disconnected' : 'ready', transport_connected: i !== 2, created_at: new Date().toISOString(), last_used_at: new Date().toISOString() }))
      for (const c of connections) record('连接已建立', 1, c.connection_id, 'connection_open')
      const completedID = `${instance.id}:x:1`, activeID = `${instance.id}:x:2`
      record(`命令已受理  ${completedID}`, 2, connections[0].connection_id, 'execution_accepted', completedID)
      record('command="test -f /srv/webapp/maintenance.flag"  cwd="/"', 3, connections[0].connection_id, 'command_input', completedID)
      record(`命令结束  ${completedID}  status=completed  exit=1  duration=324ms`, 2, connections[0].connection_id, 'execution_finished', completedID)
      record('SSH 连接已释放  reason=idle', 1, connections[2].connection_id, 'ssh_released')
      connections[1].active_execution_id = activeID
      record(`命令已受理  ${activeID}`, 2, connections[1].connection_id, 'execution_accepted', activeID)
      record('command="find /srv/builds -maxdepth 2 -type f"  cwd="/"', 3, connections[1].connection_id, 'command_input', activeID)
    }
    return status()
  }
  const stop = async () => {
    for (const timer of drainTimers.values()) clearTimeout(timer)
    drainTimers.clear()
    for (const connection of connections) { finish(connection, true); close(connection) }
    if (instance) record('MCP 服务已停止', 1, '', 'service_stopped')
    instance = null
    return status()
  }
  return {
    status: async () => status(),
    saveConfig: async value => { config = clone(value); return status() },
    start, stop,
    restart: async () => { await stop(); return start() },
    generateToken: async () => sampleToken(++tokenSequence),
    chooseTLSFile: async kind => kind === 'certificate' ? '/demo/server.crt' : '/demo/server.key',
    connections: async () => clone(connections),
    closeConnection: async (id, mode) => {
      const c = connections.find(c => c.connection_id === id)
      if (!c) throw new Error('连接不存在')
      if (c.active_execution_id) {
        if (mode === 'if_idle') throw new Error('连接仍有活动任务，请等待任务结束')
        if (!drainTimers.has(id)) {
          c.status = 'draining'
          record('连接等待当前任务结束后关闭', 1, id, 'connection_draining')
          drainTimers.set(id, setTimeout(() => { drainTimers.delete(id); close(c) }, 1000))
        }
      } else close(c)
      return clone(c)
    },
    logs: async (cursor, limit) => { const available = lines.filter(line => line.sequence > cursor), page = available.slice(0, limit); return { lines: clone(page), nextCursor: page.at(-1)?.sequence || sequence, hasMore: available.length > page.length, gap: cursor > 0 && cursor < (lines[0]?.sequence || 1) - 1 } },
  }
}
