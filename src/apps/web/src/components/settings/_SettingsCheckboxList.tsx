export type SettingsCheckboxListOption = {
  value: string
  title: string
  description?: string
  meta?: string
}

type SettingsCheckboxListProps = {
  options: SettingsCheckboxListOption[]
  selectedValues: string[]
  onChange: (values: string[]) => void
  maxHeight?: number
}

export function SettingsCheckboxList({
  options,
  selectedValues,
  onChange,
  maxHeight = 480,
}: SettingsCheckboxListProps) {
  return (
    <div
      className="flex flex-col gap-2 overflow-y-auto p-[2px]"
      style={{
        maxHeight,
      }}
    >
      {options.map((option) => (
        <SettingsCheckboxRow
          key={option.value}
          option={option}
          checked={selectedValues.includes(option.value)}
          onToggle={() => {
            onChange(
              selectedValues.includes(option.value)
                ? selectedValues.filter((value) => value !== option.value)
                : [...selectedValues, option.value],
            )
          }}
        />
      ))}
    </div>
  )
}

type SettingsCheckboxRowProps = {
  option: SettingsCheckboxListOption
  checked: boolean
  onToggle: () => void
}

export function SettingsCheckboxRow({
  option,
  checked,
  onToggle,
}: SettingsCheckboxRowProps) {
  const surface = checked
    ? 'color-mix(in srgb, var(--c-bg-input) 97%, var(--c-text-primary) 3%)'
    : 'var(--c-bg-input)'

  return (
    <button
      type="button"
      role="checkbox"
      aria-checked={checked}
      onClick={onToggle}
      className={[
        'group flex min-h-[60px] w-full items-center gap-3 rounded-xl px-4 text-left transition-[border-color,box-shadow,background-color] duration-180',
        !checked ? 'hover:[box-shadow:0_0_0_0.35px_var(--c-input-border-color-hover)]' : '',
      ].filter(Boolean).join(' ')}
      style={{
        border: `1.5px solid ${checked ? 'var(--c-accent-send)' : 'var(--c-input-border-color)'}`,
        background: surface,
      }}
    >
      <span
        className="shrink-0 self-center rounded-full transition-[border-width,border-color] duration-150"
        style={{
          width: 14,
          height: 14,
          border: checked
            ? '4px solid var(--c-accent-send)'
            : '1.5px solid var(--c-border-mid)',
        }}
      />
      <span className="min-w-0 flex-1">
        <span className="block truncate text-[13px] font-medium leading-tight text-[var(--c-text-primary)]">
          {option.title}
        </span>
        {option.description && (
          <span className="mt-0.5 block truncate text-[11px] leading-tight text-[var(--c-text-muted)]">
            {option.description}
          </span>
        )}
      </span>
      {option.meta && (
        <span className="shrink-0 rounded-md bg-[var(--c-bg-deep)] px-1.5 py-0.5 text-[10px] font-medium leading-tight text-[var(--c-text-muted)]">
          {option.meta}
        </span>
      )}
    </button>
  )
}
