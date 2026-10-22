export interface MCPConfig {
  listenHost: string
  port: number
  allowLAN: boolean
  tlsCert?: string
  tlsKey?: string
  token?: string
}

export interface MCPStatus {
  state: 'stopped' | 'running' | 'external' | 'failed'
  instance: { id: string; host: string; url: string; startedAt: string; config: MCPConfig } | null
  config: MCPConfig
  tokenConfigured: boolean
  restartRequired: boolean
  error: string
  counts?: { connections: number; ssh_connections: number; connecting: number; executions: number; unknown_executions: number; downloads?: number; unknown_downloads?: number; download_transport_slots?: number; draining: boolean }
  stopWarning?: string
}

export interface MCPBackend {
  connections(): Promise<MCPConnection[]>
  closeConnection(id: string, mode: 'if_idle' | 'drain'): Promise<MCPConnection>
  logs(cursor: number, limit: number): Promise<MCPLogPage>
  status(): Promise<MCPStatus>
  saveConfig(value: MCPConfig): Promise<MCPStatus>
  start(): Promise<MCPStatus>
  stop(): Promise<MCPStatus>
  restart(): Promise<MCPStatus>
  generateToken(): Promise<string>
  chooseTLSFile(kind: 'certificate' | 'key', currentPath: string): Promise<string>
}

export interface MCPConnection {
  connection_id: string
  target: { profile: string; organization?: string; asset_id?: string; account_id?: string; alias?: string }
  status: string
  transport_connected: boolean
  created_at: string
  last_used_at: string
  active_execution_id?: string
  close_reason?: string
  last_error?: { code: string; message: string }
  recent_unknown_execution_ids?: string[]
}

export interface MCPLogLine {
  sequence: number
  time: string
  instance: string
  severity: 'DEBUG' | 'INFO' | 'WARN' | 'ERROR'
  detail: number
  connectionID?: string
  executionID?: string
  event: string
  text: string
}

export interface MCPLogPage {
  lines: MCPLogLine[]
  nextCursor: number
  hasMore: boolean
  gap: boolean
}
