import { useState } from 'react'
import { FontSettings } from './FontSettings'
import { ThemePresetPicker } from './ThemePresetPicker'
import { ThemeColorEditor } from './ThemeColorEditor'
import { ThemeModePicker } from './AppearanceSettings'

export function DesktopAppearanceSettings() {
  const [showColorEditor, setShowColorEditor] = useState(false)

  return (
    <div className="flex flex-col gap-6">
      <ThemeModePicker />
      <ThemePresetPicker onEditColors={() => setShowColorEditor(v => !v)} />
      {showColorEditor && (
        <ThemeColorEditor onClose={() => setShowColorEditor(false)} />
      )}
      <FontSettings />
    </div>
  )
}
