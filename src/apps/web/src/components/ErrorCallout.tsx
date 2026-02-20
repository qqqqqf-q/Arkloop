type AppError = {
  message: string
  traceId?: string
  code?: string
}

export function ErrorCallout({ error }: { error: AppError }) {
  return (
    <div className="mt-3 rounded-xl border border-red-900/40 bg-red-950/30 px-4 py-3 text-sm">
      <div className="font-medium text-red-300">{error.message}</div>
      {(error.code || error.traceId) && (
        <div className="mt-1.5 space-y-0.5 font-mono text-xs text-red-400/70">
          {error.code && <div>code: {error.code}</div>}
          {error.traceId && <div>trace_id: {error.traceId}</div>}
        </div>
      )}
    </div>
  )
}

export type { AppError }
