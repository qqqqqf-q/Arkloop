import { memo } from 'react'

const BAR_WIDTHS = ['85%', '65%', '90%', '55%', '75%', '60%', '80%', '50%', '70%', '40%']

export const ChatSkeleton = memo(function ChatSkeleton() {
  return (
    <div className="animate-pulse flex flex-col gap-6">
      {/* user prompt placeholder - right aligned */}
      <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
        <div
          style={{
            width: 200,
            height: 40,
            borderRadius: 11,
            background: 'var(--c-text-primary)',
            opacity: 0.08,
          }}
        />
      </div>

      {/* agent output placeholder - left aligned bars with fading opacity */}
      <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
        {BAR_WIDTHS.map((w, i) => (
          <div
            key={i}
            style={{
              width: w,
              height: 12,
              borderRadius: 4,
              background: 'var(--c-text-primary)',
              opacity: 0.15 - i * 0.013,
            }}
          />
        ))}
      </div>
    </div>
  )
})
