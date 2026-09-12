import { Bot, Box, Cloud, Contact, Database, Globe, Monitor, Network, Server, type LucideIcon } from 'lucide-react'
import type { Asset } from '../lib/backend'

type AssetType = Pick<Asset, 'type' | 'category' | 'typeValue' | 'categoryValue'>

const typeIcons: Record<string, LucideIcon> = {
  linux: Server, unix: Server, windows: Monitor,
  mysql: Database, mariadb: Database, postgresql: Database, oracle: Database,
  sqlserver: Database, redis: Database, mongodb: Database, clickhouse: Database,
  dameng: Database, db2: Database,
  cisco: Network, huawei: Network, h3c: Network, general: Network,
  k8s: Cloud, kubernetes: Cloud, vmware: Cloud, vsphere: Cloud,
  website: Globe, ldap: Contact, activedirectory: Contact, chatgpt: Bot,
}
const categoryIcons: Record<string, LucideIcon> = {
  host: Server, device: Network, database: Database, cloud: Cloud,
  web: Globe, ds: Contact, directoryservice: Contact, gpt: Bot,
}

export function AssetIcon({ asset }: { asset: AssetType }) {
  // 标识用于选择图标；服务端显示名称仍按原样呈现，未知类型保留通用图标。
  const type = (asset.typeValue || asset.type).toLowerCase()
  const category = (asset.categoryValue || asset.category).toLowerCase()
  const Icon = (Object.hasOwn(typeIcons, type) ? typeIcons[type] : undefined)
    ?? (Object.hasOwn(categoryIcons, category) ? categoryIcons[category] : Box)
  return <Icon role="img" aria-label={`${asset.type || asset.category || '未知类型'} 资产`} />
}
