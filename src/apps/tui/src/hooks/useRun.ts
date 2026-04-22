import type { ApiClient } from "../api/client"
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
  async function sendMessage(input: string) {
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

      startLiveTurn(input)
      await client.addMessage(threadId, input)
      setDebugInfo(`message:created thread=${threadId.slice(0, 8)} chars=${input.length}`)

      const run = await client.createRun(threadId, {
        ...(currentModel() ? { model: currentModel() } : {}),
        persona_id: currentPersona() || "work",
        ...(currentEffort() ? { reasoning_mode: currentEffort() } : {}),
      })
      setDebugInfo(`run:created id=${run.run_id.slice(0, 8)}`)
      setStreaming(true)

      await streamRun(client, run.run_id, (event: SSEEvent) => {
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
      })
    } catch (err) {
      setStreaming(false)
      setDebugInfo(`send:error ${err instanceof Error ? err.message : String(err)}`)
      setError(err instanceof Error ? err.message : String(err))
    }
  }

  return { sendMessage }
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
