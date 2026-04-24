import { describe, expect, it } from "bun:test"
import { streamRun, type RunStreamClient } from "./sse"
import type { Run } from "./types"

describe("streamRun", () => {
  it("在流中途报错后按 after_seq 续传", async () => {
    const afterSeqs: number[] = []
    let statusChecks = 0

    const client: RunStreamClient = {
      async streamEvents(runId: string, afterSeq = 0): Promise<Response> {
        afterSeqs.push(afterSeq)
        if (afterSeq === 0) {
          return createSSEStreamResponse([
            createEventChunk(runId, 1, "message.delta", { content_delta: "partial" }),
            new Error("socket connection error"),
          ])
        }
        return createSSEStreamResponse([
          createEventChunk(runId, 2, "run.completed", {}),
        ])
      },
      async getRun(runId: string): Promise<Run> {
        statusChecks++
        return { run_id: runId, thread_id: "thread_1", status: "running" }
      },
    }

    const seen: number[] = []
    await streamRun(client, "run_1", (event) => {
      seen.push(event.seq)
    })

    expect(seen).toEqual([1, 2])
    expect(afterSeqs).toEqual([0, 1])
    expect(statusChecks).toBe(1)
  })

  it("重连耗尽后抛出可读错误", async () => {
    const client: RunStreamClient = {
      async streamEvents(): Promise<Response> {
        throw new Error("socket connection error")
      },
      async getRun(runId: string): Promise<Run> {
        return { run_id: runId, thread_id: "thread_1", status: "running" }
      },
    }

    await expect(streamRun(client, "run_2", () => undefined)).rejects.toThrow(
      "socket connection error (reconnect exhausted after 5 attempts)",
    )
  }, 15_000)
})

function createEventChunk(
  runId: string,
  seq: number,
  type: string,
  data: Record<string, unknown>,
): string {
  return [
    `id: ${seq}`,
    `event: ${type}`,
    `data: ${JSON.stringify({
      event_id: `evt_${seq}`,
      run_id: runId,
      seq,
      ts: "2026-04-23T00:00:00.000Z",
      type,
      data,
    })}`,
    "",
    "",
  ].join("\n")
}

function createSSEStreamResponse(items: Array<string | Error>): Response {
  const encoder = new TextEncoder()
  let index = 0
  const stream = new ReadableStream<Uint8Array>({
    pull(controller) {
      if (index >= items.length) {
        controller.close()
        return
      }

      const item = items[index++]
      if (item instanceof Error) {
        controller.error(item)
        return
      }

      controller.enqueue(encoder.encode(item))
    },
  })

  return new Response(stream, {
    status: 200,
    headers: {
      "Content-Type": "text/event-stream",
    },
  })
}
