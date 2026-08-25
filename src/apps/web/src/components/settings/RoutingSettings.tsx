import type { ReactNode } from 'react'
import { useLocale } from '../../contexts/LocaleContext'
import { ChatModelSettingControl } from './ChatModelSettingControl'

type Props = {
  accessToken: string
}

function RoutingSection({
  title,
  children,
}: {
  title: string
  children: ReactNode
}) {
  return (
    <section className="flex flex-col gap-2.5">
      <h3 className="pl-2.5 text-[13px] font-normal text-[var(--c-text-secondary)]">{title}</h3>
      {children}
    </section>
  )
}

function RoutingCard({ children }: { children: ReactNode }) {
  return (
    <div className="overflow-hidden rounded-xl border border-[var(--c-border-subtle)] bg-[var(--c-bg-menu)]">
      {children}
    </div>
  )
}

function RoutingRow({
  title,
  description,
  control,
}: {
  title: string
  description?: ReactNode
  control: ReactNode
}) {
  return (
    <div className="relative grid gap-3 px-5 py-4 sm:grid-cols-[minmax(0,1fr)_minmax(220px,320px)] sm:items-center sm:gap-6 [&+&]:before:absolute [&+&]:before:left-5 [&+&]:before:right-5 [&+&]:before:top-0 [&+&]:before:h-px [&+&]:before:bg-[var(--c-border-subtle)] [&+&]:before:content-['']">
      <div className="min-w-0">
        <div className="text-[13px] font-medium text-[var(--c-text-primary)]">{title}</div>
        {description && (
          <div className="mt-1 text-xs leading-5 text-[var(--c-text-tertiary)]">{description}</div>
        )}
      </div>
      <div className="min-w-0 sm:justify-self-end">{control}</div>
    </div>
  )
}

export function RoutingSettings({ accessToken }: Props) {
  const { t } = useLocale()
  const ds = t.desktopSettings

  return (
    <div className="mx-auto flex w-full max-w-[760px] flex-col gap-6 px-1 pb-8">
      <div>
        <h2 className="text-[24px] font-semibold leading-tight tracking-normal text-[var(--c-text-heading)]">
          {ds.routing}
        </h2>
      </div>

      <RoutingSection title={ds.backgroundToolsSection}>
        <RoutingCard>
          <RoutingRow
            title={ds.chatModel}
            control={(
              <ChatModelSettingControl accessToken={accessToken} />
            )}
          />
        </RoutingCard>
      </RoutingSection>
    </div>
  )
}
