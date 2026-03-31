import * as fs from 'fs'
import * as os from 'os'
import * as path from 'path'
import { inspect } from 'util'

const LOG_DIR = path.join(os.homedir(), '.arkloop', 'logs')
const MAIN_LOG_PATH = path.join(LOG_DIR, 'desktop-main.log')
const SIDECAR_LOG_PATH = path.join(LOG_DIR, 'desktop-sidecar.log')
const MAX_LOG_FILE_BYTES = 10 * 1024 * 1024
const MAX_LOG_BACKUPS = 3
const MAX_LOG_LINE_CHARS = 16 * 1024
const MAX_LOG_CHUNK_LINES = 400
const MAX_LOG_ARG_CHARS = 2048

let initialized = false

function ensureLogDir(): void {
  fs.mkdirSync(LOG_DIR, { recursive: true })
}

function now(): string {
  return new Date().toISOString()
}

function truncateText(value: string, limit = MAX_LOG_LINE_CHARS): string {
  if (value.length <= limit) return value
  return `${value.slice(0, limit)}… [truncated ${value.length - limit} chars]`
}

function stringifyArgs(args: unknown[]): string {
  return truncateText(args
    .map((arg) => {
      const value = typeof arg === 'string'
        ? arg
        : inspect(arg, {
        depth: 6,
        breakLength: 120,
        maxArrayLength: 50,
      })
      return truncateText(value, MAX_LOG_ARG_CHARS)
    })
    .join(' '))
}

function rotateLogs(filePath: string): void {
  try {
    const stat = fs.statSync(filePath)
    if (stat.size < MAX_LOG_FILE_BYTES) return
  } catch {
    return
  }

  for (let index = MAX_LOG_BACKUPS; index >= 1; index -= 1) {
    const source = `${filePath}.${index}`
    const dest = `${filePath}.${index + 1}`
    try {
      if (index === MAX_LOG_BACKUPS) {
        fs.rmSync(source, { force: true })
      } else if (fs.existsSync(source)) {
        fs.renameSync(source, dest)
      }
    } catch {
      // ignore rotation failure to avoid breaking app startup
    }
  }

  try {
    if (fs.existsSync(filePath)) {
      fs.renameSync(filePath, `${filePath}.1`)
    }
  } catch {
    // ignore rotation failure to avoid breaking app startup
  }
}

function appendLine(filePath: string, line: string): void {
  ensureLogDir()
  rotateLogs(filePath)
  fs.appendFileSync(filePath, `${truncateText(line)}\n`, 'utf8')
}

function appendChunk(filePath: string, stream: string, text: string): void {
  if (!text) return
  const lines = text.replace(/\r\n/g, '\n').split('\n').slice(0, MAX_LOG_CHUNK_LINES)
  for (const line of lines) {
    if (!line) continue
    appendLine(filePath, `[${now()}] [${stream}] ${line}`)
  }
  if (lines.length === MAX_LOG_CHUNK_LINES) {
    appendLine(filePath, `[${now()}] [${stream}] … [truncated additional lines]`)
  }
}

export function getDesktopLogDir(): string {
  ensureLogDir()
  return LOG_DIR
}

export function getDesktopLogPaths(): { main: string; sidecar: string } {
  ensureLogDir()
  return {
    main: MAIN_LOG_PATH,
    sidecar: SIDECAR_LOG_PATH,
  }
}

export function setupMainProcessLogging(): void {
  if (initialized) return
  initialized = true

  appendLine(
    MAIN_LOG_PATH,
    `[${now()}] [session] desktop main start pid=${process.pid} platform=${process.platform} arch=${process.arch}`,
  )

  for (const level of ['log', 'info', 'warn', 'error'] as const) {
    const original = console[level].bind(console)
    console[level] = (...args: unknown[]) => {
      try {
        appendLine(MAIN_LOG_PATH, `[${now()}] [${level}] ${stringifyArgs(args)}`)
      } catch {
        // ignore log write failure to avoid breaking startup
      }
      original(...args)
    }
  }

  process.on('uncaughtException', (error) => {
    try {
      appendLine(MAIN_LOG_PATH, `[${now()}] [uncaughtException] ${error.stack ?? error.message}`)
    } catch {}
  })

  process.on('unhandledRejection', (reason) => {
    try {
      appendLine(MAIN_LOG_PATH, `[${now()}] [unhandledRejection] ${inspect(reason, { depth: 6 })}`)
    } catch {}
  })
}

export function appendSidecarLog(stream: 'stdout' | 'stderr', chunk: Buffer | string): void {
  try {
    appendChunk(SIDECAR_LOG_PATH, stream, typeof chunk === 'string' ? chunk : chunk.toString('utf8'))
  } catch {
    // ignore log write failure to avoid breaking sidecar startup
  }
}
