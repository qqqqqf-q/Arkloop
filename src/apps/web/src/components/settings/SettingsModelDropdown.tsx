import { SettingsSelect } from './_SettingsSelect'

export type SettingsModelOption = { value: string; label: string }

export function SettingsModelDropdown({
  value,
  options,
  placeholder,
  disabled,
  onChange,
  showEmpty = true,
}: {
  value: string
  options: SettingsModelOption[]
  placeholder: string
  disabled: boolean
  onChange: (v: string) => void
  showEmpty?: boolean
}) {
  const selectOptions = showEmpty
    ? [{ value: '', label: placeholder }, ...options]
    : options

  return (
    <SettingsSelect
      value={value}
      options={selectOptions}
      placeholder={placeholder}
      disabled={disabled}
      onChange={onChange}
      triggerClassName="h-9"
    />
  )
}
