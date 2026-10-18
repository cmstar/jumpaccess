import type { MCPBackend, MCPConfig, MCPConnection, MCPLogLine, MCPStatus } from './mcpTypes'

// 独立浏览器预览仅操作模拟内存，不监听端口或执行命令。
export function createMCPPreviewBackend(): MCPBackend {
  let config: MCPConfig = { listenHost: '127.0.0.1', port: 39287, allowLAN: false, token: 'A'.repeat(43), tlsCert: '', tlsKey: '' }
  let instance: MCPStatus['instance'] = null, runtimeToken = '', sequence = 0
  let connections: MCPConnection[] = []
  let lines: MCPLogLine[] = []
  const clone = <T,>(value: T): T => JSON.parse(JSON.stringify(value))
  const record = (text: string, detail = 1, connectionID = '', event = 'service') => {
    lines.push({ sequence: ++sequence, time: new Date().toISOString(), instance: instance?.id || '', severity: detail === 3 ? 'DEBUG' : 'INFO', detail, connectionID, event, text })
    lines = lines.slice(-3000)
  }
  const status = (): MCPStatus => clone({ state: instance ? 'running' : 'stopped', instance, config, tokenConfigured: Boolean(config.token), restartRequired: Boolean(instance && (runtimeToken !== config.token || Object.keys(instance.config).some(key => instance!.config[key as keyof MCPConfig] !== config[key as keyof MCPConfig]))), error: '', counts: instance ? { connections: connections.length, ssh_connections: connections.filter(c => c.transport_connected).length, executions: 0, connecting: 0, unknown_executions: 0, draining: false } : undefined })
  const start = async () => {
    if (!instance) {
      if (!config.token) throw new Error('请先保存 Bearer Token')
      const { token, ...publicConfig } = config
      runtimeToken = token
      instance = { id: `preview-${Date.now()}`, host: 'gui', url: `${config.tlsCert ? 'https' : 'http'}://${config.listenHost}:${config.port}/mcp`, startedAt: new Date().toISOString(), config: publicConfig }
      record('MCP 服务已启动（浏览器演示）')
      record('list_assets  调用完成  count=3  total=3  has_more=false  duration=28ms', 2, '', 'tool_call')
      record('list_assets  查询条件  profile="office"  organization="研发中心"  search=""  limit=100  has_cursor=false', 3, '', 'tool_conditions')
      connections = ['dev-sandbox', 'build-runner', 'database'].map((alias, i) => ({ connection_id: `${instance!.id}:c:${i + 1}`, target: { profile: 'office', organization: '研发中心', asset_id: `demo-asset-${i + 1}`, alias, account_id: 'ops' }, status: i === 2 ? 'disconnected' : 'ready', transport_connected: i !== 2, created_at: new Date().toISOString(), last_used_at: new Date().toISOString() }))
      for (const c of connections) record('逻辑连接已建立', 1, c.connection_id, 'connection_open')
      record('command="find /var/log -maxdepth 1 -type f"  cwd="/"', 3, connections[0].connection_id, 'command_input')
      record('命令结束  exit=7  duration=324ms', 2, connections[0].connection_id, 'execution_finished')
    }
    return status()
  }
  const stop = async () => { if (instance) record('MCP 服务已停止'); instance = null; connections = []; return status() }
  return {
    status: async () => status(),
    saveConfig: async value => { config = clone(value); return status() },
    start, stop,
    restart: async () => { await stop(); return start() },
    generateToken: async () => { const bytes = crypto.getRandomValues(new Uint8Array(32)); return btoa(String.fromCharCode(...bytes)).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '') },
    chooseTLSFile: async kind => kind === 'certificate' ? '/demo/server.crt' : '/demo/server.key',
    connections: async () => clone(connections),
    closeConnection: async id => { const c = connections.find(c => c.connection_id === id); if (!c) throw new Error('连接不存在'); connections = connections.filter(item => item !== c); record('逻辑连接已关闭', 1, id, 'connection_closed'); return clone({ ...c, status: 'closed', transport_connected: false }) },
    logs: async (cursor, limit) => { const available = lines.filter(line => line.sequence > cursor), page = available.slice(0, limit); return { lines: clone(page), nextCursor: page.at(-1)?.sequence || sequence, hasMore: available.length > page.length, gap: cursor > 0 && cursor < (lines[0]?.sequence || 1) - 1 } },
  }
}
