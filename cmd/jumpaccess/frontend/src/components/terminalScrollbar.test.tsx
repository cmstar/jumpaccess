import { render } from '@testing-library/react'
import { expect, test } from 'vitest'
import appStyles from '../App.css?inline'

test.each(['terminal-host', 'terminal-preview-host'])('%s 隐藏滚动条控件并保留滚动容器', className => {
  const view = (visible: boolean) => <>
    <style>{appStyles}</style>
    <div className={className} data-terminal-show-scrollbar={visible}>
      <div className="xterm"><div className="xterm-viewport" /></div>
      <div className="xterm-scrollable-element">
        <div className="scrollbar vertical visible"><div className="slider" /></div>
      </div>
    </div>
    <div className="scrollbar vertical" data-testid="other-scrollbar" />
  </>
  const { container, rerender, getByTestId } = render(view(true))
  const scrollbar = container.querySelector('.xterm-scrollable-element > .scrollbar')!
  const viewport = container.querySelector('.xterm-viewport')!
  expect(getComputedStyle(scrollbar).display).not.toBe('none')
  rerender(view(false))
  expect(getComputedStyle(scrollbar).display).toBe('none')
  expect(getComputedStyle(viewport).scrollbarWidth).toBe('none')
  expect(getComputedStyle(scrollbar.parentElement!).display).not.toBe('none')
  expect(getComputedStyle(getByTestId('other-scrollbar')).display).not.toBe('none')
  rerender(view(true))
  expect(getComputedStyle(scrollbar).display).not.toBe('none')
})
