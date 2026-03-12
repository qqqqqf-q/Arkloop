import { useState, useEffect, useRef, useCallback } from 'react'
import { Loader2, CheckCircle2, XCircle } from 'lucide-react'
import { Modal } from './Modal'
import { bridgeClient, type ModuleAction } from '../api/bridge'

type Props = {
  moduleId: string
  moduleName: string
  action: ModuleAction
  operationId: string
  onClose: () => void
}

type OperationState = 'running' | 'completed' | 'failed'

const ACTION_LABELS: Record<ModuleAction, string> = {
  install: 'Installing',
  start: 'Starting',
  stop: 'Stopping',
  restart: 'Restarting',
  configure: 'Configuring',
  configure_connection: 'Configuring',
  bootstrap_defaults: 'Bootstrapping',
}

export function OperationModal({ moduleName, action, operationId, onClose }: Props) {
  const [logs, setLogs] = useState<string[]>([])
  const [state, setState] = useState<OperationState>('running')
  const [error, setError] = useState<string | undefined>()
  const logEndRef = useRef<HTMLDivElement>(null)
  const cleanupRef = useRef<(() => void) | null>(null)

  useEffect(() => {
    const stop = bridgeClient.streamOperation(
      operationId,
      (line) => setLogs((prev) => [...prev, line]),
      (result) => {
        if (result.status === 'completed') {
          setState('completed')
        } else {
          setState('failed')
          setError(result.error)
        }
      },
    )
    cleanupRef.current = stop
    return () => stop()
  }, [operationId])

  useEffect(() => {
    logEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [logs])

  const handleClose = useCallback(() => {
    if (cleanupRef.current) cleanupRef.current()
    onClose()
  }, [onClose])

  const title = `${ACTION_LABELS[action]} ${moduleName}`

  return (
    <Modal open onClose={handleClose} title={title} width="600px">
      <div className="flex flex-col gap-3">
        <div
          className="overflow-y-auto rounded-md border border-[var(--c-border-console)] bg-[var(--c-bg-page)] p-3"
          style={{ maxHeight: '400px', minHeight: '200px' }}
        >
          {logs.length === 0 && state === 'running' && (
            <span className="text-xs text-[var(--c-text-muted)]">Waiting for output…</span>
          )}
          {logs.map((line, i) => (
            <div key={i} className="font-mono text-xs leading-5 text-[var(--c-text-muted)]">
              {line}
            </div>
          ))}
          <div ref={logEndRef} />
        </div>

        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            {state === 'running' && (
              <>
                <Loader2 size={14} className="animate-spin text-[var(--c-text-muted)]" />
                <span className="text-xs text-[var(--c-text-muted)]">Running…</span>
              </>
            )}
            {state === 'completed' && (
              <>
                <CheckCircle2 size={14} className="text-emerald-500" />
                <span className="text-xs text-emerald-500">Completed</span>
              </>
            )}
            {state === 'failed' && (
              <>
                <XCircle size={14} className="text-red-500" />
                <span className="text-xs text-red-500">{error ?? 'Operation failed'}</span>
              </>
            )}
          </div>

          <button
            onClick={handleClose}
            className="rounded-md bg-[var(--c-bg-tag)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-border)]"
          >
            Close
          </button>
        </div>
      </div>
    </Modal>
  )
}
