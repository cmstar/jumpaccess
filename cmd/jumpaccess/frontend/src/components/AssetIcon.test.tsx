import { render, screen } from '@testing-library/react'
import { expect, test } from 'vitest'
import { AssetIcon } from './AssetIcon'

test.each([
  ['linux', 'host', 'Linux', 'server'],
  ['windows', 'host', 'Windows', 'monitor'],
  ['mysql', 'database', 'MySQL', 'database'],
  ['cisco', 'device', 'Cisco', 'network'],
  ['k8s', 'cloud', 'Kubernetes', 'cloud'],
  ['website', 'web', '网站', 'globe'],
  ['ldap', 'ds', '目录服务', 'contact'],
  ['chatgpt', 'gpt', 'ChatGPT', 'bot'],
  ['custom-db', 'database', '自定义数据库', 'database'],
  ['future', 'future-category', '未来平台', 'box'],
  ['constructor', '__proto__', '自定义平台', 'box'],
])('按服务端类型 %s 和类别 %s 选择图标，保留显示名称', (typeValue, categoryValue, type, glyph) => {
  render(<AssetIcon asset={{ type, category: '本地化类别', typeValue, categoryValue }} />)
  expect(screen.getByRole('img', { name: `${type} 资产` })).toHaveClass(`lucide-${glyph}`)
})

test('旧数据缺少类型标识时仍可识别类型名称', () => {
  render(<AssetIcon asset={{ type: 'MySQL', category: 'Database' }} />)
  expect(screen.getByRole('img', { name: 'MySQL 资产' })).toHaveClass('lucide-database')
})
