// @ts-expect-error Vitest 在 Node 中运行，前端生产类型配置不加载 Node 声明。
import { readFileSync } from 'node:fs'
import { expect, test } from 'vitest'

const appStyles = readFileSync('src/App.css', 'utf8')
const baseStyles = readFileSync('src/style.css', 'utf8')

test('应用界面使用 125% 全局字体缩放且不保留固定像素字号', () => {
  expect(baseStyles).toMatch(/:root\s*{[^}]*font-size:\s*125%/s)
  expect(appStyles).not.toMatch(/font-size:\s*\d+(?:\.\d+)?px/)
})

test('应用界面使用清晰的 Windows 原生字体和正文基准字重', () => {
  expect(baseStyles).toMatch(/font-family:\s*"Segoe UI Variable Text",\s*"Segoe UI",\s*"Microsoft YaHei UI"/)
  expect(baseStyles).toMatch(/body\s*{[^}]*font-weight:\s*450/s)
})

test('顶部 Tab 保持放大前的视觉字号', () => {
  expect(appStyles).toMatch(/\.tab-primary\s*{[^}]*font-size:\s*\.6rem/s)
  expect(appStyles).toMatch(/\.tab-activate small\s*{[^}]*font-size:\s*\.5rem/s)
})

test('Profile 认证状态点固定在图标右下角', () => {
  expect(appStyles).toMatch(/\.profile-status-icon\s*{[^}]*position:\s*relative[^}]*width:\s*15px[^}]*height:\s*15px/s)
  expect(appStyles).toMatch(/\.profile-status-icon \.auth-indicator\s*{[^}]*right:\s*-3px[^}]*bottom:\s*-3px/s)
  expect(appStyles).not.toMatch(/\.auth-status(?:\s|\.|\{|:)/)
})

test('设置页使用清晰的标题、正文和控件层级', () => {
  expect(appStyles).toMatch(/\.settings-card-title h2\s*{[^}]*font-size:\s*\.8rem[^}]*font-weight:\s*650/s)
  expect(appStyles).toMatch(/\.settings-card-title p\s*{[^}]*font-size:\s*\.65rem[^}]*font-weight:\s*450/s)
  expect(appStyles).toMatch(/\.segmented-control button\s*{[^}]*font-size:\s*\.7rem[^}]*font-weight:\s*600/s)
  expect(appStyles).toMatch(/\.setting-row strong\s*{[^}]*font-size:\s*\.7rem[^}]*font-weight:\s*650/s)
  expect(appStyles).toMatch(/\.setting-row small\s*{[^}]*font-size:\s*\.65rem[^}]*font-weight:\s*450/s)
})

test('Asset ID 保留开头并在末尾显示省略号', () => {
  expect(appStyles).toMatch(/\.asset-id-value > \.asset-id-text\s*{[^}]*min-width:\s*0[^}]*overflow:\s*hidden[^}]*text-overflow:\s*ellipsis[^}]*white-space:\s*nowrap/s)
  expect(appStyles).toMatch(/\.asset-id-value > button\s*{[^}]*flex:\s*0 0 auto/s)
})
