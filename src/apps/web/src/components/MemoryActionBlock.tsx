import type { CSSProperties } from 'react'
import { Brain, Check, Loader2, Pencil, Search, Eye, Trash2, X } from 'lucide-react'
import { motion, AnimatePresence } from 'framer-motion'
import type { MemoryActionRef } from '../storage'

type Props = {
  actions: MemoryActionRef[]
  live?: boolean
}

function getToolLabel(toolName: MemoryActionRef['toolName']): string {
  switch (toolName) {
    case 'memory_write': return '写入记忆'
    case 'memory_search': return '搜索记忆'
    case 'memory_read': return '读取记忆'
    case 'memory_forget': return '删除记忆'
    case 'notebook_write': return '写入笔记'
    case 'notebook_read': return '读取笔记'
    case 'notebook_edit': return '编辑笔记'
    case 'notebook_forget': return '删除笔记'
  }
}

function MemoryToolGlyph({
  toolName,
  size,
  style,
}: {
  toolName: MemoryActionRef['toolName']
  size: number
  style: CSSProperties
}) {
  switch (toolName) {
    case 'memory_write':
      return <Pencil size={size} style={style} />
    case 'memory_search':
      return <Search size={size} style={style} />
    case 'memory_read':
      return <Eye size={size} style={style} />
    case 'memory_forget':
    case 'notebook_forget':
      return <Trash2 size={size} style={style} />
    case 'notebook_edit':
      return <Pencil size={size} style={style} />
  }
}

function getArgSummary(action: MemoryActionRef): string {
  const { toolName, args } = action
  if (toolName === 'memory_write' || toolName === 'notebook_write' || toolName === 'notebook_edit') {
    const parts: string[] = []
    if (args.category) parts.push(args.category)
    if (args.key) parts.push(args.key)
    return parts.join('/') || ''
  }
  if (toolName === 'memory_search') {
    return args.query ? `"${args.query}"` : ''
  }
  if (toolName === 'memory_read' || toolName === 'memory_forget' || toolName === 'notebook_read' || toolName === 'notebook_forget') {
    if (args.uri) {
      const id = args.uri.replace('local://memory/', '')
      return id.length > 8 ? id.slice(0, 8) + '…' : id
    }
    return ''
  }
  return ''
}

function MemoryActionRow({ action, live }: { action: MemoryActionRef; live?: boolean }) {
  const label = getToolLabel(action.toolName)
  const argSummary = getArgSummary(action)
  const isActive = action.status === 'active'
  const isError = action.status === 'error'

  return (
    <motion.div
      initial={{ opacity: 0, y: 4 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.2, ease: 'easeOut' }}
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: '6px',
        padding: '3px 0',
        fontSize: '12px',
        color: isError ? 'var(--c-status-error-text, #ef4444)' : 'var(--c-text-secondary)',
      }}
    >
      <MemoryToolGlyph toolName={action.toolName} size={11} style={{ flexShrink: 0, opacity: 0.7 }} />
      <span style={{ fontWeight: 500, flexShrink: 0 }}>{label}</span>
      {argSummary && (
        <span
          style={{
            color: 'var(--c-text-muted)',
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
            maxWidth: '200px',
          }}
        >
          {argSummary}
        </span>
      )}
      {action.resultSummary && action.status === 'done' && (
        <span style={{ color: 'var(--c-text-muted)', flexShrink: 0 }}>· {action.resultSummary}</span>
      )}
      <span style={{ marginLeft: 'auto', flexShrink: 0 }}>
        {isActive && live ? (
          <Loader2 size={11} style={{ animation: 'spin 1s linear infinite' }} />
        ) : isError ? (
          <X size={11} />
        ) : (
          <Check size={11} style={{ color: 'var(--c-status-success-text, #22c55e)', opacity: 0.8 }} />
        )}
      </span>
    </motion.div>
  )
}

export function MemoryActionBlock({ actions, live }: Props) {
  if (actions.length === 0) return null

  return (
    <motion.div
      initial={{ opacity: 0, y: 6 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.25, ease: 'easeOut' }}
      style={{
        marginBottom: '10px',
        padding: '8px 10px',
        borderRadius: '8px',
        background: 'var(--c-bg-elevated, var(--c-bg-menu))',
        border: '0.5px solid var(--c-border-subtle)',
        maxWidth: '480px',
      }}
    >
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: '5px',
          marginBottom: actions.length > 0 ? '4px' : 0,
          fontSize: '11px',
          fontWeight: 600,
          color: 'var(--c-text-muted)',
          textTransform: 'uppercase',
          letterSpacing: '0.04em',
        }}
      >
        <Brain size={11} />
        记忆操作
      </div>
      <AnimatePresence initial={false}>
        {actions.map((action) => (
          <MemoryActionRow key={action.id} action={action} live={live} />
        ))}
      </AnimatePresence>
    </motion.div>
  )
}
