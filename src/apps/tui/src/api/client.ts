import type { Config } from "../lib/config"
import type {
  CreateMessageRequest,
  LlmProvider,
  LoginResponse,
  Me,
  Persona,
  Run,
  RunParams,
  Thread,
  ThreadMessage,
  UploadedThreadAttachment,
} from "./types"

export class ApiClient {
  private accessToken: string | null = null
  private sessionPromise: Promise<string> | null = null

  constructor(private config: Config) {}

  sseTraceEnabled(): boolean {
    return this.config.debugSSE
  }

  private async request<T>(
    method: string,
    path: string,
    options?: { body?: unknown; headers?: Record<string, string> },
  ): Promise<T> {
    const url = `${this.config.host}${path}`
    const headers: Record<string, string> = { ...(options?.headers ?? {}) }

    let body: RequestInit["body"]
    if (isRequestBody(options?.body)) {
      body = options.body
    } else if (options?.body !== undefined) {
      headers["Content-Type"] = headers["Content-Type"] ?? "application/json"
      body = JSON.stringify(options.body)
    }

    const res = await this.authorizedFetch(url, () => ({
      method,
      headers,
      body,
      signal: AbortSignal.timeout(10_000),
    }))

    if (!res.ok) {
      const text = await res.text().catch(() => "")
      throw new Error(`API ${method} ${path}: ${res.status} ${text}`)
    }

    return res.json() as Promise<T>
  }

  async getMe(): Promise<Me> {
    return this.request("GET", "/v1/me")
  }

  async listModels(): Promise<LlmProvider[]> {
    return this.request("GET", "/v1/llm-providers?scope=user")
  }

  async listPersonas(): Promise<Persona[]> {
    return this.request("GET", "/v1/me/selectable-personas")
  }

  async listThreads(limit = 50): Promise<Thread[]> {
    return this.request("GET", `/v1/threads?limit=${limit}`)
  }

  async createThread(title?: string): Promise<{ id: string }> {
    return this.request("POST", "/v1/threads", { body: title ? { title } : {} })
  }

  async uploadStagingAttachment(file: File): Promise<UploadedThreadAttachment> {
    const body = new FormData()
    body.append("file", file)
    return this.request("POST", "/v1/attachments/stage", { body })
  }

  async addMessage(threadId: string, payload: CreateMessageRequest): Promise<void> {
    await this.request("POST", `/v1/threads/${threadId}/messages`, { body: payload })
  }

  async listThreadMessages(threadId: string, limit = 50): Promise<ThreadMessage[]> {
    return this.request("GET", `/v1/threads/${threadId}/messages?limit=${limit}`)
  }

  async createRun(threadId: string, params?: RunParams): Promise<{ run_id: string }> {
    const body: Record<string, string> = {}
    if (params?.persona_id) body.persona_id = params.persona_id
    if (params?.model) body.model = params.model
    if (params?.work_dir) body.work_dir = params.work_dir
    if (params?.reasoning_mode) body.reasoning_mode = params.reasoning_mode
    return this.request("POST", `/v1/threads/${threadId}/runs`, { body })
  }

  async getRun(runId: string): Promise<Run> {
    return this.request("GET", `/v1/runs/${runId}`)
  }

  /** Returns a ReadableStream of SSE events */
  async streamEvents(runId: string, afterSeq = 0): Promise<Response> {
    const url = `${this.config.host}/v1/runs/${runId}/events?follow=true&after_seq=${afterSeq}`
    return this.authorizedFetch(url, () => ({
      headers: { "Accept": "text/event-stream" },
    }))
  }

  // desktop token 只能在本机换 access token,不能直接访问 API;过期时重新换一次。
  private async authorizedFetch(url: string, init: () => RequestInit): Promise<Response> {
    const send = async (token: string) => {
      const req = init()
      const headers = new Headers(req.headers)
      headers.set("Authorization", `Bearer ${token}`)
      return fetch(url, { ...req, headers })
    }

    const res = await send(this.accessToken ?? await this.refreshSession())
    if (res.status !== 401) return res
    await res.body?.cancel()
    return send(await this.refreshSession())
  }

  private refreshSession(): Promise<string> {
    if (!this.sessionPromise) {
      this.sessionPromise = this.requestLocalSession().finally(() => {
        this.sessionPromise = null
      })
    }
    return this.sessionPromise
  }

  private async requestLocalSession(): Promise<string> {
    if (!this.config.desktopToken) {
      throw new Error("desktop token not found (~/.arkloop/desktop.token)")
    }
    const res = await fetch(`${this.config.host}/v1/auth/local-session`, {
      method: "POST",
      headers: { "Authorization": `Bearer ${this.config.desktopToken}` },
      signal: AbortSignal.timeout(10_000),
    })
    if (!res.ok) {
      const text = await res.text().catch(() => "")
      throw new Error(`API POST /v1/auth/local-session: ${res.status} ${text}`)
    }
    const data = await res.json() as LoginResponse
    this.accessToken = data.access_token
    return data.access_token
  }
}

function isRequestBody(value: unknown): value is Exclude<RequestInit["body"], null | undefined> {
  return value instanceof FormData ||
    value instanceof Blob ||
    typeof value === "string" ||
    value instanceof URLSearchParams ||
    value instanceof ArrayBuffer ||
    ArrayBuffer.isView(value)
}
