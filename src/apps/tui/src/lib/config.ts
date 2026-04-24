import { readFileSync, existsSync } from "node:fs"
import { join } from "node:path"
import { homedir } from "node:os"

export interface Config {
  host: string
  token: string
  debugSSE: boolean
}

function arkloopDir(): string {
  return join(homedir(), ".arkloop")
}

function resolveHost(flagHost?: string): string {
  if (flagHost) return flagHost

  const envHost = process.env.ARKLOOP_HOST
  if (envHost) return envHost

  // Read ~/.arkloop/config.json
  const configPath = join(arkloopDir(), "config.json")
  if (existsSync(configPath)) {
    try {
      const raw = readFileSync(configPath, "utf-8")
      const config = JSON.parse(raw)
      if (config.mode === "local" && config.local?.port) {
        return `http://127.0.0.1:${config.local.port}`
      }
    } catch {
      // ignore parse errors
    }
  }

  return "http://127.0.0.1:19001"
}

function resolveToken(flagToken?: string): string {
  if (flagToken) return flagToken

  if (process.env.ARKLOOP_TOKEN) return process.env.ARKLOOP_TOKEN
  if (process.env.ARKLOOP_DESKTOP_TOKEN) return process.env.ARKLOOP_DESKTOP_TOKEN

  const tokenPath = join(arkloopDir(), "desktop.token")
  if (existsSync(tokenPath)) {
    try {
      return readFileSync(tokenPath, "utf-8").trim()
    } catch {
      // ignore
    }
  }

  return ""
}

function resolveDebugSSE(flagDebugSSE?: boolean): boolean {
  if (flagDebugSSE) return true

  const raw = process.env.ARKLOOP_DEBUG_SSE
  if (!raw) return false

  const value = raw.trim().toLowerCase()
  return value === "1" || value === "true" || value === "yes"
}

export interface CLIFlags {
  host?: string
  token?: string
  resume?: string
  debugSSE?: boolean
}

export function parseFlags(argv: string[]): CLIFlags {
  const flags: CLIFlags = {}
  for (let i = 0; i < argv.length; i++) {
    if (argv[i] === "--host" && argv[i + 1]) {
      flags.host = argv[++i]
    } else if (argv[i] === "--token" && argv[i + 1]) {
      flags.token = argv[++i]
    } else if (argv[i] === "--resume" && argv[i + 1]) {
      flags.resume = argv[++i]
    } else if (argv[i] === "--debug-sse") {
      flags.debugSSE = true
    }
  }
  return flags
}

export function resolveConfig(flags: CLIFlags): Config {
  return {
    host: resolveHost(flags.host),
    token: resolveToken(flags.token),
    debugSSE: resolveDebugSSE(flags.debugSSE),
  }
}
