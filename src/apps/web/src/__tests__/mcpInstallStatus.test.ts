import { describe, expect, it } from 'vitest'
import { statusLabel, statusVariant } from '../components/mcp/types'

describe('MCP install discovery status', () => {
  it('将 ready 展示为检查结果而不是运行就绪', () => {
    expect(statusLabel('ready', {
      checked: '检查通过',
      pending: '待检查',
      configured: '已配置',
      failed: '连接失败',
      authError: '认证异常',
      error: '协议异常',
      missing: '缺少依赖',
    })).toBe('检查通过')
    expect(statusVariant('ready')).toBe('neutral')
  })
})
