import { useNotifications } from './Notifications'

export function useCopyText() {
  const { showInfo, showWarning, showError } = useNotifications()
  return async (text: string) => {
    if (!navigator.clipboard?.writeText) {
      showWarning('当前环境无法访问剪贴板，请手动复制。')
      return
    }
    try {
      await navigator.clipboard.writeText(text)
      showInfo('已复制到剪贴板')
    } catch {
      showError('复制失败，请重试或手动复制。')
    }
  }
}
