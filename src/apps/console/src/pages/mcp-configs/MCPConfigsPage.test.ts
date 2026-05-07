import { describe, expect, it } from 'vitest'
import { mcpDiscoveryStatusLabel, mcpDiscoveryStatusVariant } from './mcpDiscoveryStatus'

describe('MCP discovery status', () => {
  it('将 ready 展示为检查结果而不是运行就绪', () => {
    expect(mcpDiscoveryStatusLabel('ready', { checked: '检查通过' })).toBe('检查通过')
    expect(mcpDiscoveryStatusVariant('ready')).toBe('neutral')
  })
})
