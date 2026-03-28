import * as fs from 'fs'
import * as os from 'os'
import * as path from 'path'
import { inspect } from 'util'

const LOG_DIR = path.join(os.homedir(), '.arkloop', 'logs')
const MAIN_LOG_PATH = path.join(LOG_DIR, 'desktop-main.log')
const SIDECAR_LOG_PATH = path.join(LOG_DIR, 'desktop-sidecar.log')

let initialized = false

function ensureLogDir(): void {
  fs.mkdirSync(LOG_DIR, { recursive: true })
}

function now(): string {
  return new Date().toISOString()
}

function stringifyArgs(args: unknown[]): string {
  return args
    .map((arg) => {
      if (typeof arg === 'string') return arg
      return inspect(arg, {
        depth: 6,
        breakLength: 120,
        maxArrayLength: 50,
      })
    })
    .join(' ')
}

function appendLine(filePath: string, line: string): void {
  ensureLogDir()
  fs.appendFileSync(filePath, `${line}\n`, 'utf8')
}

function appendChunk(filePath: string, stream: string, text: string): void {
  if (!text) return
  const lines = text.replace(/\r\n/g, '\n').split('\n')
  for (const line of lines) {
    if (!line) continue
    appendLine(filePath, `[${now()}] [${stream}] ${line}`)
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
