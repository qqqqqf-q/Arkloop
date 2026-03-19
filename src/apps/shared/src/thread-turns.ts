export type ThreadMessageContentPart = {
  type: string
  text?: string
}

export type ThreadMessage = {
  id: string
  role: string
  content: string
  content_json?: {
    parts?: ThreadMessageContentPart[]
  }
  created_at: string
  run_id?: string | null
}

export type ThreadTurn = {
  key: string
  userText: string
  assistantText: string
  isCurrent: boolean
}

export function threadMessageTextContent(message: Pick<ThreadMessage, 'content' | 'content_json'>): string {
  const parts = message.content_json?.parts
  if (parts?.length) {
    return parts
      .filter((part) => part.type === 'text')
      .map((part) => part.text ?? '')
      .join('\n\n')
      .trim()
  }
  return message.content.trim()
}

export function buildThreadTurns(
  messages: ThreadMessage[],
  currentRunId: string,
  currentPrompt?: string,
): ThreadTurn[] {
  const ordered = [...messages].sort((left, right) => left.created_at.localeCompare(right.created_at))
  const turns: Array<{ key: string; user?: ThreadMessage; assistants: ThreadMessage[] }> = []
  let current: { key: string; user?: ThreadMessage; assistants: ThreadMessage[] } | null = null

  for (const message of ordered) {
    if (message.role === 'user') {
      if (current) turns.push(current)
      current = { key: message.id, user: message, assistants: [] }
      continue
    }
    if (message.role === 'assistant' && current) {
      current.assistants.push(message)
    }
  }

  if (current) turns.push(current)

  let currentIndex = turns.findIndex((turn) =>
    turn.assistants.some((message) => message.run_id === currentRunId),
  )
  if (currentIndex < 0 && currentPrompt) {
    currentIndex = turns.findIndex(
      (turn) => turn.user && threadMessageTextContent(turn.user).trim() === currentPrompt.trim(),
    )
  }

  const visibleTurns = currentIndex >= 0 ? turns.slice(0, currentIndex + 1) : turns
  return visibleTurns
    .map((turn, index) => ({
      key: turn.key,
      userText: turn.user ? threadMessageTextContent(turn.user).trim() : '',
      assistantText: turn.assistants.map((message) => threadMessageTextContent(message).trim()).filter(Boolean).join('\n\n'),
      isCurrent: currentIndex >= 0 && index === visibleTurns.length - 1,
    }))
    .filter((turn) => turn.userText || turn.assistantText)
}
