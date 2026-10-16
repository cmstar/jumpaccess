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
  counts?: { connections: number; ssh_connections: number; connecting: number; executions: number; unknown_executions: number; draining: boolean }
  stopWarning?: string
}

export interface MCPBackend {
  status(): Promise<MCPStatus>
  saveConfig(value: MCPConfig): Promise<MCPStatus>
  start(): Promise<MCPStatus>
  stop(): Promise<MCPStatus>
  restart(): Promise<MCPStatus>
  generateToken(): Promise<string>
  chooseTLSFile(kind: 'certificate' | 'key', currentPath: string): Promise<string>
}
