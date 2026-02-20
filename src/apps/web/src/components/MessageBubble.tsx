import { Copy, RefreshCw, Share2, Split } from 'lucide-react'
import type { MessageResponse } from '../api'

type Props = {
  message: MessageResponse
}

export function MessageBubble({ message }: Props) {
  if (message.role === 'user') {
    return (
      <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
        <div
          style={{
            background: '#141413',
            borderRadius: '11px',
            padding: '10px 16px',
            color: '#ffffff',
            fontSize: '16px',
            lineHeight: 1.6,
            letterSpacing: '-0.64px',
            maxWidth: '663px',
            whiteSpace: 'pre-wrap',
            wordBreak: 'break-word',
          }}
        >
          {message.content}
        </div>
      </div>
    )
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column' }}>
      <div style={{ maxWidth: '663px' }}>
        <p
          style={{
            color: '#faf9f5',
            fontSize: '16px',
            lineHeight: 1.6,
            letterSpacing: '0.16px',
            marginBottom: '16px',
            whiteSpace: 'pre-wrap',
          }}
        >
          {message.content}
        </p>
        <div style={{ display: 'flex', gap: '16px', marginTop: '16px' }}>
          <button className="flex items-center justify-center bg-transparent border-none cursor-pointer" style={{ padding: '4px', opacity: 0.6, color: '#c2c0b6' }}>
            <Copy size={16} />
          </button>
          <button className="flex items-center justify-center bg-transparent border-none cursor-pointer" style={{ padding: '4px', opacity: 0.6, color: '#c2c0b6' }}>
            <RefreshCw size={16} />
          </button>
          <button className="flex items-center justify-center bg-transparent border-none cursor-pointer" style={{ padding: '4px', opacity: 0.6, color: '#c2c0b6' }}>
            <Share2 size={16} />
          </button>
          <button className="flex items-center justify-center bg-transparent border-none cursor-pointer" style={{ padding: '4px', opacity: 0.6, color: '#c2c0b6' }}>
            <Split size={16} />
          </button>
        </div>
      </div>
    </div>
  )
}

type StreamingBubbleProps = {
  content: string
}

export function StreamingBubble({ content }: StreamingBubbleProps) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column' }}>
      <div style={{ maxWidth: '663px' }}>
        <p
          style={{
            color: '#faf9f5',
            fontSize: '16px',
            lineHeight: 1.6,
            letterSpacing: '0.16px',
            whiteSpace: 'pre-wrap',
          }}
        >
          {content}
        </p>
      </div>
    </div>
  )
}
