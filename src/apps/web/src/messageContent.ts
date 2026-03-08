import type {
  CreateMessageRequest,
  MessageContent,
  MessageContentPart,
  MessageResponse,
  UploadedThreadAttachment,
} from './api'

export function extractLegacyFilesFromContent(content: string): { text: string; fileNames: string[] } {
  const fileNames: string[] = []
  const text = content
    .replace(/<file name="([^"]+)" encoding="[^"]+">[\s\S]*?<\/file>/g, (_, name: string) => {
      fileNames.push(name)
      return ''
    })
    .trim()
  return { text, fileNames }
}

export function messageTextContent(message: Pick<MessageResponse, 'content' | 'content_json'>): string {
  if (message.content_json?.parts?.length) {
    return message.content_json.parts
      .filter((part): part is Extract<MessageContentPart, { type: 'text' }> => part.type === 'text')
      .map((part) => part.text)
      .join('\n\n')
      .trim()
  }
  return extractLegacyFilesFromContent(message.content).text
}

export function messageAttachmentParts(message: Pick<MessageResponse, 'content' | 'content_json'>): MessageContentPart[] {
  if (message.content_json?.parts?.length) {
    return message.content_json.parts.filter((part) => part.type === 'image' || part.type === 'file')
  }
  return []
}

export function buildMessageRequest(text: string, uploads: UploadedThreadAttachment[]): CreateMessageRequest {
  const parts: MessageContentPart[] = []
  if (text.trim()) {
    parts.push({ type: 'text', text: text.trim() })
  }
  for (const item of uploads) {
    const attachment = {
      key: item.key,
      filename: item.filename,
      mime_type: item.mime_type,
      size: item.size,
    }
    if (item.kind === 'image') {
      parts.push({ type: 'image', attachment })
      continue
    }
    parts.push({ type: 'file', attachment, extracted_text: item.extracted_text ?? '' })
  }
  if (parts.length === 0) {
    return { content: text.trim() }
  }
  return {
    content: text.trim() || undefined,
    content_json: { parts },
  }
}

export function hasMessageAttachments(message: Pick<MessageResponse, 'content' | 'content_json'>): boolean {
  return messageAttachmentParts(message).length > 0 || extractLegacyFilesFromContent(message.content).fileNames.length > 0
}

export function isImagePart(part: MessageContentPart): part is Extract<MessageContentPart, { type: 'image' }> {
  return part.type === 'image'
}

export function isFilePart(part: MessageContentPart): part is Extract<MessageContentPart, { type: 'file' }> {
  return part.type === 'file'
}

export function ensureContent(value?: MessageContent): MessageContent | undefined {
  if (!value?.parts?.length) return undefined
  return value
}
