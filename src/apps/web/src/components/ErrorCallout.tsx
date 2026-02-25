type AppError = {
  message: string
  traceId?: string
  code?: string
}

export function ErrorCallout({ error }: { error: AppError }) {
  return (
    <div
      className="mt-3 rounded-xl border px-4 py-3 text-sm"
      style={{
        background: 'var(--c-error-bg)',
        borderColor: 'var(--c-error-border)',
      }}
    >
      <div className="font-medium" style={{ color: 'var(--c-error-text)' }}>
        {error.message}
      </div>
      {(error.code || error.traceId) && (
        <div
          className="mt-1.5 space-y-0.5 font-mono text-xs"
          style={{ color: 'var(--c-error-subtext)' }}
        >
          {error.code && <div>code: {error.code}</div>}
          {error.traceId && <div>trace_id: {error.traceId}</div>}
        </div>
      )}
    </div>
  )
}

export type { AppError }
