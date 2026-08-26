import { getDesktopAccessToken, getDesktopBridgeBaseUrl } from '@arkloop/shared/desktop'

export type ModuleStatus =
  | 'not_installed'
  | 'installed_disconnected'
  | 'pending_bootstrap'
  | 'running'
  | 'stopped'
  | 'error'

export type ModuleCapabilities = {
  installable: boolean
  configurable: boolean
  healthcheck: boolean
  bootstrap_supported: boolean
  external_admin_supported: boolean
  privileged_required: boolean
}

export type ModuleInfo = {
  id: string
  name: string
  description: string
  category: 'memory' | 'search' | 'infrastructure'
  status: ModuleStatus
  version?: string
  port?: number
  web_url?: string
  capabilities: ModuleCapabilities
  depends_on: string[]
  mutually_exclusive: string[]
}

export type ModuleAction =
  | 'install'
  | 'start'
  | 'stop'
  | 'restart'
  | 'configure'
  | 'configure_connection'
  | 'bootstrap_defaults'

export type BridgeHealth = {
  status: 'ok' | 'error'
  version?: string
}

class BridgeClient {
  private readonly getBaseUrl: () => string

  constructor(getBaseUrl: () => string) {
    this.getBaseUrl = getBaseUrl
  }

  private baseUrl(): string {
    return this.getBaseUrl()
  }

  private authHeaders(): Record<string, string> | undefined {
    const token = getDesktopAccessToken()
    return token ? { Authorization: `Bearer ${token}` } : undefined
  }

  private jsonHeaders(): Record<string, string> {
    return {
      'Content-Type': 'application/json',
      ...(this.authHeaders() ?? {}),
    }
  }

  async healthz(): Promise<BridgeHealth> {
    const resp = await fetch(`${this.baseUrl()}/healthz`, {
      signal: AbortSignal.timeout(3000),
    })
    if (!resp.ok) throw new Error(`Bridge health check failed: ${resp.status}`)
    return await resp.json()
  }

  async listModules(): Promise<ModuleInfo[]> {
    const resp = await fetch(`${this.baseUrl()}/v1/modules`, {
      headers: this.authHeaders(),
      signal: AbortSignal.timeout(5000),
    })
    if (!resp.ok) throw new Error(`List modules failed: ${resp.status}`)
    return await resp.json()
  }

  async performAction(
    moduleId: string,
    action: ModuleAction,
    params?: Record<string, unknown>,
  ): Promise<{ operation_id: string }> {
    const resp = await fetch(
      `${this.baseUrl()}/v1/modules/${encodeURIComponent(moduleId)}/actions`,
      {
        method: 'POST',
        headers: this.jsonHeaders(),
        body: JSON.stringify({ action, params }),
        signal: AbortSignal.timeout(10000),
      },
    )
    if (!resp.ok) throw new Error(`Module action failed: ${resp.status}`)
    return await resp.json()
  }

  streamOperation(
    operationId: string,
    onLog: (line: string) => void,
    onDone: (result: { status: string; error?: string }) => void,
  ): () => void {
    let finished = false
    const controller = new AbortController()
    const finish = (result: { status: string; error?: string }) => {
      if (finished) return
      finished = true
      onDone(result)
    }

    void this.readOperationStream(operationId, controller.signal, onLog, finish)
      .catch((error: unknown) => {
        if (finished || controller.signal.aborted) return
        finished = true
        onDone({
          status: 'failed',
          error: error instanceof Error ? error.message : 'Connection lost',
        })
      })

    return () => {
      finished = true
      controller.abort()
    }
  }

  private async readOperationStream(
    operationId: string,
    signal: AbortSignal,
    onLog: (line: string) => void,
    onDone: (result: { status: string; error?: string }) => void,
  ): Promise<void> {
    const resp = await fetch(
      `${this.baseUrl()}/v1/operations/${encodeURIComponent(operationId)}/stream`,
      {
        headers: this.authHeaders(),
        signal,
      },
    )
    if (!resp.ok) throw new Error(`Operation stream failed: ${resp.status}`)
    if (!resp.body) throw new Error('Operation stream missing body')

    const reader = resp.body.getReader()
    const decoder = new TextDecoder()
    let buffer = ''
    let streamDone = false
    const finish = (result: { status: string; error?: string }) => {
      streamDone = true
      onDone(result)
    }

    try {
      while (true) {
        const { done, value } = await reader.read()
        if (done) break
        buffer += decoder.decode(value, { stream: true })
        buffer = this.consumeOperationFrames(buffer, onLog, finish)
        if (signal.aborted || streamDone) return
      }

      buffer += decoder.decode()
      this.consumeOperationFrames(`${buffer}\n\n`, onLog, finish)
      if (!signal.aborted && !streamDone) {
        throw new Error('Operation stream ended before status')
      }
    } finally {
      reader.releaseLock()
    }
  }

  private consumeOperationFrames(
    buffer: string,
    onLog: (line: string) => void,
    onDone: (result: { status: string; error?: string }) => void,
  ): string {
    const parts = buffer.replace(/\r\n/g, '\n').split('\n\n')
    const rest = parts.pop() ?? ''
    for (const part of parts) {
      const eventLine = part.split('\n').find((line) => line.startsWith('event:'))
      const dataLine = part.split('\n').find((line) => line.startsWith('data:'))
      const event = eventLine?.slice('event:'.length).trim()
      const data = dataLine?.slice('data:'.length).trimStart() ?? ''

      if (event === 'log') {
        onLog(data)
      } else if (event === 'status') {
        onDone(JSON.parse(data) as { status: string; error?: string })
      }
    }
    return rest
  }

  waitForOperation(operationId: string): Promise<void> {
    return new Promise((resolve, reject) => {
      let stop = () => {}
      stop = this.streamOperation(
        operationId,
        () => {},
        (result) => {
          stop()
          if (result.status === 'completed') {
            resolve()
            return
          }
          reject(new Error(result.error || `Operation ${result.status}`))
        },
      )
    })
  }
}

function resolveBridgeBaseUrl(): string {
  return normalizeBridgeBaseUrl(getDesktopBridgeBaseUrl())
    ?? normalizeBridgeBaseUrl(import.meta.env.VITE_BRIDGE_URL)
    ?? 'http://localhost:19003'
}

function normalizeBridgeBaseUrl(value: string | null | undefined): string | null {
  const trimmed = value?.trim()
  if (!trimmed) return null
  return trimmed.replace(/\/+$/, '')
}

export const bridgeClient = new BridgeClient(resolveBridgeBaseUrl)

export async function checkBridgeAvailable(): Promise<boolean> {
  try {
    await bridgeClient.healthz()
    return true
  } catch {
    return false
  }
}
