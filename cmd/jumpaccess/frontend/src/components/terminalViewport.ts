export function synchronizeTerminalViewportBackground(host: HTMLElement, background: string) {
  host.style.backgroundColor = background
  const viewport = host.querySelector<HTMLElement>('.xterm-viewport')
  if (viewport) viewport.style.backgroundColor = background
}
