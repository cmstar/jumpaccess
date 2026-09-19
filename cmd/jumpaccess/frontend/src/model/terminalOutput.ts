// start 是文本在整个会话输出中的绝对字符位置，不随历史裁剪归零。
export interface TerminalOutput {
  text: string
  start: number
}

export function appendTerminalOutput(current: TerminalOutput | undefined, data: string, limit: number): TerminalOutput {
  const combined = (current?.text ?? '') + data
  const removed = Math.max(0, combined.length - limit)
  return { text: combined.slice(removed), start: (current?.start ?? 0) + removed }
}
