export type MCPDiscoveryStatusVariant = 'success' | 'warning' | 'error' | 'neutral'

export type MCPDiscoveryStatusLabels = {
  checked: string
}

export function mcpDiscoveryStatusVariant(status: string): MCPDiscoveryStatusVariant {
  switch (status) {
    case 'ready':
      return 'neutral'
    case 'needs_check':
    case 'configured':
      return 'warning'
    case 'install_missing':
    case 'auth_invalid':
    case 'connect_failed':
    case 'discovered_empty':
    case 'protocol_error':
      return 'error'
    default:
      return 'neutral'
  }
}

export function mcpDiscoveryStatusLabel(status: string, labels: MCPDiscoveryStatusLabels): string {
  switch (status) {
    case 'ready':
      return labels.checked
    default:
      return status
  }
}
