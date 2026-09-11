import { defaultTerminalBackground } from '../model/terminalBackground'
import { afterEach, expect, test, vi } from 'vitest'
import { wailsBackend } from './backend'

afterEach(() => vi.unstubAllGlobals())

test.each(['block', 'bar', 'underline', 'quarter_block'])('Wails 偏好映射双向保留行高、%s 光标和关闭的闪烁值', async (style) => {
  const preferences = {
    Version: 6, Appearance: { Theme: 'light' },
    Terminal: { Background: { ...defaultTerminalBackground, enabled: true, filePath: 'D:/图片/a.png', transparencyPercent: 0, positionXPercent: 0, positionYPercent: 100, fitMode: 'tile', tileFitLongEdge: true, tileOnlyWholeTiles: true }, FontFamily: 'monospace', FontSize: 12, ColorScheme: 'nord', LineHeight: 1.25, CursorStyle: style, CursorBlink: false, RightClickAction: 'paste', WarnOnMultiLinePaste: true },
    Tabs: { ConfirmCloseActiveSession: true, ShowCloseButtons: true, NewTabPosition: 'after_current' },
  }
  const save = vi.fn().mockResolvedValue(undefined)
  Object.assign(preferences.Terminal, { ScrollbarVisibility: 'hidden' })
  Object.assign(preferences.Terminal, { CopyOnEnter: false })
  vi.stubGlobal('go', { main: { desktopApp: { Bootstrap: vi.fn().mockResolvedValue({ preferences }), SavePreferences: save } } })
  const state = await wailsBackend.bootstrap()
  expect(state.preferences).toMatchObject({ terminalScrollbarVisibility: 'hidden' })
  expect(state.preferences.newTabPosition).toBe('after_current')
  expect(state.preferences.terminalCopyOnEnter).toBe(false)
  expect(state.preferences).toMatchObject({ terminalLineHeight: 1.25, terminalCursorStyle: style, terminalCursorBlink: false })
  await wailsBackend.savePreferences(state.preferences)
  expect(save).toHaveBeenCalledWith(preferences)
})
