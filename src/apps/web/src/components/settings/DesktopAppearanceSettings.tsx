import { useState } from 'react'
import type { ReactNode } from 'react'
import { DesktopFontSettings } from './FontSettings'
import { ThemePresetPicker } from './ThemePresetPicker'
import { ThemeColorEditor } from './ThemeColorEditor'
import { ThemeModePicker, SidebarGroupingPicker } from './AppearanceSettings'
import { useLocale } from '../../contexts/LocaleContext'

function DesktopAppearanceSection({
  title,
  children,
  contentClassName,
}: {
  title: string
  children: ReactNode
  contentClassName?: string
}) {
  return (
    <section className="flex flex-col gap-2.5">
      <h3 className="text-[13px] font-normal text-[var(--c-text-secondary)]">{title}</h3>
      <div className={contentClassName}>
        {children}
      </div>
    </section>
  )
}

export function DesktopAppearanceSettings() {
  const { t } = useLocale()
  const [showColorEditor, setShowColorEditor] = useState(false)

  return (
    <div className="mx-auto flex w-full max-w-[760px] flex-col gap-6 px-1 pb-8">
      <div>
        <h2 className="text-[24px] font-semibold leading-tight tracking-normal text-[var(--c-text-heading)]">
          {t.appearance}
        </h2>
      </div>

      <DesktopAppearanceSection title={t.themeModeSection} contentClassName="max-w-[560px]">
        <ThemeModePicker showLabel={false} />
      </DesktopAppearanceSection>

      <DesktopAppearanceSection title={t.themePresetSection} contentClassName="max-w-[560px]">
        <ThemePresetPicker showTitle={false} onEditColors={() => setShowColorEditor(v => !v)} />
        {showColorEditor && (
          <ThemeColorEditor onClose={() => setShowColorEditor(false)} />
        )}
      </DesktopAppearanceSection>

      <DesktopAppearanceSection title={t.sidebarGrouping} contentClassName="max-w-[560px]">
        <SidebarGroupingPicker showLabel={false} />
      </DesktopAppearanceSection>

      <DesktopAppearanceSection title={t.fontSection}>
        <DesktopFontSettings />
      </DesktopAppearanceSection>
    </div>
  )
}
