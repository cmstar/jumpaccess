import { useCallback, useRef, useState } from 'react'
import type { HostKeyPrompt } from '../lib/backend'

export function useHostKeyPrompts(resolve: (id: string, accepted: boolean) => Promise<void>) {
  const [prompts, setPrompts] = useState<HostKeyPrompt[]>([])
  const deciding = useRef(new Set<string>())
  const enqueue = useCallback((prompt: HostKeyPrompt) => {
    setPrompts(current => current.some(item => item.id === prompt.id) ? current : [...current, prompt])
  }, [])
  async function decide(accepted: boolean) {
    const prompt = prompts[0]
    if (!prompt || deciding.current.has(prompt.id)) return
    deciding.current.add(prompt.id)
    try {
      await resolve(prompt.id, accepted)
    } finally {
      // 会话取消后后端可能已移除请求；报告错误，同时放行其他等待确认的连接。
      setPrompts(current => current.filter(item => item.id !== prompt.id))
      deciding.current.delete(prompt.id)
    }
  }
  return { prompt: prompts[0], enqueue, decide }
}
