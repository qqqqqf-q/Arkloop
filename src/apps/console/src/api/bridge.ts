// Bridge API client for full console - identical contract to console-lite.
// Connects to Installer Bridge service on localhost:8003.

const BRIDGE_BASE_URL = import.meta.env.VITE_BRIDGE_URL ?? 'http://localhost:8003'

export type ModuleStatus =
  | 'not_installed'
  | 'installed_disconnected'
  | 'pending_bootstrap'
  | 'running'
  | 'stopped'
  | 'error'

export type ModuleCategory = 'memory' | 'sandbox' | 'search' | 'browser' | 'console' | 'infrastructure'

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
  capabilities: ModuleCapabilities
  depends_on: string[]
  mutually_exclusive: string[]
}

export type ModuleAction = 'install' | 'start' | 'stop' | 'restart' | 'configure_connection' | 'bootstrap_defaults'

class BridgeClient {
  private baseUrl: string

  constructor(baseUrl: string) {
    this.baseUrl = baseUrl
  }

  async healthz(): Promise<{ status: string }> {
    const resp = await fetch(`${this.baseUrl}/healthz`, {
      signal: AbortSignal.timeout(3000),
    })
    if (!resp.ok) throw new Error(`Bridge health check failed: ${resp.status}`)
    return resp.json()
  }

  async listModules(): Promise<ModuleInfo[]> {
    const resp = await fetch(`${this.baseUrl}/v1/modules`, {
      signal: AbortSignal.timeout(5000),
    })
    if (!resp.ok) throw new Error(`List modules failed: ${resp.status}`)
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
