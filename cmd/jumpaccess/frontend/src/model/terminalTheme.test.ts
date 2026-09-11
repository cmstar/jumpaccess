import { expect, test } from 'vitest'
import { ansiColorKeys, terminalDisplayOptions, terminalScheme, terminalSchemes } from './terminalTheme'

test('内置方案均包含完整 ANSI 色表、基础颜色和固定版本的开源来源', () => {
  expect(terminalSchemes.length).toBe(28)
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

test.each([
  ['campbell', 'Campbell', '#0c0c0c'],
  ['campbell-powershell', 'Campbell Powershell', '#012456'],
  ['cga', 'CGA', '#000000'],
  ['dark-plus', 'Dark+', '#1e1e1e'],
  ['dimidium', 'Dimidium', '#141414'],
  ['ibm-5153', 'IBM 5153', '#000000'],
  ['ottosson', 'Ottosson', '#000000'],
])('Windows Terminal 方案 %s 使用自身配色而非回退默认值', (id, name, background) => {
  expect(terminalScheme(id)).toMatchObject({ id, name, kind: 'dark', theme: { background } })
})

test.each([
  ['tango-dark', 'Tango Dark', 'dark', '#000000'],
  ['tango-light', 'Tango Light', 'light', '#ffffff'],
  ['vintage', 'Vintage', 'dark', '#000000'],
  ['vscode-dark-modern', 'VSCode Dark Modern', 'dark', '#1f1f1f'],
  ['vscode-light-modern', 'VSCode Light Modern', 'light', '#ffffff'],
  ['ubuntu-22-04', 'Ubuntu-22.04-ColorScheme', 'dark', '#300a24'],
  ['ubuntu-22-04-light', 'Ubuntu-22.04-Light-ColorScheme', 'light', '#f8f8f8'],
])('补全方案 %s 的名称、深浅分类和底色', (id, name, kind, background) => {
  expect(terminalScheme(id)).toMatchObject({ id, name, kind, theme: { background } })
})

test.each(['tango-light', 'vscode-dark-modern', 'vscode-light-modern', 'ubuntu-22-04', 'ubuntu-22-04-light'])(
  '%s 的不透明选区提供可辨识的文字颜色', (id) => {
    const scheme = terminalScheme(id)
    expect(scheme.id).toBe(id)
    function luminance(color: string) {
      const channels = [1, 3, 5].map((offset) => parseInt(color.slice(offset, offset + 2), 16) / 255)
        .map((value) => value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4)
      return channels[0] * 0.2126 + channels[1] * 0.7152 + channels[2] * 0.0722
    }
    const foreground = luminance(scheme.theme.selectionForeground!)
    const background = luminance(scheme.theme.selectionBackground!)
    expect((Math.max(foreground, background) + 0.05) / (Math.min(foreground, background) + 0.05)).toBeGreaterThanOrEqual(4.5)
  },
)

test('Campbell 保留官方 ANSI 色表并适配光标与选区', () => {
  const { theme } = terminalScheme('campbell')
  expect(ansiColorKeys.map((key) => theme[key])).toEqual([
    '#0c0c0c', '#c50f1f', '#13a10e', '#c19c00', '#0037da', '#881798', '#3a96dd', '#cccccc',
    '#767676', '#e74856', '#16c60c', '#f9f1a5', '#3b78ff', '#b4009e', '#61d6d6', '#f2f2f2',
  ])
  expect(theme).toMatchObject({ foreground: '#cccccc', cursor: '#ffffff', cursorAccent: '#0c0c0c', selectionBackground: '#cccccc55' })
})

test('预览与终端获得独立的主题对象，修改实例不会污染其他会话', () => {
  const preferences = { terminalColorScheme: 'nord', terminalFontFamily: 'monospace', terminalFontSize: 12, terminalLineHeight: 1, terminalCursorStyle: 'block' as const, terminalCursorBlink: true }
  const options = terminalDisplayOptions(preferences)
  options.theme.background = '#000000'
  expect(terminalDisplayOptions(preferences).theme.background).toBe('#2e3440')
})

test('共用渲染参数包含行高、光标样式和关闭的闪烁设置', () => {
  const preferences = { terminalColorScheme: 'nord', terminalFontFamily: 'monospace', terminalFontSize: 12, terminalLineHeight: 1.5, terminalCursorStyle: 'bar' as const, terminalCursorBlink: false }
  expect(terminalDisplayOptions(preferences)).toMatchObject({ lineHeight: 1.5, cursorStyle: 'bar', cursorBlink: false })
})

test.each(terminalSchemes)('$id 的滚动滑块使用普通文字颜色和 50% 透明度，背景图不改变滑块颜色', scheme => {
  const preferences = { terminalColorScheme: scheme.id, terminalFontFamily: 'monospace', terminalFontSize: 12, terminalLineHeight: 1, terminalCursorStyle: 'block' as const, terminalCursorBlink: true }
  for (const backgroundVisible of [false, true]) {
    expect(terminalDisplayOptions(preferences, backgroundVisible).theme).toMatchObject({
      scrollbarSliderBackground: `${scheme.theme.foreground.slice(0, 7)}80`,
      scrollbarSliderHoverBackground: `${scheme.theme.foreground.slice(0, 7)}80`,
      scrollbarSliderActiveBackground: `${scheme.theme.foreground.slice(0, 7)}80`,
    })
  }
})

test('底部四分之一方块映射到 xterm 下划线，不传入不支持的光标值', () => {
  const preferences = { terminalColorScheme: 'nord', terminalFontFamily: 'monospace', terminalFontSize: 12, terminalLineHeight: 1.5, terminalCursorStyle: 'quarter_block' as const, terminalCursorBlink: true }
  expect(terminalDisplayOptions(preferences)).toMatchObject({ cursorStyle: 'underline', cursorBlink: true })
})
