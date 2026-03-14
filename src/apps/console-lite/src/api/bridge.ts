// Bridge API client - proxied through Vite dev server to avoid CORS.

const BRIDGE_BASE_URL = import.meta.env.VITE_BRIDGE_URL ?? '/bridge'

export type ModuleStatus =
  | 'not_installed'
  | 'installed_disconnected'
  | 'pending_bootstrap'
  | 'running'
  | 'stopped'
  | 'error'

export type ModuleCategory = 'memory' | 'sandbox' | 'search' | 'browser' | 'console' | 'infrastructure' | 'security'

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
  category: ModuleCategory
  status: ModuleStatus
  version?: string
  port?: number
  web_url?: string
  capabilities: ModuleCapabilities
  depends_on: string[]
  mutually_exclusive: string[]
}

export type ModuleAction = 'install' | 'start' | 'stop' | 'restart' | 'configure' | 'configure_connection' | 'bootstrap_defaults'

export type PlatformInfo = {
  os: string
  docker_available: boolean
  kvm_available: boolean
}

export type BridgeHealth = {
  status: 'ok' | 'error'
  version?: string
}

export type SystemVersionInfo = {
  version: string
  compose_dir: string
}

export type UpgradeRequest = {
  mode: 'prod' | 'dev'
  target_version?: string
}

class BridgeClient {
  private baseUrl: string

  constructor(baseUrl: string) {
    this.baseUrl = baseUrl
  }

  async healthz(): Promise<BridgeHealth> {
    const resp = await fetch(`${this.baseUrl}/healthz`, {
      signal: AbortSignal.timeout(3000),
    })
    if (!resp.ok) throw new Error(`Bridge health check failed: ${resp.status}`)
    return resp.json()
  }

  async detectPlatform(): Promise<PlatformInfo> {
    const resp = await fetch(`${this.baseUrl}/v1/platform/detect`, {
      signal: AbortSignal.timeout(5000),
    })
    if (!resp.ok) throw new Error(`Platform detect failed: ${resp.status}`)
    return resp.json()
  }

  async listModules(): Promise<ModuleInfo[]> {
    const resp = await fetch(`${this.baseUrl}/v1/modules`, {
      signal: AbortSignal.timeout(5000),
    })
    if (!resp.ok) throw new Error(`List modules failed: ${resp.status}`)
    return resp.json()
  }

  async getModule(id: string): Promise<ModuleInfo> {
    const resp = await fetch(`${this.baseUrl}/v1/modules/${encodeURIComponent(id)}`, {
      signal: AbortSignal.timeout(5000),
    })
    if (!resp.ok) throw new Error(`Get module failed: ${resp.status}`)
    return resp.json()
  }

  async performAction(moduleId: string, action: ModuleAction, params?: Record<string, string>): Promise<{ operation_id: string }> {
    const resp = await fetch(`${this.baseUrl}/v1/modules/${encodeURIComponent(moduleId)}/actions`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ action, params }),
      signal: AbortSignal.timeout(10000),
    })
    if (!resp.ok) throw new Error(`Module action failed: ${resp.status}`)
    return resp.json()
  }

  async cancelOperation(operationId: string): Promise<void> {
    const resp = await fetch(`${this.baseUrl}/v1/operations/${encodeURIComponent(operationId)}/cancel`, {
      method: 'POST',
      signal: AbortSignal.timeout(5000),
    })
    if (!resp.ok) throw new Error(`Cancel operation failed: ${resp.status}`)
  }

  async systemVersion(): Promise<SystemVersionInfo> {
    const resp = await fetch(`${this.baseUrl}/v1/system/version`, {
      signal: AbortSignal.timeout(5000),
    })
    if (!resp.ok) throw new Error(`System version failed: ${resp.status}`)
    return resp.json()
  }

  async systemUpgrade(req: UpgradeRequest): Promise<{ operation_id: string }> {
    const resp = await fetch(`${this.baseUrl}/v1/system/upgrade`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(req),
      signal: AbortSignal.timeout(10000),
    })
    if (!resp.ok) throw new Error(`System upgrade failed: ${resp.status}`)
    return resp.json()
  }

  streamOperation(
    operationId: string,
    onLog: (line: string) => void,
    onDone: (result: { status: string; error?: string }) => void,
  ): () => void {
    const es = new EventSource(
      `${this.baseUrl}/v1/operations/${encodeURIComponent(operationId)}/stream`,
    )
    es.addEventListener('log', (e: MessageEvent) => onLog(e.data as string))
    es.addEventListener('status', (e: MessageEvent) => {
      const result = JSON.parse(e.data as string) as { status: string; error?: string }
      onDone(result)
      es.close()
    })
    es.onerror = () => {
      onDone({ status: 'failed', error: 'Connection lost' })
      es.close()
    }
    return () => es.close()
  }
}

export const bridgeClient = new BridgeClient(BRIDGE_BASE_URL)

export async function checkBridgeAvailable(): Promise<boolean> {
  try {
    await bridgeClient.healthz()
    return true
  } catch {
    return false
  }
}
