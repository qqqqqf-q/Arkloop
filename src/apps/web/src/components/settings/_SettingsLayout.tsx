import type { KeyboardEvent, ReactNode } from 'react'
import { SettingsSwitch } from './_SettingsSwitch'

/** 与 ProvidersSettings 列表区一致：一行两列、窄屏单列 */
export const SETTINGS_TWO_COLUMN_GRID_CLASS = 'grid gap-3 sm:grid-cols-2'

/** 与 PluginsSettings PluginListRow 单项外壳一致 */
export const SETTINGS_STANDALONE_LIST_CARD_CLASS =
  'overflow-hidden rounded-xl border border-[var(--c-border-subtle)] bg-[var(--c-bg-menu)] transition-colors duration-[140ms] hover:bg-[var(--c-bg-deep)]'

export function SettingsPage({
  title,
  description,
  children,
  className = 'max-w-[760px]',
}: {
  title: string
  description?: ReactNode
  children: ReactNode
  className?: string
}) {
  return (
    <div className={`mx-auto flex w-full flex-col gap-6 px-1 pb-8 ${className}`}>
      <header className="flex flex-col gap-1">
        <h2 className="text-[24px] font-semibold leading-tight tracking-normal text-[var(--c-text-heading)]">
          {title}
        </h2>
        {description ? (
          <p className="max-w-[560px] text-[13px] leading-5 text-[var(--c-text-secondary)]">{description}</p>
        ) : null}
      </header>
      {children}
    </div>
  )
}

export function SettingsGroup({
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

/** 描边、圆角、背景（不裁切，供卡片内浮层使用） */
export const SETTINGS_CARD_SURFACE_BASE =
  'rounded-xl border border-[var(--c-border-subtle)] bg-[var(--c-bg-menu)]'

/** 与 General / SettingsCard 白卡片一致；默认裁切以贴合圆角 */
export const SETTINGS_CARD_SURFACE_CLASS = `${SETTINGS_CARD_SURFACE_BASE} overflow-hidden`

/** 技能卡等：保留圆角描边，但不裁切内部下拉，避免 ⋯ 菜单被切掉 */
export const SETTINGS_CARD_SURFACE_OVERFLOW_VISIBLE_CLASS = `${SETTINGS_CARD_SURFACE_BASE} overflow-visible`

/** SettingsRow 同类分割线：不贴卡片左右边 */
export const SETTINGS_CARD_INSET_RULE_CLASS = 'mx-5 h-px shrink-0 bg-[var(--c-border-subtle)]'

export function SettingsCard({ children }: { children: ReactNode }) {
  return <div className={SETTINGS_CARD_SURFACE_CLASS}>{children}</div>
}

export function SettingsRow({
  title,
  description,
  control,
  disabled,
  onClick,
  children,
}: {
  title: string
  description?: ReactNode
  control?: ReactNode
  disabled?: boolean
  onClick?: () => void
  children?: ReactNode
}) {
  const interactive = onClick !== undefined
  const hasControl = control !== undefined

  const handleKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (!interactive || disabled) return
    if (event.key !== 'Enter' && event.key !== ' ') return
    event.preventDefault()
    onClick()
  }

  return (
    <div
      role={interactive ? 'button' : undefined}
      tabIndex={interactive && !disabled ? 0 : undefined}
      onClick={disabled ? undefined : onClick}
      onKeyDown={handleKeyDown}
      className={[
        'group/settings-row relative grid items-center gap-3 px-5 py-4 outline-none transition-colors duration-[160ms] sm:gap-6 [&+&]:before:absolute [&+&]:before:left-5 [&+&]:before:right-5 [&+&]:before:top-0 [&+&]:before:h-px [&+&]:before:bg-[var(--c-border-subtle)] [&+&]:before:content-[\'\']',
        hasControl ? 'sm:grid-cols-[minmax(0,1fr)_auto]' : '',
        interactive && !disabled ? 'cursor-pointer hover:bg-[var(--c-bg-deep)]/25 focus-visible:ring-2 focus-visible:ring-[var(--c-accent)]' : '',
        disabled ? 'pointer-events-none opacity-40' : '',
      ].filter(Boolean).join(' ')}
    >
      <div className="min-w-0">
        <div className="text-[13px] font-medium text-[var(--c-text-primary)]">{title}</div>
        {description && (
          <div className="mt-1 text-xs leading-5 text-[var(--c-text-tertiary)]">{description}</div>
        )}
        {children}
      </div>
      {hasControl && (
        <div className="flex min-w-0 items-center sm:justify-self-end" onClick={(event) => event.stopPropagation()}>
          {control}
        </div>
      )}
    </div>
  )
}

export function SettingsSwitchRow({
  title,
  description,
  checked,
  onChange,
  disabled,
  forceHover,
}: {
  title: string
  description?: ReactNode
  checked: boolean
  onChange: (next: boolean) => void
  disabled?: boolean
  forceHover?: boolean
}) {
  const handleChange = (next: boolean) => {
    if (disabled) return
    onChange(next)
  }

  return (
    <SettingsRow
      title={title}
      description={description}
      disabled={disabled}
      onClick={() => handleChange(!checked)}
      control={<SettingsSwitch checked={checked} onChange={handleChange} disabled={disabled} forceHover={forceHover} />}
    />
  )
}
