import { ChildProcess, execFileSync, spawn } from 'child_process'
import { randomBytes } from 'crypto'
import * as fs from 'fs'
import * as http from 'http'
import * as https from 'https'
import * as net from 'net'
import * as os from 'os'
import * as path from 'path'
import { app } from 'electron'
import type { LocalPortMode, MemoryConfig, NetworkConfig } from './types'
import { appendSidecarLog, getDesktopLogPaths } from './logging'

export type SidecarStatus = 'stopped' | 'starting' | 'running' | 'crashed'

export type SidecarRuntime = {
  status: SidecarStatus
  port: number | null
  portMode: LocalPortMode
  lastError?: string
}

export type DownloadProgress = {
  phase: 'connecting' | 'downloading' | 'verifying' | 'done' | 'error'
  percent: number
  bytesDownloaded: number
  bytesTotal: number
  error?: string
}

export type SidecarStartErrorCode =
  | 'binary_missing'
  | 'port_in_use'
  | 'health_timeout'
  | 'launch_failed'

export class SidecarStartError extends Error {
  readonly code: SidecarStartErrorCode

  constructor(code: SidecarStartErrorCode, message: string) {
    super(message)
    this.name = 'SidecarStartError'
    this.code = code
  }
}

const HEALTH_POLL_MS = 500
const HEALTH_TIMEOUT_MS = 30_000
const BRIDGE_READY_POLL_MS = 400
const BRIDGE_READY_ATTEMPTS = 40
const OPENVIKING_INSTALL_WAIT_MS = 600_000
const OPENVIKING_START_WAIT_MS = 180_000
const OPENVIKING_STOP_WAIT_MS = 120_000
const MODULE_ACTION_RETRIES = 3
const MODULE_ACTION_RETRY_MS = 2000
const MAX_RESTARTS = 3
const MAX_AUTO_PORT_RETRIES = 6
const AUTO_PORT_SCAN_WINDOW = 20
const DEFAULT_BRIDGE_PORT = 19003
const ARKLOOP_HOME = path.join(os.homedir(), '.arkloop')
const SIDECAR_DIR = path.join(os.homedir(), '.arkloop', 'bin')
const VERSION_FILE = path.join(os.homedir(), '.arkloop', 'bin', 'sidecar.version.json')
const PACKAGED_PROJECT_DIR = path.join(ARKLOOP_HOME, 'project')
const PACKAGED_PROJECT_STATE_FILE = path.join(PACKAGED_PROJECT_DIR, '.bundle-state.json')
const PACKAGED_PROJECT_PRESERVED_FILES = new Set([
  path.join('config', 'openviking', 'ov.conf'),
])
const DEFAULT_GITHUB_REPO = 'qqqqqf-q/Arkloop'
const DEFAULT_DOWNLOAD_BASE = `https://github.com/${DEFAULT_GITHUB_REPO}/releases/download`
const GITHUB_API_LATEST_RELEASE = `https://api.github.com/repos/${DEFAULT_GITHUB_REPO}/releases/latest`
const desktopAccessToken = `arkloop-desktop-${randomBytes(24).toString('hex')}`
const DESKTOP_TOKEN_FILE = path.join(ARKLOOP_HOME, 'desktop.token')

let proc: ChildProcess | null = null
let restartCount = 0
let stopping = false
let statusListener: ((status: SidecarStatus) => void) | null = null
let runtimeListener: ((runtime: SidecarRuntime) => void) | null = null
let bridgeUrlListener: ((bridgeBaseUrl: string) => void) | null = null
let runtime: SidecarRuntime = {
  status: 'stopped',
  port: null,
  portMode: 'auto',
}
let bridgeBaseUrl = `http://127.0.0.1:${DEFAULT_BRIDGE_PORT}`
let memoryConfig: MemoryConfig | null = null
let networkConfig: NetworkConfig | null = null

export function getSidecarStatus(): SidecarStatus {
  return runtime.status
}

export function setMemoryConfig(config: MemoryConfig): void {
  memoryConfig = config
}

export function setNetworkConfig(config: NetworkConfig): void {
  networkConfig = config
}

export function getSidecarRuntime(): SidecarRuntime {
  return { ...runtime }
}

export function getDesktopAccessToken(): string {
  return desktopAccessToken
}

export function getBridgeBaseUrl(): string {
  return bridgeBaseUrl
}

export function setStatusListener(fn: (status: SidecarStatus) => void): void {
  statusListener = fn
}

export function setRuntimeListener(fn: (runtime: SidecarRuntime) => void): void {
  runtimeListener = fn
}

export function setBridgeUrlListener(fn: (bridgeBaseUrl: string) => void): void {
  bridgeUrlListener = fn
}

function setRuntime(patch: Partial<SidecarRuntime>): void {
  runtime = { ...runtime, ...patch }
  statusListener?.(runtime.status)
  runtimeListener?.({ ...runtime })
}

function setBridgeBaseUrl(nextBridgeBaseUrl: string): void {
  bridgeBaseUrl = nextBridgeBaseUrl
  bridgeUrlListener?.(bridgeBaseUrl)
}

function getSidecarBinaryName(): string {
  const platform = process.platform
  const arch = process.arch === 'arm64' ? 'arm64' : 'x64'
  const name = `desktop-${platform}-${arch}`
  return platform === 'win32' ? `${name}.exe` : name
}

export function getSidecarPath(): string {
  return path.join(SIDECAR_DIR, getSidecarBinaryName())
}

function getBundledSidecarPath(): string {
  const bundledName = process.platform === 'win32' ? 'desktop.exe' : 'desktop'
  return path.join(process.resourcesPath, 'sidecar', bundledName)
}

function getDevBuiltSidecarPath(): string {
  return path.resolve(
    __dirname,
    '..',
    '..',
    '..',
    '..',
    'services',
    'desktop',
    'bin',
    process.platform === 'win32' ? 'desktop.exe' : 'desktop',
  )
}

function getDevPackagedSidecarPath(): string {
  return path.resolve(
    __dirname,
    '..',
    '..',
    'sidecar-bin',
    getSidecarBinaryName(),
  )
}

function getSidecarBinaryCandidates(): string[] {
  if (app.isPackaged) {
    return [getSidecarPath(), getBundledSidecarPath()]
  }
  return [
    getDevBuiltSidecarPath(),
    getDevPackagedSidecarPath(),
    getSidecarPath(),
    getBundledSidecarPath(),
  ]
}

export function isSidecarAvailable(): boolean {
  for (const candidate of getSidecarBinaryCandidates()) {
    try {
      fs.accessSync(candidate, fs.constants.X_OK)
      return true
    } catch {}
  }
  return false
}

function httpsGet(url: string, maxRedirects = 5): Promise<http.IncomingMessage> {
  return new Promise((resolve, reject) => {
    if (maxRedirects <= 0) {
      reject(new Error('too many redirects'))
      return
    }
    https.get(url, { headers: { 'User-Agent': 'arkloop-desktop' } }, (res) => {
      if (res.statusCode && res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
        res.resume()
        httpsGet(res.headers.location, maxRedirects - 1).then(resolve, reject)
        return
      }
      resolve(res)
    }).on('error', reject)
  })
}

export async function checkSidecarVersion(): Promise<{
  current: string | null
  latest: string | null
  updateAvailable: boolean
}> {
  let current: string | null = null
  try {
    const raw = fs.readFileSync(VERSION_FILE, 'utf-8')
    current = JSON.parse(raw).version ?? null
  } catch {}

  let latest: string | null = null
  try {
    const res = await httpsGet(GITHUB_API_LATEST_RELEASE)
    const body = await new Promise<string>((resolve, reject) => {
      const chunks: Buffer[] = []
      res.on('data', (c: Buffer) => chunks.push(c))
      res.on('end', () => resolve(Buffer.concat(chunks).toString()))
      res.on('error', reject)
    })
    if (res.statusCode === 200) {
      const data = JSON.parse(body)
      latest = data.tag_name?.replace(/^v/, '') ?? null
    }
  } catch {
    return { current, latest: null, updateAvailable: false }
  }

  return {
    current,
    latest,
    updateAvailable: !!(latest && latest !== current),
  }
}

export async function downloadSidecar(
  onProgress?: (progress: DownloadProgress) => void,
): Promise<void> {
  const emit = (progress: DownloadProgress) => onProgress?.(progress)
  const tmpPath = `${getSidecarPath()}.tmp`

  emit({ phase: 'connecting', percent: 0, bytesDownloaded: 0, bytesTotal: 0 })

  try {
    const releaseRes = await httpsGet(GITHUB_API_LATEST_RELEASE)
    const releaseBody = await new Promise<string>((resolve, reject) => {
      const chunks: Buffer[] = []
      releaseRes.on('data', (chunk: Buffer) => chunks.push(chunk))
      releaseRes.on('end', () => resolve(Buffer.concat(chunks).toString()))
      releaseRes.on('error', reject)
    })
    if (releaseRes.statusCode !== 200) {
      throw new Error(`failed to fetch release info: ${releaseRes.statusCode}`)
    }

    const release = JSON.parse(releaseBody)
    const version = (release.tag_name as string)?.replace(/^v/, '')
    if (!version) throw new Error('invalid release: missing tag_name')

    const downloadBase = process.env.ARKLOOP_SIDECAR_DOWNLOAD_URL || DEFAULT_DOWNLOAD_BASE
    const binaryName = getSidecarBinaryName()
    const url = `${downloadBase}/v${version}/${binaryName}`

    fs.mkdirSync(SIDECAR_DIR, { recursive: true })

    const dlRes = await httpsGet(url)
    if (dlRes.statusCode !== 200) {
      dlRes.resume()
      throw new Error(`download failed: ${dlRes.statusCode}`)
    }

    const bytesTotal = parseInt(dlRes.headers['content-length'] || '0', 10)
    let bytesDownloaded = 0

    emit({ phase: 'downloading', percent: 0, bytesDownloaded: 0, bytesTotal })

    const ws = fs.createWriteStream(tmpPath)
    await new Promise<void>((resolve, reject) => {
      dlRes.on('data', (chunk: Buffer) => {
        bytesDownloaded += chunk.length
        const percent = bytesTotal > 0 ? Math.round((bytesDownloaded / bytesTotal) * 100) : 0
        emit({ phase: 'downloading', percent, bytesDownloaded, bytesTotal })
      })
      dlRes.pipe(ws)
      ws.on('finish', resolve)
      ws.on('error', reject)
      dlRes.on('error', reject)
    })

    emit({ phase: 'verifying', percent: 100, bytesDownloaded, bytesTotal })

    if (process.platform !== 'win32') {
      fs.chmodSync(tmpPath, 0o755)
    }

    fs.renameSync(tmpPath, getSidecarPath())
    fs.writeFileSync(VERSION_FILE, JSON.stringify({
      version,
      downloadedAt: new Date().toISOString(),
    }))

    emit({ phase: 'done', percent: 100, bytesDownloaded, bytesTotal })
  } catch (error) {
    try { fs.unlinkSync(tmpPath) } catch {}
    const message = error instanceof Error ? error.message : String(error)
    emit({ phase: 'error', percent: 0, bytesDownloaded: 0, bytesTotal: 0, error: message })
    throw error
  }
}

const OPENCLI_GITHUB_API = 'https://api.github.com/repos/nashsu/AutoCLI/releases/latest'
const OPENCLI_VERSION_FILE = path.join(os.homedir(), '.arkloop', 'bin', 'opencli.version.json')

function getOpenCLIAssetName(): string {
  const { platform, arch } = process
  if (platform === 'win32') return 'autocli-x86_64-pc-windows-msvc.zip'
  if (platform === 'darwin') return arch === 'arm64' ? 'autocli-aarch64-apple-darwin.tar.gz' : 'autocli-x86_64-apple-darwin.tar.gz'
  if (platform === 'linux') return arch === 'arm64' ? 'autocli-aarch64-unknown-linux-musl.tar.gz' : 'autocli-x86_64-unknown-linux-musl.tar.gz'
  throw new Error(`unsupported platform: ${platform}`)
}

function getOpenCLIDestPath(): string {
  return path.join(SIDECAR_DIR, process.platform === 'win32' ? 'autocli.exe' : 'autocli')
}

function getOpenCLIArchivePath(destPath: string): string {
  if (process.platform === 'win32') return `${destPath}.archive.zip`
  return `${destPath}.archive.tmp`
}

export async function downloadOpenCLI(onProgress?: (progress: DownloadProgress) => void, targetVersion?: string): Promise<void> {
  const emit = (progress: DownloadProgress) => onProgress?.(progress)
  const destPath = getOpenCLIDestPath()
  const tmpArchive = getOpenCLIArchivePath(destPath)
  const extractDir = `${destPath}.extract.tmp`

  emit({ phase: 'connecting', percent: 0, bytesDownloaded: 0, bytesTotal: 0 })

  try {
    let version: string
    let downloadUrl: string

    if (targetVersion) {
      version = targetVersion
      const assetName = getOpenCLIAssetName()
      downloadUrl = `https://github.com/nashsu/AutoCLI/releases/download/v${version}/${assetName}`
    } else {
      const releaseRes = await httpsGet(OPENCLI_GITHUB_API)
      const releaseBody = await new Promise<string>((resolve, reject) => {
        const chunks: Buffer[] = []
        releaseRes.on('data', (chunk: Buffer) => chunks.push(chunk))
        releaseRes.on('end', () => resolve(Buffer.concat(chunks).toString()))
        releaseRes.on('error', reject)
      })
      if (releaseRes.statusCode !== 200) {
        throw new Error(`failed to fetch autocli release info: ${releaseRes.statusCode}`)
      }

      const release = JSON.parse(releaseBody) as {
        tag_name?: string
        assets?: Array<{ name: string; browser_download_url: string }>
      }
      version = (release.tag_name as string)?.replace(/^v/, '')
      if (!version) throw new Error('invalid autocli release: missing tag_name')

      const assetName = getOpenCLIAssetName()
      const asset = (release.assets ?? []).find((a) => a.name === assetName)
      if (!asset) throw new Error(`autocli asset not found: ${assetName}`)
      downloadUrl = asset.browser_download_url
    }

    fs.mkdirSync(SIDECAR_DIR, { recursive: true })

    const dlRes = await httpsGet(downloadUrl)
    if (dlRes.statusCode !== 200) {
      dlRes.resume()
      throw new Error(`autocli download failed: ${dlRes.statusCode}`)
    }

    const bytesTotal = parseInt(dlRes.headers['content-length'] || '0', 10)
    let bytesDownloaded = 0

    emit({ phase: 'downloading', percent: 0, bytesDownloaded: 0, bytesTotal })

    const ws = fs.createWriteStream(tmpArchive)
    await new Promise<void>((resolve, reject) => {
      dlRes.on('data', (chunk: Buffer) => {
        bytesDownloaded += chunk.length
        const percent = bytesTotal > 0 ? Math.round((bytesDownloaded / bytesTotal) * 100) : 0
        emit({ phase: 'downloading', percent, bytesDownloaded, bytesTotal })
      })
      dlRes.pipe(ws)
      ws.on('finish', resolve)
      ws.on('error', reject)
      dlRes.on('error', reject)
    })

    emit({ phase: 'verifying', percent: 100, bytesDownloaded, bytesTotal })

    fs.mkdirSync(extractDir, { recursive: true })
    try {
      if (process.platform === 'win32') {
        execFileSync('powershell', [
          '-Command',
          `Expand-Archive -Force -Path '${tmpArchive}' -DestinationPath '${extractDir}'`,
        ])
      } else {
        execFileSync('tar', ['xzf', tmpArchive, '-C', extractDir])
      }

      const binaryName = process.platform === 'win32' ? 'autocli.exe' : 'autocli'
      const extractedBin = path.join(extractDir, binaryName)
      if (!fs.existsSync(extractedBin)) {
        throw new Error(`extracted binary not found: ${extractedBin}`)
      }

      if (process.platform !== 'win32') {
        fs.chmodSync(extractedBin, 0o755)
      }
      fs.renameSync(extractedBin, destPath)
    } finally {
      try { fs.rmSync(extractDir, { recursive: true, force: true }) } catch {}
    }

    fs.unlinkSync(tmpArchive)
    fs.writeFileSync(OPENCLI_VERSION_FILE, JSON.stringify({
      version,
      downloadedAt: new Date().toISOString(),
    }))

    emit({ phase: 'done', percent: 100, bytesDownloaded, bytesTotal })
  } catch (error) {
    try { fs.unlinkSync(tmpArchive) } catch {}
    try { fs.rmSync(extractDir, { recursive: true, force: true }) } catch {}
    const message = error instanceof Error ? error.message : String(error)
    emit({ phase: 'error', percent: 0, bytesDownloaded: 0, bytesTotal: 0, error: message })
    throw error
  }
}

export async function ensureOpenCLI(): Promise<void> {
  try {
    const destPath = getOpenCLIDestPath()
    if (!fs.existsSync(destPath)) {
      await downloadOpenCLI()
    }
  } catch (error) {
    console.error('[sidecar] autocli ensure failed:', error instanceof Error ? error.message : String(error))
  }
}

function resolveBinaryPath(): string {
  for (const candidate of getSidecarBinaryCandidates()) {
    if (fs.existsSync(candidate)) return candidate
  }
  return getBundledSidecarPath()
}

function resolveBundledProjectDir(): string | null {
  if (!app.isPackaged) return null
  const candidate = path.join(process.resourcesPath, 'arkloop-project')
  if (!fs.existsSync(path.join(candidate, 'compose.yaml'))) return null
  return ensureWritableBundledProjectDir(candidate)
}

type PackagedProjectState = {
  version?: string
}

function readPackagedProjectState(): PackagedProjectState | null {
  try {
    return JSON.parse(fs.readFileSync(PACKAGED_PROJECT_STATE_FILE, 'utf-8')) as PackagedProjectState
  } catch {
    return null
  }
}

function syncPackagedProjectTree(sourceDir: string, destDir: string, relDir = ''): void {
  fs.mkdirSync(destDir, { recursive: true })
  const entries = fs.readdirSync(sourceDir, { withFileTypes: true })
  for (const entry of entries) {
    const relPath = relDir ? path.join(relDir, entry.name) : entry.name
    const sourcePath = path.join(sourceDir, entry.name)
    const destPath = path.join(destDir, entry.name)

    if (entry.isDirectory()) {
      syncPackagedProjectTree(sourcePath, destPath, relPath)
      continue
    }

    if (PACKAGED_PROJECT_PRESERVED_FILES.has(relPath) && fs.existsSync(destPath)) {
      continue
    }

    fs.mkdirSync(path.dirname(destPath), { recursive: true })
    fs.copyFileSync(sourcePath, destPath)
  }
}

function ensureWritableBundledProjectDir(sourceDir: string): string {
  const currentVersion = app.getVersion()
  const currentState = readPackagedProjectState()
  if (currentState?.version === currentVersion && isBridgeProjectDir(PACKAGED_PROJECT_DIR)) {
    return PACKAGED_PROJECT_DIR
  }

  syncPackagedProjectTree(sourceDir, PACKAGED_PROJECT_DIR)
  fs.writeFileSync(
    PACKAGED_PROJECT_STATE_FILE,
    JSON.stringify({ version: currentVersion, syncedAt: new Date().toISOString() }),
    'utf-8',
  )
  return PACKAGED_PROJECT_DIR
}

function readEnvVar(name: string): string | null {
  const value = process.env[name]
  if (typeof value !== 'string') return null
  const trimmed = value.trim()
  return trimmed || null
}

function pathIsDirectory(target: string): boolean {
  try {
    return fs.statSync(target).isDirectory()
  } catch {
    return false
  }
}

function pathIsFile(target: string): boolean {
  try {
    return fs.statSync(target).isFile()
  } catch {
    return false
  }
}

function isProjectDir(candidate: string): boolean {
  return pathIsFile(path.join(candidate, 'compose.yaml')) && pathIsDirectory(path.join(candidate, 'src'))
}

// Bridge 只依赖 compose 工程根；不必有 monorepo 的 src/
function isBridgeProjectDir(candidate: string): boolean {
  return pathIsFile(path.join(candidate, 'compose.yaml'))
    || pathIsFile(path.join(candidate, 'docker-compose.yml'))
}

function resolveDevProjectDir(): string | null {
  const candidates = [
    // dist/main -> repo root
    path.resolve(__dirname, '..', '..', '..', '..', '..'),
    // dist/main -> src
    path.resolve(__dirname, '..', '..', '..', '..'),
    process.cwd(),
  ]
  for (const candidate of candidates) {
    if (isProjectDir(candidate)) return candidate
  }
  return null
}

function discoverProjectDirFromDocker(): string | null {
  try {
    const out = execFileSync('docker', ['compose', 'ls', '-a', '--format', 'json'], {
      encoding: 'utf-8',
      timeout: 8000,
      maxBuffer: 4 * 1024 * 1024,
    })
    const trimmed = out.trim()
    if (!trimmed) return null

    type ComposeLsRow = { Name?: string; ConfigFiles?: string }
    const rows: ComposeLsRow[] = []
    try {
      const parsed = JSON.parse(trimmed) as unknown
      if (Array.isArray(parsed)) {
        for (const item of parsed) {
          if (item && typeof item === 'object') rows.push(item as ComposeLsRow)
        }
      } else if (parsed && typeof parsed === 'object') {
        rows.push(parsed as ComposeLsRow)
      }
    } catch {
      for (const line of trimmed.split('\n')) {
        if (!line.trim()) continue
        try {
          rows.push(JSON.parse(line) as ComposeLsRow)
        } catch {
          // skip bad line
        }
      }
    }

    const candidates: string[] = []
    for (const row of rows) {
      const files = row.ConfigFiles
      if (typeof files !== 'string' || !files) continue
      const first = files.split(',')[0]?.trim()
      if (!first) continue
      const dir = path.dirname(first)
      if (!isBridgeProjectDir(dir)) continue
      candidates.push(dir)
    }

    const unique = [...new Set(candidates)]
    for (const c of unique) {
      if (isProjectDir(c)) return c
    }
    for (const c of unique) {
      if (pathIsFile(path.join(c, 'install', 'modules.yaml'))) return c
    }
    return unique[0] ?? null
  } catch {
    return null
  }
}

function resolveProjectDir(): string | null {
  const explicit = readEnvVar('ARKLOOP_PROJECT_DIR')
  if (explicit) {
    if (isProjectDir(explicit) || isBridgeProjectDir(explicit)) {
      return explicit
    }
  }
  const bundled = resolveBundledProjectDir()
  if (bundled) return bundled
  const dev = resolveDevProjectDir()
  if (dev) return dev
  return discoverProjectDirFromDocker()
}

function buildRuntimeResourceEnv(projectDir: string | null): Record<string, string> {
  const env: Record<string, string> = {}
  if (!projectDir) return env

  if (!readEnvVar('ARKLOOP_PROJECT_DIR') && isProjectDir(projectDir)) {
    env.ARKLOOP_PROJECT_DIR = projectDir
  }

  const personasRoot = path.join(projectDir, 'src', 'personas')
  if (!readEnvVar('ARKLOOP_PERSONAS_ROOT') && pathIsDirectory(personasRoot)) {
    env.ARKLOOP_PERSONAS_ROOT = personasRoot
  }

  const skillsRoot = path.join(projectDir, 'src', 'skills')
  if (!readEnvVar('ARKLOOP_SKILLS_ROOT') && pathIsDirectory(skillsRoot)) {
    env.ARKLOOP_SKILLS_ROOT = skillsRoot
  }

  return env
}

function defaultWorkspaceRoot(projectDir: string | null): string {
  if (!app.isPackaged && projectDir && isProjectDir(projectDir)) {
    return projectDir
  }
  return os.homedir()
}

function buildWorkspaceEnv(projectDir: string | null): Record<string, string> {
  const env: Record<string, string> = {}
  const workspaceRoot = defaultWorkspaceRoot(projectDir)
  if (!readEnvVar('ARKLOOP_LOCAL_SHELL_WORKSPACE')) {
    env.ARKLOOP_LOCAL_SHELL_WORKSPACE = workspaceRoot
  }
  if (!readEnvVar('ARKLOOP_WORKING_DIR')) {
    env.ARKLOOP_WORKING_DIR = workspaceRoot
  }
  return env
}

function buildMemoryEnv(projectDir: string | null): Record<string, string> {
  const env: Record<string, string> = {}
  const cfg = memoryConfig
  if (!cfg) return env

  env.ARKLOOP_MEMORY_ENABLED = cfg.enabled ? 'true' : 'false'
  env.ARKLOOP_MEMORY_COMMIT_EACH_TURN = cfg.memoryCommitEachTurn === false ? 'false' : 'true'
  env.ARKLOOP_MEMORY_PROVIDER = cfg.provider
  if (cfg.enabled && cfg.provider === 'nowledge') {
    if (cfg.nowledge?.baseUrl) {
      env.ARKLOOP_NOWLEDGE_BASE_URL = cfg.nowledge.baseUrl
    }
    if (cfg.nowledge?.apiKey) {
      env.ARKLOOP_NOWLEDGE_API_KEY = cfg.nowledge.apiKey
    }
    if (cfg.nowledge?.requestTimeoutMs) {
      env.ARKLOOP_NOWLEDGE_REQUEST_TIMEOUT_MS = String(cfg.nowledge.requestTimeoutMs)
    }
  } else if (cfg.enabled && cfg.provider === 'openviking') {
    env.ARKLOOP_OPENVIKING_BASE_URL = 'http://127.0.0.1:19010'
    if (cfg.openviking?.rootApiKey) {
      env.ARKLOOP_OPENVIKING_ROOT_API_KEY = cfg.openviking.rootApiKey
    } else {
      // Fallback: read root_api_key directly from ov.conf (source of truth).
      const ovConfPath = projectDir
        ? path.join(projectDir, 'config', 'openviking', 'ov.conf')
        : null
      if (ovConfPath) {
        try {
          const ovData = JSON.parse(fs.readFileSync(ovConfPath, 'utf-8'))
          if (ovData?.server?.root_api_key) {
            env.ARKLOOP_OPENVIKING_ROOT_API_KEY = String(ovData.server.root_api_key)
          }
        } catch {
          // ov.conf not present or unreadable; env var will be used as last resort.
        }
      }
    }
  }
  return env
}

function buildNetworkEnv(): Record<string, string> {
  const env: Record<string, string> = {}
  const cfg = networkConfig
  if (!cfg) return env

  if (cfg.proxyEnabled && cfg.proxyUrl?.trim()) {
    env.ARKLOOP_OUTBOUND_PROXY_URL = cfg.proxyUrl.trim()
  }
  if (typeof cfg.requestTimeoutMs === 'number' && cfg.requestTimeoutMs > 0) {
    env.ARKLOOP_OUTBOUND_TIMEOUT_MS = String(cfg.requestTimeoutMs)
  }
  if (typeof cfg.retryCount === 'number' && cfg.retryCount >= 0) {
    env.ARKLOOP_OUTBOUND_RETRY_COUNT = String(cfg.retryCount)
  }
  if (cfg.userAgent?.trim()) {
    env.ARKLOOP_OUTBOUND_USER_AGENT = cfg.userAgent.trim()
  }
  return env
}

function buildBridgeEnv(bridgePort: number, projectDir: string | null): Record<string, string> {
  const env: Record<string, string> = {
    ARKLOOP_BRIDGE_ADDR: `127.0.0.1:${bridgePort}`,
    ARKLOOP_BRIDGE_AUTH_TOKEN: desktopAccessToken,
  }

  const devUrl = process.env.VITE_DEV_URL?.trim()
  if (devUrl) {
    try {
      env.ARKLOOP_BRIDGE_CORS_ORIGINS = new URL(devUrl).origin
    } catch {
      // Ignore malformed dev URLs and fall back to the bridge defaults.
    }
  }

  if (!projectDir) return env

  if (isBridgeProjectDir(projectDir)) {
    env.ARKLOOP_BRIDGE_PROJECT_DIR = projectDir
  }
  const modulesFile = path.join(projectDir, 'install', 'modules.yaml')
  if (pathIsFile(modulesFile)) {
    env.ARKLOOP_BRIDGE_MODULES_FILE = modulesFile
  }

  // Keep packaged defaults unchanged to avoid affecting dev-mode local stacks.
  if (!app.isPackaged) return env

  env.ARKLOOP_POSTGRES_USER = process.env.ARKLOOP_POSTGRES_USER ?? 'arkloop'
  env.ARKLOOP_POSTGRES_PASSWORD = process.env.ARKLOOP_POSTGRES_PASSWORD ?? 'arkloop_desktop'
  env.ARKLOOP_POSTGRES_DB = process.env.ARKLOOP_POSTGRES_DB ?? 'arkloop'
  env.ARKLOOP_REDIS_PASSWORD = process.env.ARKLOOP_REDIS_PASSWORD ?? 'arkloop_redis'
  env.ARKLOOP_S3_ACCESS_KEY = process.env.ARKLOOP_S3_ACCESS_KEY ?? 'arkloop'
  env.ARKLOOP_S3_SECRET_KEY = process.env.ARKLOOP_S3_SECRET_KEY ?? 'arkloop_s3'

  return env
}


async function resolveBridgePort(apiPort: number): Promise<number> {
  // Keep bridge and API ports disjoint even when the allocator falls back to
  // ephemeral ports under heavy local port contention.
  return await resolveLaunchPort(DEFAULT_BRIDGE_PORT, 'auto', {
    excludePorts: [apiPort],
  })
}

function healthCheck(port: number): Promise<boolean> {
  return new Promise((resolve) => {
    const req = http.get(`http://127.0.0.1:${port}/healthz`, (res) => {
      resolve(res.statusCode === 200)
    })
    req.on('error', () => resolve(false))
    req.setTimeout(2000, () => {
      req.destroy()
      resolve(false)
    })
  })
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

/** Worker 内 title_summarizer 调试；未设置时在非打包开发构建下默认开启。 */
function desktopTitleDebugFlag(): string {
  const raw = process.env.ARKLOOP_DESKTOP_TITLE_DEBUG
  if (typeof raw === 'string') {
    const v = raw.trim().toLowerCase()
    if (v === '0' || v === 'false' || v === 'off' || v === 'no') return '0'
    if (v === '1' || v === 'true' || v === 'on' || v === 'yes') return '1'
  }
  return !app.isPackaged ? '1' : '0'
}

function desktopOsUsername(): string {
  try {
    return os.userInfo().username.trim()
  } catch {
    return os.hostname().trim()
  }
}

export type BridgeModuleRow = {
  id: string
  status: string
  version?: string
}

async function waitForBridgeReady(): Promise<boolean> {
  const base = bridgeBaseUrl
  for (let i = 0; i < BRIDGE_READY_ATTEMPTS; i++) {
    const ok = await new Promise<boolean>((resolve) => {
      const req = http.get(`${base}/healthz`, (res) => {
        resolve(res.statusCode === 200)
      })
      req.on('error', () => resolve(false))
      req.setTimeout(1500, () => {
        req.destroy()
        resolve(false)
      })
    })
    if (ok) return true
    await sleep(BRIDGE_READY_POLL_MS)
  }
  return false
}

async function bridgeGetJson<T>(urlPath: string): Promise<T | null> {
  const base = bridgeBaseUrl
  return new Promise((resolve) => {
    const req = http.get(`${base}${urlPath}`, {
      headers: {
        Authorization: `Bearer ${desktopAccessToken}`,
      },
    }, (res) => {
      let data = ''
      res.on('data', (c) => { data += c })
      res.on('end', () => {
        if (res.statusCode !== 200) {
          resolve(null)
          return
        }
        try {
          resolve(JSON.parse(data) as T)
        } catch {
          resolve(null)
        }
      })
    })
    req.on('error', () => resolve(null))
    req.setTimeout(10_000, () => {
      req.destroy()
      resolve(null)
    })
  })
}

async function bridgePostModuleAction(
  moduleId: string,
  action: string,
): Promise<{ operationId: string | null; statusCode: number }> {
  const base = bridgeBaseUrl
  const body = JSON.stringify({ action })
  return new Promise((resolve) => {
    const url = new URL(`${base}/v1/modules/${encodeURIComponent(moduleId)}/actions`)
    const req = http.request(
      url,
      {
        method: 'POST',
        headers: {
          Authorization: `Bearer ${desktopAccessToken}`,
          'Content-Type': 'application/json',
          'Content-Length': Buffer.byteLength(body, 'utf8'),
        },
      },
      (res) => {
        let data = ''
        res.on('data', (c) => { data += c })
        res.on('end', () => {
          if (res.statusCode !== 202) {
            resolve({ operationId: null, statusCode: res.statusCode ?? 0 })
            return
          }
          try {
            const j = JSON.parse(data) as { operation_id?: string }
            resolve({ operationId: j.operation_id ?? null, statusCode: 202 })
          } catch {
            resolve({ operationId: null, statusCode: 202 })
          }
        })
      },
    )
    req.on('error', () => resolve({ operationId: null, statusCode: 0 }))
    req.setTimeout(15_000, () => {
      req.destroy()
      resolve({ operationId: null, statusCode: 0 })
    })
    req.write(body)
    req.end()
  })
}

async function bridgePostActionWithRetry(moduleId: string, action: string): Promise<string | null> {
  for (let attempt = 0; attempt < MODULE_ACTION_RETRIES; attempt++) {
    const { operationId, statusCode } = await bridgePostModuleAction(moduleId, action)
    if (operationId) return operationId
    if (statusCode === 409 && attempt + 1 < MODULE_ACTION_RETRIES) {
      await sleep(MODULE_ACTION_RETRY_MS)
      continue
    }
    return null
  }
  return null
}

export async function waitForBridgeOperation(
  operationId: string,
  timeoutMs: number,
): Promise<{ ok: boolean; error?: string }> {
  const base = bridgeBaseUrl
  return new Promise((resolve) => {
    const url = new URL(`/v1/operations/${encodeURIComponent(operationId)}/stream`, base)
    const req = http.get(url, {
      headers: {
        Authorization: `Bearer ${desktopAccessToken}`,
      },
    }, (res) => {
      let buf = ''
      const timer = setTimeout(() => {
        req.destroy()
        resolve({ ok: false, error: 'timeout' })
      }, timeoutMs)

      const done = (result: { ok: boolean; error?: string }) => {
        clearTimeout(timer)
        req.destroy()
        resolve(result)
      }

      res.on('data', (chunk: Buffer) => {
        buf += chunk.toString()
        let scanFrom = 0
        for (;;) {
          const ev = buf.indexOf('event: status', scanFrom)
          if (ev < 0) break
          const dataLabel = buf.indexOf('data:', ev)
          if (dataLabel < 0) break
          const lineEnd = buf.indexOf('\n', dataLabel)
          if (lineEnd < 0) break
          const jsonStr = buf.slice(dataLabel + 5, lineEnd).trim()
          scanFrom = lineEnd + 1
          try {
            const j = JSON.parse(jsonStr) as { status: string; error?: string }
            if (j.status === 'completed') {
              done({ ok: true })
              return
            }
            if (j.status === 'failed') {
              done({ ok: false, error: j.error })
              return
            }
          } catch {
            // keep scanning
          }
        }
        if (buf.length > 512 * 1024) {
          buf = buf.slice(-256 * 1024)
        }
      })

      res.on('end', () => {
        clearTimeout(timer)
        resolve({ ok: false, error: 'stream_closed' })
      })
    })
    req.on('error', () => resolve({ ok: false, error: 'request' }))
  })
}

export async function bridgeListModules(): Promise<BridgeModuleRow[] | null> {
  return await bridgeGetJson<BridgeModuleRow[]>('/v1/modules')
}

async function maybeEnsureOpenVikingRunning(): Promise<void> {
  const cfg = memoryConfig
  if (!cfg?.enabled || cfg.provider !== 'openviking') return

  const ready = await waitForBridgeReady()
  if (!ready) {
    console.error('[sidecar] bridge health timeout (openviking autostart skipped)')
    return
  }

  let list = await bridgeListModules()
  if (!list) return
  let mod = list.find((m) => m.id === 'openviking')
  if (!mod) return

  if (mod.status === 'not_installed') {
    const opId = await bridgePostActionWithRetry('openviking', 'install')
    if (!opId) {
      console.error('[sidecar] openviking install request failed')
      return
    }
    const inst = await waitForBridgeOperation(opId, OPENVIKING_INSTALL_WAIT_MS)
    if (!inst.ok) {
      console.error('[sidecar] openviking install:', inst.error ?? 'failed')
      return
    }
    list = await bridgeListModules()
    mod = list?.find((m) => m.id === 'openviking')
  }

  if (mod?.status === 'running') return

  if (mod?.status === 'stopped' || mod?.status === 'error') {
    const startOp = await bridgePostActionWithRetry('openviking', 'start')
    if (!startOp) {
      console.error('[sidecar] openviking start request failed')
      return
    }
    const st = await waitForBridgeOperation(startOp, OPENVIKING_START_WAIT_MS)
    if (!st.ok) {
      console.error('[sidecar] openviking start:', st.error ?? 'failed')
    }
  }
}

export async function stopBridgeOpenvikingIfNeeded(memory: MemoryConfig): Promise<void> {
  if (!memory.enabled || memory.provider !== 'openviking') return

  const ready = await waitForBridgeReady()
  if (!ready) return

  const list = await bridgeListModules()
  const mod = list?.find((m) => m.id === 'openviking')
  if (!mod || mod.status !== 'running') return

  const opId = await bridgePostActionWithRetry('openviking', 'stop')
  if (!opId) return
  await waitForBridgeOperation(opId, OPENVIKING_STOP_WAIT_MS)
}

function isPortConflictText(text: string, sidecarPort: number): boolean {
  const normalized = text.toLowerCase()
  const hasConflictKeyword = normalized.includes('address already in use')
    || normalized.includes('bind address already in use')
    || normalized.includes('eaddrinuse')
  if (!hasConflictKeyword) return false
  // Only treat as sidecar port conflict if the message references the sidecar's
  // own port. Other components (e.g., bridge) may log bind failures for their
  // own ports which should not abort the sidecar launch.
  return normalized.includes(`:${sidecarPort}`)
}

function isPortInUseError(error: unknown): error is SidecarStartError {
  return error instanceof SidecarStartError && error.code === 'port_in_use'
}

function describeLaunchFailure(port: number, code: number | null, recentOutput: string): string {
  const excerpt = recentOutput.trim()
  if (excerpt) {
    return `Sidecar failed to start on 127.0.0.1:${port}: ${excerpt.slice(-240)}`
  }
  return `Sidecar exited before becoming healthy on 127.0.0.1:${port} (code=${code ?? 'unknown'}).`
}

function setPortConflictError(port: number): SidecarStartError {
  return new SidecarStartError('port_in_use', `Local port ${port} is already in use.`)
}

function canRetryPortConflict(mode: LocalPortMode, attempt: number): boolean {
  return mode === 'auto' && attempt < MAX_AUTO_PORT_RETRIES - 1
}

async function isTcpPortAvailable(port: number): Promise<boolean> {
  return await new Promise<boolean>((resolve) => {
    const server = net.createServer()

    const finish = (available: boolean) => {
      try {
        server.close(() => resolve(available))
      } catch {
        resolve(available)
      }
    }

    server.once('error', (error: NodeJS.ErrnoException) => {
      if (error.code === 'EADDRINUSE' || error.code === 'EACCES') {
        resolve(false)
        return
      }
      resolve(false)
    })

    server.once('listening', () => finish(true))
    server.listen(port, '127.0.0.1')
  })
}

function normalizeExcludedPorts(excludePorts?: Iterable<number>): Set<number> {
  const excluded = new Set<number>()
  if (!excludePorts) return excluded
  for (const port of excludePorts) {
    if (Number.isInteger(port) && port > 0 && port <= 65535) {
      excluded.add(port)
    }
  }
  return excluded
}

async function reserveEphemeralPort(excludePorts?: Iterable<number>): Promise<number> {
  const excluded = normalizeExcludedPorts(excludePorts)
  const maxAttempts = Math.max(8, excluded.size + 2)

  for (let attempt = 0; attempt < maxAttempts; attempt++) {
    const port = await new Promise<number>((resolve, reject) => {
      const server = net.createServer()
      server.once('error', reject)
      server.listen(0, '127.0.0.1', () => {
        const address = server.address()
        if (!address || typeof address === 'string') {
          server.close(() => reject(new Error('failed to allocate local port')))
          return
        }
        const nextPort = address.port
        server.close((error) => {
          if (error) {
            reject(error)
            return
          }
          resolve(nextPort)
        })
      })
    })

    if (!excluded.has(port)) {
      return port
    }
  }

  throw new Error('failed to allocate local port')
}

async function resolveLaunchPort(
  preferredPort: number,
  portMode: LocalPortMode,
  options?: { excludePorts?: Iterable<number> },
): Promise<number> {
  const excluded = normalizeExcludedPorts(options?.excludePorts)

  if (portMode === 'manual') {
    if (excluded.has(preferredPort)) throw setPortConflictError(preferredPort)
    const available = await isTcpPortAvailable(preferredPort)
    if (!available) throw setPortConflictError(preferredPort)
    return preferredPort
  }

  const start = Math.max(1, preferredPort)
  for (let offset = 0; offset < AUTO_PORT_SCAN_WINDOW; offset++) {
    const candidate = start + offset
    if (candidate > 65535) break
    if (excluded.has(candidate)) continue
    if (await isTcpPortAvailable(candidate)) return candidate
  }

  return await reserveEphemeralPort(excluded)
}

async function terminateChildProcess(child: ChildProcess): Promise<void> {
  await new Promise<void>((resolve) => {
    let resolved = false
    let killTimer: NodeJS.Timeout | null = null

    const finish = () => {
      if (resolved) return
      resolved = true
      if (killTimer) clearTimeout(killTimer)
      child.removeListener('exit', finish)
      resolve()
    }

    child.once('exit', finish)
    if (child.exitCode !== null || child.signalCode !== null) {
      finish()
      return
    }

    killTimer = setTimeout(() => {
      try { child.kill('SIGKILL') } catch {}
      finish()
    }, 5000)

    try {
      child.kill('SIGTERM')
    } catch {
      finish()
    }
  })
}

async function launchOnPort(port: number, portMode: LocalPortMode): Promise<SidecarRuntime> {
  const binPath = resolveBinaryPath()
  if (!fs.existsSync(binPath)) {
    const error = new SidecarStartError('binary_missing', 'sidecar binary not found, call downloadSidecar() first')
    setRuntime({ status: 'crashed', port, portMode, lastError: error.message })
    throw error
  }

  stopping = false
  let launchError: Error | null = null
  let recentOutput = ''
  let healthy = false
  const bridgePort = await resolveBridgePort(port)
  const projectDir = resolveProjectDir()
  setBridgeBaseUrl(`http://127.0.0.1:${bridgePort}`)
  console.info('[sidecar] launch request', {
    binPath,
    port,
    bridgePort,
    projectDir,
    logPath: getDesktopLogPaths().sidecar,
    packaged: app.isPackaged,
  })

  setRuntime({
    status: 'starting',
    port,
    portMode,
    lastError: undefined,
  })

  const child = spawn(binPath, [], {
    env: {
      ...process.env,
      ARKLOOP_API_GO_ADDR: `127.0.0.1:${port}`,
      ARKLOOP_DESKTOP_TOKEN: desktopAccessToken,
      ARKLOOP_DESKTOP_OS_USERNAME: desktopOsUsername(),
      ARKLOOP_OUTBOUND_TRUST_FAKE_IP: process.env.ARKLOOP_OUTBOUND_TRUST_FAKE_IP ?? 'true',
      ...buildRuntimeResourceEnv(projectDir),
      ...buildWorkspaceEnv(projectDir),
      ...buildBridgeEnv(bridgePort, projectDir),
      ...buildMemoryEnv(projectDir),
      ...buildNetworkEnv(),
      ARKLOOP_DESKTOP_TITLE_DEBUG: desktopTitleDebugFlag(),
    },
    stdio: ['ignore', 'pipe', 'pipe'],
  })
  proc = child

  const handleOutput = (stream: 'stdout' | 'stderr') => (chunk: Buffer) => {
    const text = chunk.toString()
    recentOutput = `${recentOutput}${text}`.slice(-4000)
    appendSidecarLog(stream, text)
    process[stream].write(text)
    if (isPortConflictText(text, port)) {
      launchError = setPortConflictError(port)
    }
  }

  child.stdout?.on('data', handleOutput('stdout'))
  child.stderr?.on('data', handleOutput('stderr'))
  child.on('error', (error) => {
    launchError = new SidecarStartError('launch_failed', error.message)
  })

  const exitPromise = new Promise<never>((_, reject) => {
    child.once('exit', (code) => {
      proc = null
      if (stopping || runtime.status === 'stopped') {
        reject(new SidecarStartError('launch_failed', 'sidecar stopped during launch'))
        return
      }

      const error = launchError
        ?? new SidecarStartError('launch_failed', describeLaunchFailure(port, code, recentOutput))

      if (!healthy) {
        setRuntime({ status: 'crashed', port, portMode, lastError: error.message })
        reject(error)
        return
      }

      console.error(`sidecar exited: code=${code}`)
      setRuntime({ status: 'crashed', port, portMode, lastError: error.message })
      if (restartCount < MAX_RESTARTS) {
        restartCount++
        setTimeout(() => {
          void startSidecar(port, portMode).catch(() => {})
        }, 1000)
      }
      reject(error)
    })
  })
  void exitPromise.catch(() => {})

  const healthyPromise = (async () => {
    const deadline = Date.now() + HEALTH_TIMEOUT_MS
    while (Date.now() < deadline) {
      if (launchError) throw launchError
      if (await healthCheck(port)) return
      await sleep(HEALTH_POLL_MS)
    }

    throw launchError
      ?? new SidecarStartError('health_timeout', `Sidecar did not become healthy on 127.0.0.1:${port}.`)
  })()

  try {
    await Promise.race([exitPromise, healthyPromise])
  } catch (error) {
    if (!healthy) {
      await terminateChildProcess(child)
      if (proc === child) {
        proc = null
      }
    }
    throw error
  }

  healthy = true
  restartCount = 0
  setRuntime({ status: 'running', port, portMode, lastError: undefined })
  await maybeEnsureOpenVikingRunning()
  return getSidecarRuntime()
}

export async function startSidecar(preferredPort: number, portMode: LocalPortMode = 'auto'): Promise<SidecarRuntime> {
  if (proc) return getSidecarRuntime()

  try {
    fs.mkdirSync(path.dirname(DESKTOP_TOKEN_FILE), { recursive: true })
    fs.writeFileSync(DESKTOP_TOKEN_FILE, desktopAccessToken, { mode: 0o600 })
  } catch {
    // best-effort
  }

  let nextPreferredPort = preferredPort

  for (let attempt = 0; attempt < MAX_AUTO_PORT_RETRIES; attempt++) {
    try {
      const port = await resolveLaunchPort(nextPreferredPort, portMode)
      return await launchOnPort(port, portMode)
    } catch (error) {
      if (error instanceof SidecarStartError) {
        setRuntime({
          status: 'crashed',
          port: preferredPort,
          portMode,
          lastError: error.message,
        })
      }
      if (canRetryPortConflict(portMode, attempt) && isPortInUseError(error)) {
        nextPreferredPort += 1
        continue
      }
      throw error
    }
  }

  const error = new SidecarStartError('port_in_use', 'Unable to allocate an available local port for the sidecar.')
  setRuntime({ status: 'crashed', port: preferredPort, portMode, lastError: error.message })
  throw error
}

export function stopSidecar(): Promise<void> {
  return new Promise((resolve) => {
    stopping = true
    restartCount = 0
    removeTokenFile()

    if (!proc) {
      setRuntime({ status: 'stopped', lastError: undefined })
      stopping = false
      resolve()
      return
    }

    setRuntime({ status: 'stopped', lastError: undefined })
    const child = proc
    proc = null

    const killTimer = setTimeout(() => {
      try { child.kill('SIGKILL') } catch {}
      stopping = false
      resolve()
    }, 5000)

    child.once('exit', () => {
      clearTimeout(killTimer)
      stopping = false
      resolve()
    })

    try { child.kill('SIGTERM') } catch {}
  })
}

function removeTokenFile(): void {
  try { fs.unlinkSync(DESKTOP_TOKEN_FILE) } catch {}
}
