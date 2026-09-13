export interface TransferCapabilities { checked: boolean; upload: boolean; download: boolean }
export interface TransferFile { id: string; name: string; path: string; size: number }
export interface ZmodemBackend {
  probeSSHTransferCommands(id: string): Promise<TransferCapabilities>
  writeSSHBinary(id: string, data: string): Promise<void>
  chooseZmodemUploadFiles(id: string): Promise<TransferFile[]>
  chooseZmodemDownloadDirectory(id: string): Promise<string>
  createZmodemDownload(session: string, grant: string, name: string, size: number): Promise<TransferFile>
  readZmodemFile(id: string): Promise<string>
  writeZmodemFile(id: string, data: string): Promise<void>
  closeZmodemFile(id: string, complete: boolean): Promise<void>
  endZmodemTransfer(id: string): Promise<void>
}
export interface ZmodemState extends TransferCapabilities {
  busy: boolean
  direction?: 'upload' | 'download'
  name?: string
  transferred?: number
  size?: number
  message?: string
}
