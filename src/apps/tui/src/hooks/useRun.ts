import type { ApiClient } from "../api/client"
import type {
  MessageComposePayload,
  MessageContentPart,
  PendingImageAttachment,
  UploadedThreadAttachment,
} from "../api/types"
import { streamRun } from "../api/sse"
import type { SSEEvent } from "../api/types"
import {
  commitLiveTurns,
  setStreaming,
  setError,
  setDebugInfo,
  startLiveTurn,
  appendRunEvent,
  liveAssistantTurn,
} from "../store/chat"
import { currentEffort, currentModel, currentPersona, setCurrentThreadId, setTokenUsage, tokenUsage, currentThreadId } from "../store/app"
import { writeThreadMode } from "../lib/threadMode"

export function createRunHandler(client: ApiClient) {
  async function sendMessage(input: MessageComposePayload) {
    const preview = formatPendingMessage(input)
    if (!preview) return
    setError(null)
    try {
      let threadId = currentThreadId()
      setDebugInfo(`send:start thread=${threadId ?? "new"} model=${currentModel() || "auto"} effort=${currentEffort()}`)
      if (!threadId) {
        const thread = await client.createThread()
        threadId = thread.id
        writeThreadMode(threadId, "work")
        setCurrentThreadId(threadId)
        setDebugInfo(`thread:created id=${threadId.slice(0, 8)}`)
      }

      startLiveTurn(preview)
      const uploads = await Promise.all(input.images.map((image) => uploadImage(client, image)))
      await client.addMessage(threadId, buildCreateMessageRequest(input.text, uploads))
      setDebugInfo(`message:created thread=${threadId.slice(0, 8)} chars=${input.text.length} images=${uploads.length}`)

      const run = await client.createRun(threadId, {
        ...(currentModel() ? { model: currentModel() } : {}),
        persona_id: currentPersona() || "work",
        ...(currentEffort() ? { reasoning_mode: currentEffort() } : {}),
      })
      setDebugInfo(`run:created id=${run.run_id.slice(0, 8)}`)
      setStreaming(true)

      await streamRun(
        client,
        run.run_id,
        (event: SSEEvent) => {
          appendRunEvent(toRunEventRaw(event))

          switch (event.type) {
            case "llm.turn.completed": {
              updateUsage(event)
              break
            }
            case "run.completed": {
              updateUsage(event)
              if (!liveAssistantTurn()) {
                commitLiveTurns()
              }
              setStreaming(false)
              break
            }
            case "run.failed": {
              if (!liveAssistantTurn()) {
                commitLiveTurns()
              }
              const errMsg = (event.data.error as string) ?? "Run failed"
              setError(errMsg)
              setStreaming(false)
              break
            }
            case "run.cancelled":
            case "run.interrupted": {
              if (!liveAssistantTurn()) {
                commitLiveTurns()
              }
              setStreaming(false)
              break
            }
          }
        },
        { onTrace: createSSETraceWriter(client, run.run_id) },
      )
    } catch (err) {
      setStreaming(false)
      setDebugInfo(`send:error ${err instanceof Error ? err.message : String(err)}`)
      setError(err instanceof Error ? err.message : String(err))
    }
  }

  return { sendMessage }
}

function uploadImage(client: ApiClient, image: PendingImageAttachment): Promise<UploadedThreadAttachment> {
  const file = new File([image.bytes], image.filename, { type: image.mimeType })
  return client.uploadStagingAttachment(file).then((upload) => {
    if (upload.kind !== "image") {
      throw new Error("Uploaded attachment is not an image")
    }
    return upload
  })
}

function buildCreateMessageRequest(text: string, uploads: UploadedThreadAttachment[]) {
  const normalizedText = text.trim()
  if (uploads.length === 0) {
    return { content: normalizedText }
  }

  const parts: MessageContentPart[] = []
  if (normalizedText) {
    parts.push({ type: "text", text: normalizedText })
  }
  for (const upload of uploads) {
    parts.push({
      type: "image",
      attachment: {
        key: upload.key,
        filename: upload.filename,
        mime_type: upload.mime_type,
        size: upload.size,
      },
    })
  }
  return {
    ...(normalizedText ? { content: normalizedText } : {}),
    content_json: { parts },
  }
}

function formatPendingMessage(input: MessageComposePayload): string {
  const parts: string[] = []
  const text = input.text.trim()
  if (text) parts.push(text)
  for (const image of input.images) {
    parts.push(`[图片: ${image.filename}]`)
  }
  return parts.join("\n\n")
}

function updateUsage(event: SSEEvent) {
  const usage = event.data.usage as {
    input_tokens?: number
    output_tokens?: number
    cache_read_input_tokens?: number
    cache_creation_input_tokens?: number
  } | undefined
  if (!usage) return
  const input = usage.input_tokens ?? 0
  const cacheRead = usage.cache_read_input_tokens ?? 0
  const realPrompt = event.data.last_real_prompt_tokens as number | undefined
  const prev = tokenUsage()
  setTokenUsage({
    input: prev.input + input,
    output: prev.output + (usage.output_tokens ?? 0),
    context: realPrompt ?? (input + cacheRead),
  })
}

function toRunEventRaw(event: SSEEvent) {
  return {
    event_id: event.eventId,
    run_id: event.runId,
    seq: event.seq,
    ts: event.ts,
    type: event.type,
    data: event.data,
    tool_name: event.toolName,
    error_class: event.errorClass,
  }
}

function createSSETraceWriter(client: ApiClient, runId: string) {
  if (!client.sseTraceEnabled()) return undefined

  return (event: string, fields: Record<string, unknown>) => {
    const payload = {
      ts: new Date().toISOString(),
      event,
      run_id: runId,
      ...fields,
    }
    process.stderr.write(`${JSON.stringify(payload)}\n`)
  }
}
