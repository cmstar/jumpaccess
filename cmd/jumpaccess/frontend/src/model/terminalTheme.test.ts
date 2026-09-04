import { expect, test } from 'vitest'
import { ansiColorKeys, terminalDisplayOptions, terminalSchemes } from './terminalTheme'

test('内置方案均包含完整 ANSI 色表、基础颜色和固定版本的开源来源', () => {
  expect(terminalSchemes.length).toBe(14)
  expect(new Set(terminalSchemes.map((scheme) => scheme.id)).size).toBe(terminalSchemes.length)
  expect(new Set(terminalSchemes.map((scheme) => scheme.kind))).toEqual(new Set(['dark', 'light']))
  for (const scheme of terminalSchemes) {
    expect(['MIT', 'Apache-2.0']).toContain(scheme.license)
    expect(scheme.source).toMatch(/\/[a-f0-9]{40}\//)
    for (const key of [...ansiColorKeys, 'background', 'foreground', 'cursor', 'cursorAccent', 'selectionBackground'] as const) {
      expect(scheme.theme[key], `${scheme.id}.${key}`).toMatch(/^#[a-f0-9]{6}([a-f0-9]{2})?$/)
    }
  }
})

test('预览与终端获得独立的主题对象，修改实例不会污染其他会话', () => {
  const preferences = { terminalColorScheme: 'nord', terminalFontFamily: 'monospace', terminalFontSize: 12 }
  const options = terminalDisplayOptions(preferences)
  options.theme.background = '#000000'
  expect(terminalDisplayOptions(preferences).theme.background).toBe('#2e3440')
})
