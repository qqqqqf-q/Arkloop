import {
  createContext,
  useContext,
  useState,
  useCallback,
  useEffect,
  useRef,
  type ReactNode,
} from 'react'
import { bridgeClient, type ModuleAction } from '../api/bridge'

export type OperationRecord = {
  id: string
  moduleId: string
  moduleName: string
  action: ModuleAction
  status: 'running' | 'completed' | 'failed'
  error?: string
  logs: string[]
  startedAt: number
  finishedAt?: number
}

type OperationContextValue = {
  operations: OperationRecord[]
  activeCount: number
  startOperation: (
    moduleId: string,
    moduleName: string,
    action: ModuleAction,
    operationId: string,
  ) => void
  clearCompleted: () => void
  cancelOperation: (operationId: string) => Promise<void>
  isModuleBusy: (moduleId: string) => boolean
  getModuleOperation: (moduleId: string) => OperationRecord | undefined
  historyOpen: boolean
  setHistoryOpen: (open: boolean) => void
}

const STORAGE_KEY = 'arkloop_operations'

type StoredOperation = Omit<OperationRecord, 'logs'> & { logs: string[] }

function loadStoredOperations(): OperationRecord[] {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return []
    const stored = JSON.parse(raw) as StoredOperation[]
    return stored.map(op =>
      op.status === 'running'
        ? { ...op, status: 'failed' as const, error: 'Lost connection (page refreshed)', finishedAt: Date.now() }
        : op,
    )
  } catch {
    return []
  }
}

function saveOperations(ops: OperationRecord[]) {
  try {
    const toStore = ops.slice(0, 50)
    localStorage.setItem(STORAGE_KEY, JSON.stringify(toStore))
  } catch {
    // ignore quota errors
  }
}

const OperationContext = createContext<OperationContextValue | null>(null)

export function OperationProvider({ children }: { children: ReactNode }) {
  const [operations, setOperations] = useState<OperationRecord[]>(() => loadStoredOperations())
  const [historyOpen, setHistoryOpen] = useState(false)
  const cleanupRefs = useRef<Map<string, () => void>>(new Map())

  const startOperation = useCallback(
    (
      moduleId: string,
      moduleName: string,
      action: ModuleAction,
      operationId: string,
    ) => {
      const record: OperationRecord = {
        id: operationId,
        moduleId,
        moduleName,
        action,
        status: 'running',
        logs: [],
        startedAt: Date.now(),
      }

      setOperations((prev) => [record, ...prev])

      const stop = bridgeClient.streamOperation(
        operationId,
        (line) => {
          setOperations((prev) =>
            prev.map((op) =>
              op.id === operationId
                ? { ...op, logs: [...op.logs, line] }
                : op,
            ),
          )
        },
        (result) => {
          setOperations((prev) =>
            prev.map((op) =>
              op.id === operationId
                ? {
                    ...op,
                    status:
                      result.status === 'completed' ? 'completed' : 'failed',
                    error: result.error,
                    finishedAt: Date.now(),
                  }
                : op,
            ),
          )
          cleanupRefs.current.delete(operationId)
        },
      )

      cleanupRefs.current.set(operationId, stop)
    },
    [],
  )

  useEffect(() => {
    saveOperations(operations)
  }, [operations])

  const clearCompleted = useCallback(() => {
    setOperations((prev) => prev.filter((op) => op.status === 'running'))
  }, [])

  const cancelOperation = useCallback(async (operationId: string) => {
    try {
      await bridgeClient.cancelOperation(operationId)
    } catch {
      // ignore — the SSE stream will eventually report the failure
    }
  }, [])

  const isModuleBusy = useCallback((moduleId: string) => {
    return operations.some(op => op.moduleId === moduleId && op.status === 'running')
  }, [operations])

  const getModuleOperation = useCallback((moduleId: string) => {
    return operations.find(op => op.moduleId === moduleId && op.status === 'running')
  }, [operations])

  const activeCount = operations.filter((op) => op.status === 'running').length

  return (
    <OperationContext.Provider
      value={{ operations, activeCount, startOperation, clearCompleted, cancelOperation, isModuleBusy, getModuleOperation, historyOpen, setHistoryOpen }}
    >
      {children}
    </OperationContext.Provider>
  )
}

export function useOperations(): OperationContextValue {
  const ctx = useContext(OperationContext)
  if (!ctx)
    throw new Error('useOperations must be used within OperationProvider')
  return ctx
}
