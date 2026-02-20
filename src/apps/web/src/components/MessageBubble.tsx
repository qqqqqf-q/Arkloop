import { Copy, RefreshCw, Share2, Split, Paperclip } from 'lucide-react'
import type { MessageResponse } from '../api'

type Props = {
  message: MessageResponse
}

function extractFilesFromContent(content: string): { text: string; fileNames: string[] } {
  const fileNames: string[] = []
  const text = content
    .replace(/<file name="([^"]+)" encoding="[^"]+">[\s\S]*?<\/file>/g, (_, name: string) => {
      fileNames.push(name)
      return ''
    })
    .trim()
  return { text, fileNames }
}

export function MessageBubble({ message }: Props) {
  if (message.role === 'user') {
    const { text, fileNames } = extractFilesFromContent(message.content)
    return (
      <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
        <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'flex-end', gap: '8px', maxWidth: '663px' }}>
          {fileNames.length > 0 && (
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: '6px', justifyContent: 'flex-end' }}>
              {fileNames.map((name) => (
                <div
                  key={name}
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: '6px',
                    background: '#1e1e1c',
                    border: '0.5px solid #3a3a38',
                    borderRadius: '8px',
                    padding: '4px 10px',
                    fontSize: '12px',
                    color: '#c2c0b6',
                  }}
                >
                  <Paperclip size={11} style={{ color: '#7b7970', flexShrink: 0 }} />
                  <span style={{ maxWidth: '160px', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                    {name}
                  </span>
                </div>
              ))}
            </div>
          )}
          {text && (
            <div
              style={{
                background: '#141413',
                borderRadius: '11px',
                padding: '10px 16px',
                color: '#ffffff',
                fontSize: '16px',
                lineHeight: 1.6,
                letterSpacing: '-0.64px',
                whiteSpace: 'pre-wrap',
                wordBreak: 'break-word',
              }}
            >
              {text}
            </div>
          )}
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
        <div style={{ display: 'flex', gap: '4px', marginTop: '16px' }}>
          <button className="flex h-7 w-7 items-center justify-center rounded-lg text-[#c2c0b6] opacity-60 transition-[opacity,background] duration-150 hover:bg-[#141413] hover:opacity-100 cursor-pointer border-none bg-transparent">
            <Copy size={15} />
          </button>
          <button className="flex h-7 w-7 items-center justify-center rounded-lg text-[#c2c0b6] opacity-60 transition-[opacity,background] duration-150 hover:bg-[#141413] hover:opacity-100 cursor-pointer border-none bg-transparent">
            <RefreshCw size={15} />
          </button>
          <button className="flex h-7 w-7 items-center justify-center rounded-lg text-[#c2c0b6] opacity-60 transition-[opacity,background] duration-150 hover:bg-[#141413] hover:opacity-100 cursor-pointer border-none bg-transparent">
            <Share2 size={15} />
          </button>
          <button className="flex h-7 w-7 items-center justify-center rounded-lg text-[#c2c0b6] opacity-60 transition-[opacity,background] duration-150 hover:bg-[#141413] hover:opacity-100 cursor-pointer border-none bg-transparent">
            <Split size={15} />
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
