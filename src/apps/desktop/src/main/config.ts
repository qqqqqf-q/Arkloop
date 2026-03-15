import * as fs from 'fs'
import * as path from 'path'
import * as os from 'os'
import { DEFAULT_CONFIG } from './types'
import type { AppConfig, ConnectionMode, LocalConfig, LocalPortMode } from './types'

const CONFIG_DIR = path.join(os.homedir(), '.arkloop')
const CONFIG_PATH = path.join(CONFIG_DIR, 'config.json')

function normalizeConnectionMode(mode: unknown): ConnectionMode {
  return mode === 'saas' || mode === 'self-hosted' || mode === 'local'
    ? mode
    : DEFAULT_CONFIG.mode
}

function normalizePort(port: unknown): number {
  if (typeof port === 'number' && Number.isInteger(port) && port > 0 && port <= 65535) {
    return port
  }
  return DEFAULT_CONFIG.local.port
}

function normalizePortMode(mode: unknown): LocalPortMode {
  return mode === 'manual' ? 'manual' : 'auto'
}

function normalizeLocalConfig(local: unknown): LocalConfig {
  const raw = (local && typeof local === 'object') ? local as Partial<LocalConfig> : {}
  return {
    port: normalizePort(raw.port),
    portMode: normalizePortMode(raw.portMode),
  }
}

export function normalizeConfig(config: Partial<AppConfig> | null | undefined): AppConfig {
  const parsed = config ?? {}
  return {
    mode: normalizeConnectionMode(parsed.mode),
    saas: {
      ...DEFAULT_CONFIG.saas,
      ...(parsed.saas ?? {}),
    },
    selfHosted: {
      ...DEFAULT_CONFIG.selfHosted,
      ...(parsed.selfHosted ?? {}),
    },
    local: normalizeLocalConfig(parsed.local),
    window: {
      ...DEFAULT_CONFIG.window,
      ...(parsed.window ?? {}),
    },
    onboarding_completed: typeof parsed.onboarding_completed === 'boolean'
      ? parsed.onboarding_completed
      : DEFAULT_CONFIG.onboarding_completed,
  }
}

export function loadConfig(): AppConfig {
  try {
    const raw = fs.readFileSync(CONFIG_PATH, 'utf-8')
    const parsed = JSON.parse(raw) as Partial<AppConfig>
    return normalizeConfig(parsed)
  } catch {
    return normalizeConfig(undefined)
  }
}

export function saveConfig(config: AppConfig): void {
  fs.mkdirSync(CONFIG_DIR, { recursive: true })
  fs.writeFileSync(CONFIG_PATH, JSON.stringify(normalizeConfig(config), null, 2), 'utf-8')
}

export function getConfigPath(): string {
  return CONFIG_PATH
}
