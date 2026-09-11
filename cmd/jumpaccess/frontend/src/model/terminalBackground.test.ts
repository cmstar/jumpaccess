import { expect, test } from 'vitest'
import { backgroundLayout } from './terminalBackground'
import { terminalDisplayOptions, terminalScheme } from './terminalTheme'

test('完整图块居中，长边按终端尺寸缩放，区域不足时完整缩小一张', () => {
  expect(backgroundLayout(300, 200, 800, 500, false, true)).toEqual({ tileWidth: 300, tileHeight: 200, width: 600, height: 400 })
  const fitted = backgroundLayout(300, 200, 800, 500, true, false)!
  expect(fitted).toMatchObject({ tileWidth: 800, width: 800, height: 500 })
  expect(fitted.tileHeight).toBeCloseTo(800 * 200 / 300)
  expect(backgroundLayout(1000, 500, 400, 300, false, true)).toEqual({ tileWidth: 400, tileHeight: 200, width: 400, height: 200 })
  expect(backgroundLayout(0, 0, 800, 500, false, true)).toBeUndefined()
})

test('图片只使默认背景透明，保留 ANSI、选区、光标及反色需要的底色 RGB', () => {
  const preferences = { terminalColorScheme: 'nord', terminalFontFamily: 'monospace', terminalFontSize: 12, terminalLineHeight: 1, terminalCursorStyle: 'block' as const, terminalCursorBlink: true }
  const base = terminalDisplayOptions(preferences, false).theme
  expect(base).toMatchObject(terminalScheme('nord').theme)
  const options = terminalDisplayOptions(preferences, true)
  expect(options.allowTransparency).toBe(true)
  expect(options.theme).toEqual({ ...base, background: `${base.background}00` })
  expect(terminalDisplayOptions(preferences, false).theme).toEqual(base)
})
