import { ChildProcess, spawn } from 'child_process'
import { randomBytes } from 'crypto'
import * as fs from 'fs'
import * as http from 'http'
import * as https from 'https'
import * as net from 'net'
import * as os from 'os'
import * as path from 'path'
import { app } from 'electron'
import type { LocalPortMode } from './types'

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
const MAX_RESTARTS = 3
const MAX_AUTO_PORT_RETRIES = 6
const AUTO_PORT_SCAN_WINDOW = 20
const SIDECAR_DIR = path.join(os.homedir(), '.arkloop', 'bin')
const VERSION_FILE = path.join(os.homedir(), '.arkloop', 'bin', 'sidecar.version.json')
const DEFAULT_DOWNLOAD_BASE = 'https://github.com/nicepkg/arkloop/releases/download'
const desktopAccessToken = `arkloop-desktop-${randomBytes(24).toString('hex')}`

let proc: ChildProcess | null = null
let restartCount = 0
let stopping = false
let statusListener: ((status: SidecarStatus) => void) | null = null
let runtimeListener: ((runtime: SidecarRuntime) => void) | null = null
let runtime: SidecarRuntime = {
  status: 'stopped',
  port: null,
  portMode: 'auto',
}

export function getSidecarStatus(): SidecarStatus {
  return runtime.status
}

export function getSidecarRuntime(): SidecarRuntime {
  return { ...runtime }
}

export function getDesktopAccessToken(): string {
  return desktopAccessToken
}

export function setStatusListener(fn: (status: SidecarStatus) => void): void {
  statusListener = fn
}

export function setRuntimeListener(fn: (runtime: SidecarRuntime) => void): void {
  runtimeListener = fn
}

function setRuntime(patch: Partial<SidecarRuntime>): void {
  runtime = { ...runtime, ...patch }
  statusListener?.(runtime.status)
  runtimeListener?.({ ...runtime })
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

export function isSidecarAvailable(): boolean {
  try {
    fs.accessSync(getSidecarPath(), fs.constants.X_OK)
    return true
  } catch {}

  if (!app.isPackaged) {
    const devPath = path.resolve(
      __dirname, '..', '..', '..', '..', 'services', 'desktop', 'bin', 'desktop',
    )
    try {
      fs.accessSync(devPath, fs.constants.X_OK)
      return true
    } catch {}
  }

  if (app.isPackaged) {
    const bundledName = process.platform === 'win32' ? 'desktop.exe' : 'desktop'
    const bundledPath = path.join(process.resourcesPath, 'sidecar', bundledName)
    try {
      fs.accessSync(bundledPath, fs.constants.X_OK)
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
    const res = await httpsGet('https://api.github.com/repos/nicepkg/arkloop/releases/latest')
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
    const releaseRes = await httpsGet('https://api.github.com/repos/nicepkg/arkloop/releases/latest')
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

function resolveBinaryPath(): string {
  const downloaded = getSidecarPath()
  if (fs.existsSync(downloaded)) return downloaded

  if (!app.isPackaged) {
    const devPath = path.resolve(
      __dirname, '..', '..', '..', '..', 'services', 'desktop', 'bin', 'desktop',
    )
    if (fs.existsSync(devPath)) return devPath
  }

  const bundledName = process.platform === 'win32' ? 'desktop.exe' : 'desktop'
  return path.join(process.resourcesPath, 'sidecar', bundledName)
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

async function reserveEphemeralPort(): Promise<number> {
  return await new Promise<number>((resolve, reject) => {
    const server = net.createServer()
    server.once('error', reject)
    server.listen(0, '127.0.0.1', () => {
      const address = server.address()
      if (!address || typeof address === 'string') {
        server.close(() => reject(new Error('failed to allocate local port')))
        return
      }
      const port = address.port
      server.close((error) => {
        if (error) {
          reject(error)
          return
        }
        resolve(port)
      })
    })
  })
}

async function resolveLaunchPort(preferredPort: number, portMode: LocalPortMode): Promise<number> {
  if (portMode === 'manual') {
    const available = await isTcpPortAvailable(preferredPort)
    if (!available) throw setPortConflictError(preferredPort)
    return preferredPort
  }

  const start = Math.max(1, preferredPort)
  for (let offset = 0; offset < AUTO_PORT_SCAN_WINDOW; offset++) {
    const candidate = start + offset
    if (candidate > 65535) break
    if (await isTcpPortAvailable(candidate)) return candidate
  }

  return await reserveEphemeralPort()
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
      ARKLOOP_OUTBOUND_TRUST_FAKE_IP: process.env.ARKLOOP_OUTBOUND_TRUST_FAKE_IP ?? 'true',
    },
    stdio: ['ignore', 'pipe', 'pipe'],
  })
  proc = child

  const handleOutput = (stream: 'stdout' | 'stderr') => (chunk: Buffer) => {
    const text = chunk.toString()
    recentOutput = `${recentOutput}${text}`.slice(-4000)
    process[stream].write(`[sidecar] ${text}`)
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
  return getSidecarRuntime()
}

export async function startSidecar(preferredPort: number, portMode: LocalPortMode = 'auto'): Promise<SidecarRuntime> {
  if (proc) return getSidecarRuntime()

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
