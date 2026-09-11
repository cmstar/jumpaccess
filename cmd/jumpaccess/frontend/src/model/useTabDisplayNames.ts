import { useCallback, useEffect, useMemo, useState } from 'react'
import type { Account, Backend, Organization } from '../lib/backend'
import { type AppTab, type ConnectionTab, isConnectionTab } from './tabs'

export interface TabDisplayNames {
  organization: string
  account: string
}

export function useTabDisplayNames(backend: Backend, tabs: AppTab[]) {
  const [, setRevision] = useState(0)
  const cache = useMemo(() => ({
    organizations: new Map<string, Organization[]>(),
    accounts: new Map<string, Account[]>(),
    pending: new Set<string>(),
  }), [backend])

  const load = useCallback((tab: ConnectionTab) => {
    const { profile, organization, assetID, target, account } = tab.descriptor
    const asset = assetID || target
    const accountKey = JSON.stringify([profile, organization, asset])

    function request<T>(key: string, values: Map<string, T>, valueKey: string, fetch: () => Promise<T>) {
      if (values.has(valueKey) || cache.pending.has(key)) return
      cache.pending.add(key)
      void fetch().then((result) => values.set(valueKey, result))
        .catch(() => undefined)
        .finally(() => {
          cache.pending.delete(key)
          setRevision((revision) => revision + 1)
        })
    }

    if (profile && organization) {
      request(`org:${profile}`, cache.organizations, profile, () => backend.listOrganizations(profile))
    }
    if (profile && organization && asset && account) {
      request(`account:${accountKey}`, cache.accounts, accountKey,
        async () => (await backend.getAsset({ profile, organization, asset })).accounts)
    }
  }, [backend, cache])

  useEffect(() => {
    tabs.filter(isConnectionTab).forEach(load)
  }, [tabs, load])

  function names(tab: ConnectionTab): TabDisplayNames {
    const { profile, organization, assetID, target, account } = tab.descriptor
    const accounts = cache.accounts.get(JSON.stringify([profile, organization, assetID || target]))
    // ID 优先匹配，兼容旧工作区及 Alias 中保存的用户名、账号名或别名。
    const selected = accounts?.find((item) => item.id === account)
      ?? accounts?.find((item) => [item.username, item.name, item.alias].includes(account))
    return {
      organization: cache.organizations.get(profile)?.find((item) => item.id === organization)?.name || '名称暂不可用',
      account: selected?.name || selected?.username || selected?.alias || '名称暂不可用',
    }
  }

  return { names, load }
}
