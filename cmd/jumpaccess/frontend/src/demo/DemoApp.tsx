import App from '../App'
import { createPreviewBackend, type DemoScenario } from '../lib/previewBackend'
import './demo.css'

const scenarios: { value: DemoScenario; label: string }[] = [
  { value: 'ready', label: '已配置环境' },
  { value: 'empty', label: '首次使用' },
  { value: 'expired', label: '登录已过期' },
]
const requested = new URLSearchParams(window.location.search).get('scenario')
const scenario = scenarios.find(item => item.value === requested)?.value ?? 'ready'
const backend = createPreviewBackend({ scenario })

export function DemoApp() {
  return <div className="demo-root">
    <aside className="demo-toolbar" aria-label="演示控制">
      <strong>JumpAccess 演示</strong>
      <span>数据仅保存在内存 · 不连接真实服务器</span>
      <label>场景 <select aria-label="演示场景" value={scenario} onChange={event => {
        const url = new URL(window.location.href)
        url.searchParams.set('scenario', event.target.value)
        window.location.assign(url.href)
      }}>{scenarios.map(item => <option key={item.value} value={item.value}>{item.label}</option>)}</select></label>
      <button type="button" onClick={() => window.location.reload()}>重置演示</button>
      <details><summary>操作提示</summary><div className="demo-help">
        <p>可修改 Profile、Alias、主题，体验模拟 SSH 和 SFTP；刷新页面恢复样例。</p>
        <p>终端输入 help 查看示例命令。登录框输入 demo 即可完成模拟登录，不会打开授权网站。</p>
        <p>SFTP 上传使用样例文件，下载仅更新内存，不读写本机文件。原生窗口操作、打开配置、背景图文件选择和 ZMODEM 暂不演示。</p>
      </div></details>
    </aside>
    <App backend={backend} />
  </div>
}
