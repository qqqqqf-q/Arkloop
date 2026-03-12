import {
  createContext,
  useContext,
  useState,
  useCallback,
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
}

const OperationContext = createContext<OperationContextValue | null>(null)

export function OperationProvider({ children }: { children: ReactNode }) {
  const [operations, setOperations] = useState<OperationRecord[]>([])
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

  const clearCompleted = useCallback(() => {
    setOperations((prev) => prev.filter((op) => op.status === 'running'))
  }, [])

  const activeCount = operations.filter((op) => op.status === 'running').length

  return (
    <OperationContext.Provider
      value={{ operations, activeCount, startOperation, clearCompleted }}
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
