export function synchronizeTerminalViewportBackground(host: HTMLElement, background: string) {
  host.style.backgroundColor = background
  // 背景图使终端底色透明，但滚动条槽底仍使用方案原本的纯色。
  host.style.setProperty('--terminal-scrollbar-track', background.slice(0, 7))
  const viewport = host.querySelector<HTMLElement>('.xterm-viewport')
  if (viewport) viewport.style.backgroundColor = background
}
