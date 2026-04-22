import type { Config } from "../lib/config"
import type { Me, Persona, LlmProvider, Thread, Run, RunParams, ThreadMessage } from "./types"

export class ApiClient {
  constructor(private config: Config) {}

  private async request<T>(method: string, path: string, body?: unknown): Promise<T> {
    const url = `${this.config.host}${path}`
    const headers: Record<string, string> = {
      "Content-Type": "application/json",
    }
    if (this.config.token) {
      headers["Authorization"] = `Bearer ${this.config.token}`
    }

    const res = await fetch(url, {
      method,
      headers,
      body: body ? JSON.stringify(body) : undefined,
      signal: AbortSignal.timeout(10_000),
    })

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
    return this.request("POST", "/v1/threads", title ? { title } : {})
  }

  async addMessage(threadId: string, content: string): Promise<void> {
    await this.request("POST", `/v1/threads/${threadId}/messages`, { content })
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
    return this.request("POST", `/v1/threads/${threadId}/runs`, body)
  }

  async getRun(runId: string): Promise<Run> {
    return this.request("GET", `/v1/runs/${runId}`)
  }

  /** Returns a ReadableStream of SSE events */
  async streamEvents(runId: string, afterSeq = 0): Promise<Response> {
    const url = `${this.config.host}/v1/runs/${runId}/events?follow=true&after_seq=${afterSeq}`
    const headers: Record<string, string> = {
      "Accept": "text/event-stream",
    }
    if (this.config.token) {
      headers["Authorization"] = `Bearer ${this.config.token}`
    }
    return fetch(url, { headers })
  }
}
