import type {
  UploadedThreadAttachment,
} from './api'
import type {
  AgentCreateMessageRequest,
  AgentMessage,
  AgentMessageContent,
  AgentMessageContentPart,
  AgentUIMessagePart,
} from './agent-ui'

export type UserMessageDeliveryStatus = 'pending' | 'sent' | 'failed'

const LOCAL_USER_MESSAGE_PREFIX = 'local-user:'

export function createClientMessageId(): string {
  return crypto.randomUUID()
}

export function isLocalUserMessage(message: Pick<AgentMessage, 'id' | 'role'>): boolean {
  return message.role === 'user' && message.id.startsWith(LOCAL_USER_MESSAGE_PREFIX)
}

export function messageClientMessageId(message: Pick<AgentMessage, 'metadata'>): string | undefined {
  return message.metadata?.clientMessageId || undefined
}

export function messageDeliveryStatus(message: Pick<AgentMessage, 'metadata'>): UserMessageDeliveryStatus | undefined {
  return message.metadata?.deliveryStatus
}

function messageMatchesLocalDelivery(message: AgentMessage, localMessageId: string, clientMessageId: string): boolean {
  return message.id === localMessageId || messageClientMessageId(message) === clientMessageId
}

export function insertMessageByCreatedAt(messages: AgentMessage[], message: AgentMessage): AgentMessage[] {
  if (messages.some((item) => item.id === message.id)) return messages
  const messageTime = Date.parse(message.createdAt)
  if (!Number.isFinite(messageTime)) return [...messages, message]
  const index = messages.findIndex((item) => {
    const itemTime = Date.parse(item.createdAt)
    return Number.isFinite(itemTime) && itemTime > messageTime
  })
  if (index < 0) return [...messages, message]
  return [...messages.slice(0, index), message, ...messages.slice(index)]
}

export function isUndeliveredLocalUserMessage(message: AgentMessage): boolean {
  if (!isLocalUserMessage(message)) return false
  const status = messageDeliveryStatus(message)
  return status === 'pending' || status === 'failed'
}

export function mergeUndeliveredLocalUserMessages(
  remoteMessages: AgentMessage[],
  currentMessages: AgentMessage[],
): AgentMessage[] {
  const remoteClientMessageIds = new Set(
    remoteMessages
      .map(messageClientMessageId)
      .filter((id): id is string => !!id),
  )
  let merged = remoteMessages.filter((message) => !isUndeliveredLocalUserMessage(message))
  for (const message of currentMessages) {
    if (!isUndeliveredLocalUserMessage(message)) continue
    const clientMessageId = messageClientMessageId(message)
    if (clientMessageId && remoteClientMessageIds.has(clientMessageId)) continue
    merged = insertMessageByCreatedAt(merged, message)
  }
  return merged
}

export function undeliveredLocalUserMessages(messages: AgentMessage[]): AgentMessage[] {
  return messages.filter(isUndeliveredLocalUserMessage)
}

export function buildUserMessageRetryRequest(
  message: AgentMessage,
  clientMessageId: string,
): AgentCreateMessageRequest {
  return {
    content: message.content || undefined,
    contentJson: message.contentJson,
    clientMessageId,
  }
}

export function withMessageDeliveryStatus(
  message: AgentMessage,
  deliveryStatus: UserMessageDeliveryStatus,
  clientMessageId = messageClientMessageId(message),
): AgentMessage {
  return {
    ...message,
    metadata: {
      ...message.metadata,
      createdAt: message.metadata?.createdAt ?? message.createdAt,
      ...(clientMessageId ? { clientMessageId } : {}),
      deliveryStatus,
    },
  }
}

export function markDeliveryFailed(
  messages: AgentMessage[],
  keys: Array<{ messageId: string; clientMessageId: string }>,
): AgentMessage[] {
  if (keys.length === 0) return messages
  const messageIds = new Set(keys.map((k) => k.messageId))
  const clientMessageIds = new Set(keys.map((k) => k.clientMessageId))
  return messages.map((message) =>
    message.metadata?.deliveryStatus === 'pending' &&
    (messageIds.has(message.id) || clientMessageIds.has(messageClientMessageId(message) ?? ''))
      ? withMessageDeliveryStatus(message, 'failed', messageClientMessageId(message))
      : message,
  )
}

export function replaceLocalUserMessage(
  messages: AgentMessage[],
  localMessageId: string,
  clientMessageId: string,
  remoteMessage: AgentMessage,
): AgentMessage[] {
  let replaced = false
  const delivered = withMessageDeliveryStatus(remoteMessage, 'sent', clientMessageId)
  const next = messages.flatMap((message) => {
    if (message.id === remoteMessage.id) {
      replaced = true
      return [delivered]
    }
    if (messageMatchesLocalDelivery(message, localMessageId, clientMessageId)) {
      replaced = true
      return [delivered]
    }
    return [message]
  })
  return replaced ? next : [...next, delivered]
}

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

export function messageTextContent(message: Pick<AgentMessage, 'content' | 'contentJson'>): string {
  if (message.contentJson?.parts?.length) {
    return message.contentJson.parts
      .filter((part): part is Extract<AgentMessageContentPart, { type: 'text' }> => part.type === 'text')
      .map((part) => part.text)
      .join('\n\n')
      .trim()
  }
  return extractLegacyFilesFromContent(message.content).text
}

export function messageAttachmentParts(message: Pick<AgentMessage, 'content' | 'contentJson'>): AgentMessageContentPart[] {
  if (message.contentJson?.parts?.length) {
    return message.contentJson.parts.filter((part) => part.type === 'image' || part.type === 'file')
  }
  return []
}

export function buildMessageRequest(text: string, uploads: UploadedThreadAttachment[]): AgentCreateMessageRequest {
  const parts: AgentMessageContentPart[] = []
  if (text.trim()) {
    parts.push({ type: 'text', text: text.trim() })
  }
  for (const item of uploads) {
    const attachment = {
      key: item.key,
      filename: item.filename,
      mediaType: item.mime_type,
      size: item.size,
    }
    if (item.kind === 'image') {
      parts.push({ type: 'image', attachment })
      continue
    }
    parts.push({ type: 'file', attachment, extractedText: item.extracted_text ?? '' })
  }
  if (parts.length === 0) {
    return { content: text.trim() }
  }
  return {
    content: text.trim() || undefined,
    contentJson: { parts },
  }
}

export function buildAgentUIParts(contentJson: AgentMessageContent | undefined, content: string): AgentUIMessagePart[] {
  if (!contentJson?.parts?.length) {
    return content ? [{ type: 'text', text: content, state: 'done' }] : []
  }
  return contentJson.parts.flatMap<AgentUIMessagePart>((part) => {
    if (part.type === 'text') return [{ type: 'text', text: part.text, state: 'done' }]
    return [{
      type: 'file',
      mediaType: part.attachment.mediaType,
      filename: part.attachment.filename,
      url: `attachment:${part.attachment.key}`,
    }]
  })
}

export function buildOptimisticUserMessage(
  request: AgentCreateMessageRequest,
  clientMessageId: string,
  createdAt = new Date().toISOString(),
): AgentMessage {
  const content = request.content ?? messageTextContent({
    content: '',
    contentJson: request.contentJson,
  })
  return {
    id: `${LOCAL_USER_MESSAGE_PREFIX}${clientMessageId}`,
    role: 'user',
    content,
    contentJson: request.contentJson,
    createdAt,
    metadata: {
      createdAt,
      clientMessageId,
      deliveryStatus: 'pending',
    },
    parts: buildAgentUIParts(request.contentJson, content),
  }
}

export function hasMessageAttachments(message: Pick<AgentMessage, 'content' | 'contentJson'>): boolean {
  return messageAttachmentParts(message).length > 0 || extractLegacyFilesFromContent(message.content).fileNames.length > 0
}

export function isImagePart(part: AgentMessageContentPart): part is Extract<AgentMessageContentPart, { type: 'image' }> {
  return part.type === 'image'
}

export function isFilePart(part: AgentMessageContentPart): part is Extract<AgentMessageContentPart, { type: 'file' }> {
  return part.type === 'file'
}

export function ensureContent(value?: AgentMessageContent): AgentMessageContent | undefined {
  if (!value?.parts?.length) return undefined
  return value
}

const PASTED_FILENAME_RE = /^pasted-\d+\.txt$/

export function isPastedFile(filename: string): boolean {
  return PASTED_FILENAME_RE.test(filename)
}
