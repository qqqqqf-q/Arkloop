import type { Run, SSEEvent } from "./types"
import { isTerminalEvent } from "./types"

export type EventHandler = (event: SSEEvent) => void
export type StreamTraceHandler = (event: string, fields: Record<string, unknown>) => void

export interface RunStreamClient {
  streamEvents(runId: string, afterSeq?: number): Promise<Response>
  getRun(runId: string): Promise<Run>
}

/** Parse SSE text stream into typed events */
export async function* parseSSE(response: Response): AsyncGenerator<SSEEvent> {
  if (!response.body) {
    throw new Error("SSE response body is empty")
  }

  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ""
  let currentId = 0
  let currentEvent = ""
  let currentData = ""

  try {
    while (true) {
      const { done, value } = await reader.read()
      if (done) break

      buffer += decoder.decode(value, { stream: true })
      const lines = buffer.split("\n")
      buffer = lines.pop()! // keep incomplete line

      for (const line of lines) {
        if (line.startsWith(":")) continue // comment

        if (line === "") {
          // end of event
          if (currentData) {
            try {
              const payload = JSON.parse(currentData) as Record<string, unknown>
              const innerData =
                payload.data && typeof payload.data === "object" && !Array.isArray(payload.data)
                  ? payload.data as Record<string, unknown>
                  : {}
              const event: SSEEvent = {
                seq:
                  typeof payload.seq === "number"
                    ? payload.seq
                    : currentId,
                type:
                  typeof payload.type === "string" && payload.type.trim() !== ""
                    ? payload.type
                    : currentEvent,
                eventId: typeof payload.event_id === "string" ? payload.event_id : "",
                runId: typeof payload.run_id === "string" ? payload.run_id : "",
                ts:
                  typeof payload.ts === "string"
                    ? payload.ts
                    : typeof payload.created_at === "string"
                      ? payload.created_at
                      : "",
                data: innerData,
                toolName: typeof payload.tool_name === "string" ? payload.tool_name : undefined,
                errorClass: typeof payload.error_class === "string" ? payload.error_class : undefined,
              }
              if (event.type) {
                yield event
              }
            } catch {
              // skip malformed JSON
            }
          }
          currentId = 0
          currentEvent = ""
          currentData = ""
          continue
        }

        if (line.startsWith("id:")) {
          currentId = parseInt(line.slice(3).trim(), 10) || 0
        } else if (line.startsWith("event:")) {
          currentEvent = line.slice(6).trim()
        } else if (line.startsWith("data:")) {
          const chunk = line.slice(5)
          currentData = currentData ? `${currentData}\n${chunk}` : chunk
        }
      }
    }
  } finally {
    reader.releaseLock()
  }
}

const BACKOFF_BASE = 500
const BACKOFF_MAX = 3000
const MAX_RETRIES = 5

/** Stream a run's events with reconnection logic */
export async function streamRun(
  client: RunStreamClient,
  runId: string,
  onEvent: EventHandler,
  options: { onTrace?: StreamTraceHandler } = {},
): Promise<void> {
  const seen = new Set<number>()
  let lastSeq = 0
  let retries = 0

  for (;;) {
    let gotTerminal = false
    let streamErr: unknown = null
    let response: Response | null = null

    try {
      trace(options.onTrace, "tui_sse_open", {
        run_id: runId,
        after_seq: lastSeq,
        attempt: retries,
      })

      response = await client.streamEvents(runId, lastSeq)
      if (!response.ok) {
        throw new Error(`SSE stream failed: ${response.status}`)
      }

      for await (const event of parseSSE(response)) {
        if (seen.has(event.seq)) continue
        seen.add(event.seq)
        if (event.seq > lastSeq) lastSeq = event.seq

        onEvent(event)

        if (isTerminalEvent(event.type)) {
          gotTerminal = true
          break
        }
      }
    } catch (err) {
      streamErr = err
    } finally {
      await closeResponse(response)
    }

    if (gotTerminal) return

    trace(options.onTrace, "tui_sse_read_stop", {
      run_id: runId,
      last_seq: lastSeq,
      terminal: false,
      err: describeError(streamErr),
    })

    // Stream ended without terminal event -- check run status
    let run: Run
    try {
      run = await client.getRun(runId)
    } catch (err) {
      if (streamErr == null) {
        streamErr = err
      }
      trace(options.onTrace, "tui_sse_run_status_error", {
        run_id: runId,
        last_seq: lastSeq,
        err: describeError(err),
      })
      if (retries >= MAX_RETRIES) {
        trace(options.onTrace, "tui_sse_reconnect_exhausted", {
          run_id: runId,
          last_seq: lastSeq,
          attempts: retries,
          err: describeError(streamErr),
        })
        throw reconnectError(streamErr, retries)
      }

      const delay = Math.min(BACKOFF_BASE * 2 ** retries, BACKOFF_MAX)
      trace(options.onTrace, "tui_sse_reconnect_scheduled", {
        run_id: runId,
        last_seq: lastSeq,
        attempt: retries + 1,
        delay_ms: delay,
        err: describeError(streamErr),
      })
      await new Promise(r => setTimeout(r, delay))
      retries++
      continue
    }

    trace(options.onTrace, "tui_sse_run_status", {
      run_id: runId,
      last_seq: lastSeq,
      status: run.status,
    })
    if (!isActiveRunStatus(run.status)) return

    if (retries >= MAX_RETRIES) {
      trace(options.onTrace, "tui_sse_reconnect_exhausted", {
        run_id: runId,
        last_seq: lastSeq,
        attempts: retries,
        err: describeError(streamErr),
      })
      throw reconnectError(streamErr, retries)
    }

    // Exponential backoff
    const delay = Math.min(BACKOFF_BASE * 2 ** retries, BACKOFF_MAX)
    trace(options.onTrace, "tui_sse_reconnect_scheduled", {
      run_id: runId,
      last_seq: lastSeq,
      attempt: retries + 1,
      delay_ms: delay,
      err: describeError(streamErr),
    })
    await new Promise(r => setTimeout(r, delay))
    retries++
  }
}

function isActiveRunStatus(status: string): boolean {
  return status === "running" || status === "cancelling"
}

function trace(onTrace: StreamTraceHandler | undefined, event: string, fields: Record<string, unknown>) {
  onTrace?.(event, fields)
}

function describeError(err: unknown): string {
  if (err == null) return ""
  return err instanceof Error ? err.message : String(err)
}

function reconnectError(err: unknown, attempts: number): Error {
  const suffix = `reconnect exhausted after ${attempts} attempts`
  if (err instanceof Error && err.message.trim() !== "") {
    return new Error(`${err.message} (${suffix})`)
  }
  return new Error(`SSE stream failed (${suffix})`)
}

async function closeResponse(response: Response | null): Promise<void> {
  if (!response?.body) return
  try {
    await response.body.cancel()
  } catch {
    // ignore body close errors from broken streams
  }
}
