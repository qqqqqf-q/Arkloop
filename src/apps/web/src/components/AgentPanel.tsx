import { X, Bot } from 'lucide-react'
import type { SubAgentRef } from '../storage'
import { AssistantThinkingMarkdown } from './cop-timeline'

const MONO = 'ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace'

type Props = {
  agent: SubAgentRef
  onClose: () => void
}

function agentTitle(agent: SubAgentRef): string {
  return agent.nickname || agent.personaId || agent.role || 'Agent'
}

function statusLabel(status: SubAgentRef['status']): string {
  switch (status) {
    case 'spawning': return 'Spawning'
    case 'active': return 'Running'
    case 'completed': return 'Completed'
    case 'failed': return 'Failed'
    case 'closed': return 'Closed'
  }
}

export function AgentPanel({ agent, onClose }: Props) {
  const title = agentTitle(agent)
  const prompt = agent.input?.trim()
  const output = agent.output?.trim() || agent.error?.trim()

  return (
    <div style={{ width: '420px', display: 'flex', flexDirection: 'column', height: '100%', background: 'var(--c-bg-page)' }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '12px 16px', flexShrink: 0, borderBottom: '0.5px solid var(--c-border-subtle)' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 10, minWidth: 0 }}>
          <Bot size={16} color="var(--c-text-tertiary)" strokeWidth={2} />
          <div style={{ display: 'flex', flexDirection: 'column', gap: 1, minWidth: 0 }}>
            <span style={{ fontSize: 13, fontWeight: 500, color: 'var(--c-text-secondary)', lineHeight: '16px', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
              {title}
            </span>
            <span style={{ fontSize: 11, color: agent.status === 'failed' ? 'var(--c-status-error-text, #ef4444)' : 'var(--c-text-muted)', lineHeight: '14px' }}>
              {statusLabel(agent.status)}
            </span>
          </div>
        </div>
        <button
          onClick={onClose}
          className="hover:bg-[var(--c-bg-deep)]"
          style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', width: 28, height: 28, borderRadius: 8, border: 'none', color: 'var(--c-text-secondary)', cursor: 'pointer' }}
        >
          <X size={18} />
        </button>
      </div>

      <div style={{ flex: 1, overflowY: 'auto', padding: '16px', display: 'flex', flexDirection: 'column', gap: 16 }}>
        {prompt && (
          <div style={{ alignSelf: 'flex-end', maxWidth: '92%', borderRadius: 14, background: 'var(--c-bg-user-message, #eaf2ff)', color: 'var(--c-text-primary)', padding: '10px 12px', fontSize: 13, lineHeight: '20px', whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}>
            {prompt}
          </div>
        )}

        {output ? (
          <div style={{ fontSize: 14, lineHeight: '22px', color: agent.status === 'failed' ? 'var(--c-status-error-text, #ef4444)' : 'var(--c-text-primary)' }}>
            <AssistantThinkingMarkdown markdown={output} live={false} />
          </div>
        ) : (
          <div style={{ borderRadius: 12, background: 'var(--c-bg-menu)', padding: '14px 12px', color: 'var(--c-text-muted)', fontFamily: MONO, fontSize: 12 }}>
            {agent.status === 'active' || agent.status === 'spawning' ? 'Waiting for agent output…' : 'No output'}
          </div>
        )}
      </div>
    </div>
  )
}
