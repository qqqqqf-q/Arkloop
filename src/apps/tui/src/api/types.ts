export interface Me {
  id: string
  username: string
  account_id: string
  work_enabled: boolean
}

export interface Persona {
  id: string
  persona_key: string
  display_name: string
  selector_name: string
  selector_order: number
  model: string
  reasoning_mode: string
  source: string
}

export interface ProviderModel {
  id: string
  provider_id: string
  model: string
  is_default: boolean
  show_in_picker: boolean
  tags: string[]
  advanced_json?: Record<string, unknown> | null
}

export interface LlmProvider {
  id: string
  name: string
  models: ProviderModel[]
}

export interface Thread {
  id: string
  mode: string
  title: string | null
  created_at: string
  updated_at: string
  active_run_id: string | null
  is_private: boolean
}

export interface MessageAttachmentRef {
  key: string
  filename: string
  mime_type: string
  size: number
}

export type MessageContentPart =
  | { type: "text"; text: string }
  | { type: "image"; attachment: MessageAttachmentRef }

export interface MessageContent {
  parts: MessageContentPart[]
}

export interface CreateMessageRequest {
  content?: string
  content_json?: MessageContent
}

export interface UploadedThreadAttachment {
  key: string
  filename: string
  mime_type: string
  size: number
  kind: "image" | "file"
}

export interface PendingImageAttachment {
  filename: string
  mimeType: string
  size: number
  bytes: Uint8Array
}

export interface MessageComposePayload {
  text: string
  images: PendingImageAttachment[]
}

export interface ThreadMessage {
  id: string
  role: string
  content: string
  content_json?: MessageContent
  created_at: string
  run_id?: string | null
}

export interface Run {
  run_id: string
  thread_id: string
  status: string
  total_input_tokens?: number | null
  total_output_tokens?: number | null
}

export interface RunParams {
  persona_id?: string
  model?: string
  work_dir?: string
  reasoning_mode?: string
}

export interface SSEEvent {
  eventId: string
  runId: string
  seq: number
  ts: string
  type: string
  data: Record<string, unknown>
  toolName?: string
  errorClass?: string
}

export function isTerminalEvent(type: string): boolean {
  return ["run.completed", "run.failed", "run.cancelled", "run.interrupted"].includes(type)
}
