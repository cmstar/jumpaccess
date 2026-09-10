import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { expect, test, vi } from 'vitest'
import { TerminalBackgroundSettings } from './TerminalBackgroundSettings'
import { defaultTerminalBackground } from '../model/terminalBackground'
import type { Backend, Preferences } from '../lib/backend'

vi.mock('./TerminalPreview', () => ({ TerminalPreview: () => <div>预览</div> }))

test('图片选择取消不改配置，选择后启用，关闭保留参数，清除移除路径', async () => {
  const choose = vi.fn().mockResolvedValueOnce('').mockResolvedValueOnce('D:/图片/a.png')
  const backend = { chooseTerminalBackground: choose } as unknown as Backend
  const onChange = vi.fn()
  const preferences = { terminalBackground: { ...defaultTerminalBackground } } as Preferences
  const { rerender } = render(<TerminalBackgroundSettings backend={backend} preferences={preferences} onChange={onChange} />)
  fireEvent.click(screen.getByRole('button', { name: '选择图片' }))
  await waitFor(() => expect(screen.getByRole('button', { name: '选择图片' })).toBeEnabled())
  expect(onChange).not.toHaveBeenCalled()
  fireEvent.click(screen.getByRole('button', { name: '选择图片' }))
  await waitFor(() => expect(onChange).toHaveBeenCalledWith({ ...defaultTerminalBackground, enabled: true, filePath: 'D:/图片/a.png' }))
  const chosen = onChange.mock.calls[0][0]
  rerender(<TerminalBackgroundSettings backend={backend} preferences={{ ...preferences, terminalBackground: chosen }} onChange={onChange} />)
  fireEvent.click(screen.getByRole('switch', { name: '启用背景图' }))
  expect(onChange).toHaveBeenLastCalledWith({ ...chosen, enabled: false })
  fireEvent.click(screen.getByRole('button', { name: '清除' }))
  expect(onChange).toHaveBeenLastCalledWith({ ...chosen, enabled: false, filePath: '' })
})

test('显示模式仅展示相关参数，滑块数值清晰且按交互结束保存', () => {
  const onChange = vi.fn()
  const preferences = { terminalBackground: { ...defaultTerminalBackground } } as Preferences
  const { rerender } = render(<TerminalBackgroundSettings backend={{} as Backend} preferences={preferences} onChange={onChange} />)
  expect(screen.getByLabelText('水平位置')).toBeInTheDocument()
  expect(screen.queryByRole('switch', { name: '长边适配终端区域' })).not.toBeInTheDocument()
  fireEvent.change(screen.getByLabelText('图片透明度'), { target: { value: '85' } })
  expect(onChange).not.toHaveBeenCalled()
  fireEvent.pointerUp(screen.getByLabelText('图片透明度'))
  expect(onChange).toHaveBeenCalledWith({ ...defaultTerminalBackground, transparencyPercent: 85 })
  rerender(<TerminalBackgroundSettings backend={{} as Backend} preferences={{ ...preferences, terminalBackground: { ...defaultTerminalBackground, fitMode: 'tile' } }} onChange={onChange} />)
  expect(screen.queryByLabelText('水平位置')).not.toBeInTheDocument()
  expect(screen.getByRole('switch', { name: '长边适配终端区域' })).toBeInTheDocument()
})
