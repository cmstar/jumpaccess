import { expect, test } from 'vitest'
import { appendTerminalOutput } from './terminalOutput'

test('历史裁剪保留绝对游标，包括连续相同内容和大于上限的块', () => {
  const first = appendTerminalOutput(undefined, 'abcd', 4)
  const second = appendTerminalOutput(first, 'abcd', 4)
  expect(first).toEqual({ text: 'abcd', start: 0 })
  expect(second).toEqual({ text: 'abcd', start: 4 })
  expect(appendTerminalOutput(second, '012345', 4)).toEqual({ text: '2345', start: 10 })
})
