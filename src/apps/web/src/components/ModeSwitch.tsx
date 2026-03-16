import { motion } from 'framer-motion'

export type AppMode = 'chat' | 'claw'

type Props = {
  mode: AppMode
  onChange: (mode: AppMode) => void
  labels: { chat: string; claw: string }
  availableModes?: AppMode[]
}

const OPTIONS: AppMode[] = ['chat', 'claw']

export function ModeSwitch({ mode, onChange, labels, availableModes = OPTIONS }: Props) {
  const labelMap: Record<AppMode, string> = { chat: labels.chat, claw: labels.claw }
  const options = OPTIONS.filter((opt) => availableModes.includes(opt))

  return (
    <div
      className="relative flex items-center rounded-lg p-[3px]"
      style={{
        background: 'var(--c-mode-switch-track)',
        border: '0.5px solid var(--c-mode-switch-border)',
      }}
    >
      {options.map((opt) => {
        const active = mode === opt
        return (
          <button
            key={opt}
            type="button"
            onClick={() => onChange(opt)}
            className="relative z-10 flex items-center justify-center rounded-md px-3 py-[3px] text-[12px] leading-[18px] transition-colors duration-200"
            style={{
              color: active
                ? 'var(--c-mode-switch-active-text)'
                : 'var(--c-mode-switch-inactive-text)',
              fontWeight: 350,
              minWidth: '52px',
            }}
          >
            {active && (
              <motion.span
                layoutId="mode-switch-pill"
                className="absolute inset-0 rounded-md"
                style={{
                  background: 'var(--c-mode-switch-pill)',
                }}
                transition={{
                  type: 'spring',
                  stiffness: 500,
                  damping: 35,
                  mass: 0.8,
                }}
              />
            )}
            <span className="relative">{labelMap[opt]}</span>
          </button>
        )
      })}
    </div>
  )
}
