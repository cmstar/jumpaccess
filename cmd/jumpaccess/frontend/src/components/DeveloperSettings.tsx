import { Bug, CircleAlert, Info, TriangleAlert } from 'lucide-react'
import { useNotifications } from './Notifications'

export function DeveloperSettings() {
  const { showInfo, showWarning, showError } = useNotifications()
  return <section className="settings-card" id="settings-debug">
    <div className="settings-card-title"><Bug /><div><h2>开发调试</h2><p>集中预览和验证界面功能。</p></div></div>
    <div className="settings-group">
      <h3>提示信息演示</h3>
      <p className="setting-help">普通消息 3 秒、警告 5 秒后自动关闭，错误需手动关闭。</p>
      <div className="settings-debug-actions">
        <button className="button secondary small" type="button" onClick={() => showInfo('这是一条普通消息，用于预览提示效果。')}><Info />演示普通消息</button>
        <button className="button secondary small" type="button" onClick={() => showWarning('这是一条警告，用于预览提示效果。')}><TriangleAlert />演示警告</button>
        <button className="button secondary small" type="button" onClick={() => showError('这是一条错误，用于预览提示效果。')}><CircleAlert />演示错误</button>
      </div>
    </div>
  </section>
}
