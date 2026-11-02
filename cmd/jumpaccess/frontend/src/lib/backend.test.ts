import { defaultTerminalBackground } from '../model/terminalBackground'
import { afterEach, expect, test, vi } from 'vitest'
import { wailsBackend } from './backend'

afterEach(() => vi.unstubAllGlobals())

test('MCP 适配统一转交配置，生成 Token 不附带保存操作', async () => {
  const config = { listenHost: '127.0.0.1', port: 39287, allowLAN: false, token: 'synthetic' }
  const status = { state: 'stopped', config, tokenConfigured: true, instance: null, restartRequired: false, error: '' }
  const methods = {
    GetMCPStatus: vi.fn().mockResolvedValue(status), SaveMCPConfig: vi.fn().mockResolvedValue(status),
    StartMCP: vi.fn().mockResolvedValue(status), StopMCP: vi.fn().mockResolvedValue(status),
    RestartMCP: vi.fn().mockResolvedValue(status), GenerateMCPToken: vi.fn().mockResolvedValue('synthetic'), ChooseMCPTLSFile: vi.fn().mockResolvedValue('/cert.pem'),
  }
  vi.stubGlobal('go', { main: { desktopApp: methods } })
  expect(await wailsBackend.mcp!.status()).toBe(status)
  await wailsBackend.mcp!.saveConfig(config)
  expect(methods.SaveMCPConfig).toHaveBeenCalledWith(config)
  await wailsBackend.mcp!.start()
  await wailsBackend.mcp!.stop()
  expect(methods.StartMCP).toHaveBeenCalledOnce()
  expect(methods.StopMCP).toHaveBeenCalledOnce()
  await wailsBackend.mcp!.restart()
  expect(methods.RestartMCP).toHaveBeenCalledOnce()
  expect(await wailsBackend.mcp!.generateToken()).toBe('synthetic')
  expect(methods.GenerateMCPToken).toHaveBeenCalledWith()
  expect(methods.SaveMCPConfig).toHaveBeenCalledOnce()
  expect(await wailsBackend.mcp!.chooseTLSFile('certificate', '/old.pem')).toBe('/cert.pem')
  expect(methods.ChooseMCPTLSFile).toHaveBeenCalledWith('certificate', '/old.pem')
})

test.each(['block', 'bar', 'underline', 'quarter_block'])('Wails 偏好映射双向保留行高、%s 光标和关闭的闪烁值', async (style) => {
  const preferences = {
    Downloads: { Mode: 'remember', Directory: 'D:/下载' }, Version: 12, Appearance: { Theme: 'light' },
    Terminal: { Background: { ...defaultTerminalBackground, enabled: true, filePath: 'D:/图片/a.png', transparencyPercent: 0, positionXPercent: 0, positionYPercent: 100, fitMode: 'tile', tileFitLongEdge: true, tileOnlyWholeTiles: true }, FontFamily: 'monospace', FontSize: 12, ColorScheme: 'nord', LineHeight: 1.25, CursorStyle: style, CursorBlink: false, RightClickAction: 'paste', WarnOnMultiLinePaste: true },
    Tabs: { ConfirmCloseActiveSession: true, ShowCloseButtons: true, NewTabPosition: 'after_current' },
    MCP: { AutoStart: true },
  }
  const save = vi.fn().mockResolvedValue(undefined)
  Object.assign(preferences.Terminal, { ScrollbarVisibility: 'hidden' })
  Object.assign(preferences.Terminal, { CopyOnEnter: false })
  Object.assign(preferences.Terminal, { ShowStatusBar: false, FullscreenHideToolbar: false })
  vi.stubGlobal('go', { main: { desktopApp: { Bootstrap: vi.fn().mockResolvedValue({ preferences }), SavePreferences: save } } })
  const state = await wailsBackend.bootstrap()
  expect(state.preferences).toMatchObject({ terminalScrollbarVisibility: 'hidden' })
  expect(state.preferences.newTabPosition).toBe('after_current')
  expect(state.preferences.mcpAutoStart).toBe(true)
  expect(state.preferences.terminalCopyOnEnter).toBe(false)
  expect(state.preferences.terminalShowStatusBar).toBe(false)
  expect(state.preferences.terminalFullscreenHideToolbar).toBe(false)
  expect(state.preferences.downloadMode).toBe('remember')
  expect(state.preferences.downloadDirectory).toBe('D:/下载')
  expect(state.preferences).toMatchObject({ terminalLineHeight: 1.25, terminalCursorStyle: style, terminalCursorBlink: false })
  await wailsBackend.savePreferences(state.preferences)
  expect(save).toHaveBeenCalledWith(preferences)
})
