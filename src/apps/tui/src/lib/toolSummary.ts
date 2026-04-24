import type { TurnSegment } from "./runTurns"
import { redactDataUrlsInString } from "../../../shared/src/debugPayloadRedact.ts"
import { pickLogicalToolName } from "../../../shared/src/tool-names.ts"

export type ToolRenderItem = {
  toolName: string
  summary: string
  status: "pending" | "success" | "error"
  errorSummary?: string
  resultSummary?: string
  resultContent?: string
  resultLineCount?: number
}

export type MessageRenderSegment =
  | { kind: "assistant"; text: string; isFinal?: boolean }
  | { kind: "tool"; tool: ToolRenderItem }

type LiveToolCall = {
  toolName: string
  arguments: Record<string, unknown>
  result?: unknown
  errorClass?: string
}

const COUNT_KEYS = ["count", "total", "result_count", "results_count", "match_count", "file_count", "line_count"]
const ARRAY_COUNT_KEYS = ["results", "items", "files", "matches", "entries", "rows", "paths"]
const PRIMARY_ARG_KEYS = [
  "command",
  "cmd",
  "path",
  "paths",
  "file_path",
  "filepath",
  "target_path",
  "url",
  "urls",
  "uri",
  "q",
  "query",
  "pattern",
  "name",
  "text",
  "prompt",
  "message",
]

function asRecord(value: unknown): Record<string, unknown> | undefined {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : undefined
}

function cleanInline(value: unknown, maxLength = 88): string | undefined {
  if (value == null) return undefined
  const text =
    typeof value === "string"
      ? value
      : typeof value === "number" || typeof value === "boolean"
        ? String(value)
        : Array.isArray(value)
        ? value.map((item) => cleanInline(item, 40)).filter(Boolean).join(", ")
        : JSON.stringify(value)
  if (!text) return undefined
  const compact = redactDataUrlsInString(text).replace(/\s+/g, " ").trim()
  if (!compact) return undefined
  return compact.length > maxLength ? `${compact.slice(0, maxLength - 1)}...` : compact
}

function humanizeToolName(toolName: string): string {
  const cleaned = toolName.trim()
  if (!cleaned) return "Tool"
  return cleaned
    .split(/[._-]+/)
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(" ")
}

function pickFirstValue(record: Record<string, unknown>, keys: readonly string[]): unknown {
  for (const key of keys) {
    const value = record[key]
    if (value == null) continue
    if (typeof value === "string" && value.trim() === "") continue
    if (Array.isArray(value) && value.length === 0) continue
    return value
  }
  return undefined
}

function formatPathLike(value: unknown): string | undefined {
  if (typeof value === "string") return cleanInline(value, 72)
  if (Array.isArray(value)) {
    const first = value.find((item) => typeof item === "string")
    if (typeof first !== "string") return undefined
    const firstText = cleanInline(first, value.length > 1 ? 48 : 72)
    if (!firstText) return undefined
    return value.length > 1 ? `${firstText} +${value.length - 1}` : firstText
  }
  return undefined
}

function formatQuoted(value: unknown): string | undefined {
  const text = cleanInline(value, 64)
  return text ? `"${text}"` : undefined
}

function summarizeArgs(record: Record<string, unknown>, limit = 2): string | undefined {
  const parts = Object.entries(record)
    .filter(([, value]) => value != null && value !== "")
    .slice(0, limit)
    .map(([key, value]) => `${key}=${cleanInline(value, 32) ?? "..."}`)
    .filter(Boolean)
  if (parts.length === 0) return undefined
  const hidden = Object.keys(record).length - parts.length
  return hidden > 0 ? `${parts.join(", ")} +${hidden}` : parts.join(", ")
}

function summarizeCall(toolName: string, args: Record<string, unknown>): string {
  const canonical = pickLogicalToolName(undefined, toolName)
  const command = pickFirstValue(args, ["command", "cmd"])
  const pathLike = pickFirstValue(args, ["path", "paths", "file_path", "filepath", "target_path"])
  const urlLike = pickFirstValue(args, ["url", "urls", "uri"])
  const queryLike = pickFirstValue(args, ["q", "query", "pattern", "text", "prompt", "message"])

  if (canonical === "web_search") {
    return `Search web for ${formatQuoted(queryLike) ?? "query"}`
  }
  if (canonical === "web_fetch") {
    return `Fetch ${formatPathLike(urlLike) ?? "resource"}`
  }
  if (canonical.startsWith("memory_")) {
    if (canonical.includes("search")) return `Search memory for ${formatQuoted(queryLike) ?? "query"}`
    if (canonical.includes("read")) return "Read memory"
    if (canonical.includes("write")) return `Write memory${queryLike ? ` ${formatQuoted(queryLike)}` : ""}`
  }
  if (canonical.startsWith("notebook_")) {
    if (canonical.includes("read")) return "Read notebook"
    if (canonical.includes("write")) return "Write notebook"
    if (canonical.includes("edit")) return "Edit notebook"
  }
  if (canonical.includes("exec") || canonical.includes("bash") || canonical.includes("shell") || canonical.includes("command")) {
    return `Bash(${cleanInline(command, 72) ?? canonical})`
  }
  if (canonical.includes("read") || canonical.includes("open")) {
    return `Read ${formatPathLike(pathLike ?? urlLike) ?? "resource"}`
  }
  if (canonical.includes("write") || canonical.includes("edit") || canonical.includes("apply_patch") || canonical.includes("replace")) {
    return `Edit ${formatPathLike(pathLike) ?? "target"}`
  }
  if (canonical.includes("delete") || canonical.includes("remove") || canonical.includes("forget")) {
    return `Delete ${formatPathLike(pathLike) ?? "target"}`
  }
  if (canonical.includes("list") || canonical === "ls" || canonical.includes("glob")) {
    return `List ${formatPathLike(pathLike) ?? "files"}`
  }
  if (canonical.includes("search") || canonical.includes("find") || canonical.includes("grep")) {
    return `Search ${formatQuoted(queryLike ?? pathLike) ?? canonical}`
  }
  if (urlLike) {
    return `${humanizeToolName(canonical)} ${formatPathLike(urlLike)}`
  }
  if (command) {
    return `${humanizeToolName(canonical)} ${cleanInline(command, 72)}`
  }
  const primary = pickFirstValue(args, PRIMARY_ARG_KEYS)
  if (primary != null) {
    const primaryText = cleanInline(primary, 64)
    if (primaryText) return `${humanizeToolName(canonical)} ${primaryText}`
  }
  const argSummary = summarizeArgs(args)
  return argSummary ? `${humanizeToolName(canonical)} ${argSummary}` : humanizeToolName(canonical)
}

function summarizeCount(record: Record<string, unknown>): string | undefined {
  for (const key of COUNT_KEYS) {
    const value = record[key]
    if (typeof value === "number" && Number.isFinite(value) && value > 0) {
      const label = key.includes("file") ? "files" : key.includes("line") ? "lines" : key.includes("match") ? "matches" : "results"
      return `${value} ${label}`
    }
  }
  for (const key of ARRAY_COUNT_KEYS) {
    const value = record[key]
    if (Array.isArray(value) && value.length > 0) {
      const label = key === "files" || key === "paths" ? "files" : key === "matches" ? "matches" : "results"
      return `${value.length} ${label}`
    }
  }
  return undefined
}

function summarizeSuccessResult(result: unknown): string | undefined {
  if (result == null) return undefined
  if (Array.isArray(result)) return result.length > 0 ? `${result.length} results` : undefined
  if (typeof result === "string") return undefined
  const record = asRecord(result)
  if (!record) return undefined

  const exitCode = record["exit_code"]
  if (typeof exitCode === "number" && exitCode > 0) {
    return `exit ${exitCode}`
  }

  const count = summarizeCount(record)
  if (count) return count

  const stdout = cleanInline(record["stdout"], 56)
  if (stdout && stdout.length <= 32) return stdout

  return undefined
}

function summarizeError(result: unknown, errorClass?: string): string | undefined {
  const record = asRecord(result)
  const detail = record
    ? cleanInline(
        pickFirstValue(record, ["error", "message", "stderr", "detail", "details", "content", "output"]),
        96,
      )
    : cleanInline(result, 96)
  if (detail && errorClass && detail !== errorClass) return `${errorClass}: ${detail}`
  return detail ?? errorClass
}

function extractResultContent(result: unknown): { content: string; lineCount: number } | undefined {
  if (result == null) return undefined
  if (typeof result === "string") {
    if (!result.trim()) return undefined
    const lines = result.split("\n")
    return { content: result, lineCount: lines.length }
  }
  // handle arrays of content blocks: [{type: "text", text: "..."}, ...]
  if (Array.isArray(result)) {
    const texts: string[] = []
    for (const item of result) {
      const r = asRecord(item)
      if (r && typeof r["text"] === "string") texts.push(r["text"])
    }
    const joined = texts.join("\n")
    if (joined.trim()) {
      const lines = joined.split("\n")
      return { content: joined, lineCount: lines.length }
    }
    return undefined
  }
  const record = asRecord(result)
  if (!record) return undefined
  // unwrap nested result key (one level only)
  if (record["result"] != null && typeof record["result"] === "object" && !Array.isArray(record["result"])) {
    const inner = extractResultContent(record["result"])
    if (inner) return inner
  }
  const text =
    record["stdout"] ?? record["output"] ?? record["content"] ?? record["text"] ?? record["message"] ?? record["matches"]
  if (typeof text === "string" && text.trim()) {
    const lines = text.split("\n")
    return { content: text, lineCount: lines.length }
  }
  // structured arrays: files/entries/results → extract names/paths
  for (const key of ["files", "entries", "results", "items", "paths"]) {
    const arr = record[key]
    if (!Array.isArray(arr) || arr.length === 0) continue
    const lines = arr.map((item: unknown) => {
      if (typeof item === "string") return item
      const r = asRecord(item)
      if (!r) return String(item)
      return (r["path"] ?? r["name"] ?? r["title"] ?? r["file"]) as string | undefined ?? JSON.stringify(r)
    })
    const joined = lines.join("\n")
    if (joined.trim()) return { content: joined, lineCount: lines.length }
  }
  // fallback: stderr when stdout is absent
  if (typeof record["stderr"] === "string" && record["stderr"].trim() && record["stdout"] == null) {
    const lines = record["stderr"].split("\n")
    return { content: record["stderr"], lineCount: lines.length }
  }
  return undefined
}

export function summarizeToolRenderItem(input: {
  toolName: string
  args?: Record<string, unknown>
  result?: unknown
  errorClass?: string
}): ToolRenderItem {
  const toolName = pickLogicalToolName(undefined, input.toolName)
  const args = input.args ?? {}
  const summary = summarizeCall(toolName, args)

  if (input.errorClass) {
    const extracted = extractResultContent(input.result)
    return {
      toolName,
      summary,
      status: "error",
      errorSummary: summarizeError(input.result, input.errorClass),
      resultContent: extracted?.content,
      resultLineCount: extracted?.lineCount,
    }
  }

  const extracted = extractResultContent(input.result)
  return {
    toolName,
    summary,
    status: input.result === undefined ? "pending" : "success",
    resultContent: extracted?.content,
    resultLineCount: extracted?.lineCount,
  }
}

export function compressTurnSegments(segments: readonly TurnSegment[]): MessageRenderSegment[] {
  const output: MessageRenderSegment[] = []
  const toolIndexByCallId = new Map<string, number>()

  for (const segment of segments) {
    if (segment.kind === "assistant") {
      output.push({ kind: "assistant", text: segment.text, isFinal: segment.isFinal })
      continue
    }

    if (segment.kind === "tool_call") {
      const tool = summarizeToolRenderItem({
        toolName: segment.toolName,
        args: segment.argsJSON,
      })
      output.push({ kind: "tool", tool })
      if (segment.toolCallId) {
        toolIndexByCallId.set(segment.toolCallId, output.length - 1)
      }
      continue
    }

    const tool = summarizeToolRenderItem({
      toolName: segment.toolName,
      result: segment.resultJSON,
      errorClass: segment.errorClass,
    })
    const existingIndex = segment.toolCallId ? toolIndexByCallId.get(segment.toolCallId) : undefined
    if (existingIndex != null) {
      const existing = output[existingIndex]
      if (existing?.kind === "tool") {
        existing.tool = {
          ...existing.tool,
          status: tool.status,
          errorSummary: tool.errorSummary,
          resultContent: tool.resultContent,
          resultLineCount: tool.resultLineCount,
        }
        continue
      }
    }
    if (tool.status === "error") {
      output.push({ kind: "tool", tool })
    }
  }

  return output
}

export function summarizeLiveToolCall(call: LiveToolCall): ToolRenderItem {
  return summarizeToolRenderItem({
    toolName: call.toolName,
    args: call.arguments,
    result: call.result,
    errorClass: call.errorClass,
  })
}
