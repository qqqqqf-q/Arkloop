import type { CodeExecutionRef } from './storage'

export function codeExecutionAccentColor(status: CodeExecutionRef['status']): string {
  switch (status) {
    case 'failed':
      return '#ef4444'
    case 'success':
      return 'var(--c-border-subtle)'
    case 'completed':
      return 'var(--c-text-muted)'
    case 'running':
    default:
      return 'var(--c-text-secondary)'
  }
}
