import { getDesktopBridgeBaseUrl } from '@arkloop/shared/desktop'

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
  category: 'memory' | 'sandbox' | 'search' | 'browser' | 'console' | 'infrastructure'
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

  async healthz(): Promise<BridgeHealth> {
    const resp = await fetch(`${this.baseUrl()}/healthz`, {
      signal: AbortSignal.timeout(3000),
    })
    if (!resp.ok) throw new Error(`Bridge health check failed: ${resp.status}`)
    return await resp.json()
  }

  async listModules(): Promise<ModuleInfo[]> {
    const resp = await fetch(`${this.baseUrl()}/v1/modules`, {
      signal: AbortSignal.timeout(5000),
    })
    if (!resp.ok) throw new Error(`List modules failed: ${resp.status}`)
    return await resp.json()
  }

  async performAction(
    moduleId: string,
    action: ModuleAction,
    params?: Record<string, string>,
  ): Promise<{ operation_id: string }> {
    const resp = await fetch(
      `${this.baseUrl()}/v1/modules/${encodeURIComponent(moduleId)}/actions`,
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
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
    let retries = 0
    const MAX_RETRIES = 3

    const es = new EventSource(
      `${this.baseUrl()}/v1/operations/${encodeURIComponent(operationId)}/stream`,
    )
    es.addEventListener('log', (event: MessageEvent) => {
      retries = 0
      onLog(event.data as string)
    })
    es.addEventListener('status', (event: MessageEvent) => {
      if (finished) return
      finished = true
      es.close()
      onDone(JSON.parse(event.data as string) as { status: string; error?: string })
    })
    es.onerror = () => {
      if (finished) return
      retries++
      if (retries > MAX_RETRIES) {
        finished = true
        es.close()
        onDone({ status: 'failed', error: 'Connection lost' })
      }
    }
    return () => {
      finished = true
      es.close()
    }
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

  async getExecutionMode(): Promise<'local' | 'vm'> {
    const resp = await fetch(`${this.baseUrl()}/v1/execution-mode`, {
      signal: AbortSignal.timeout(3000),
    })
    if (!resp.ok) throw new Error(`Get execution mode failed: ${resp.status}`)
    const data = (await resp.json()) as { mode: string }
    return data.mode as 'local' | 'vm'
  }

  async setExecutionMode(mode: 'local' | 'vm'): Promise<void> {
    const resp = await fetch(`${this.baseUrl()}/v1/execution-mode`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ mode }),
      signal: AbortSignal.timeout(3000),
    })
    if (!resp.ok) throw new Error(`Set execution mode failed: ${resp.status}`)
  }
}

function resolveBridgeBaseUrl(): string {
  return getDesktopBridgeBaseUrl()
    ?? import.meta.env.VITE_BRIDGE_URL
    ?? 'http://localhost:19003'
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
