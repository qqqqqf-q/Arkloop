import { useState } from 'react'
import { isDesktop } from '@arkloop/shared/desktop'
import { TabBar } from '@arkloop/shared/components/prompt-injection'
import { ConnectorsSettings } from './ConnectorsSettings'
import { SearchFetchSettings } from './SearchFetchSettings'
import { useLocale } from '../../contexts/LocaleContext'

type Tab = 'connectors' | 'searchFetch'

type Props = {
  accessToken: string
}

export function ToolsSettings({ accessToken }: Props) {
  const { t } = useLocale()
  const ds = t.desktopSettings
  const desktop = isDesktop()
  const [activeTab, setActiveTab] = useState<Tab>('searchFetch')

  if (!desktop) {
    return <ConnectorsSettings accessToken={accessToken} />
  }

  const tabs: { key: Tab; label: string }[] = [
    { key: 'searchFetch', label: ds.desktopConnectorsTitle },
    { key: 'connectors', label: ds.connectorsTitle },
  ]

  return (
    <div className="flex h-full min-h-0 flex-col">
      <TabBar tabs={tabs} active={activeTab} onChange={setActiveTab} className="mb-3 shrink-0" />
      <div className="-mx-6 shrink-0 border-t border-[var(--c-border-subtle)]" />
      <div className="min-h-0 flex-1 pt-3">
        {activeTab === 'connectors' && <ConnectorsSettings accessToken={accessToken} nestedUnderTabs />}
        {activeTab === 'searchFetch' && <SearchFetchSettings accessToken={accessToken} />}
      </div>
    </div>
  )
}
