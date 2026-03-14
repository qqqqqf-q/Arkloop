import { ChildProcess, spawn } from 'child_process'
import * as path from 'path'
import * as http from 'http'
import * as https from 'https'
import * as os from 'os'
import * as fs from 'fs'
import { app } from 'electron'

export type SidecarStatus = 'stopped' | 'starting' | 'running' | 'crashed'

export type DownloadProgress = {
  phase: 'connecting' | 'downloading' | 'verifying' | 'done' | 'error'
  percent: number
  bytesDownloaded: number
  bytesTotal: number
  error?: string
}

const HEALTH_POLL_MS = 500
const HEALTH_TIMEOUT_MS = 30_000
const MAX_RESTARTS = 3
const SIDECAR_DIR = path.join(os.homedir(), '.arkloop', 'bin')
const VERSION_FILE = path.join(os.homedir(), '.arkloop', 'bin', 'sidecar.version.json')
const DEFAULT_DOWNLOAD_BASE = 'https://github.com/nicepkg/arkloop/releases/download'

let proc: ChildProcess | null = null
let status: SidecarStatus = 'stopped'
let restartCount = 0
let onStatusChange: ((s: SidecarStatus) => void) | null = null

export function getSidecarStatus(): SidecarStatus {
  return status
}

export function setStatusListener(fn: (s: SidecarStatus) => void): void {
  onStatusChange = fn
}

function setStatus(s: SidecarStatus): void {
  status = s
  onStatusChange?.(s)
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
  // 优先检查下载目录
  try {
    fs.accessSync(getSidecarPath(), fs.constants.X_OK)
    return true
  } catch {}

  // dev 模式回退
  if (!app.isPackaged) {
    const devPath = path.resolve(
      __dirname, '..', '..', '..', '..', 'services', 'desktop', 'bin', 'desktop',
    )
    try {
      fs.accessSync(devPath, fs.constants.X_OK)
      return true
    } catch {}
  }

  // 打包模式回退
  if (app.isPackaged) {
    const bundledPath = path.join(process.resourcesPath, 'sidecar', 'desktop')
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

  const updateAvailable = !!(latest && latest !== current)
  return { current, latest, updateAvailable }
}

export async function downloadSidecar(
  onProgress?: (progress: DownloadProgress) => void,
): Promise<void> {
  const emit = (p: DownloadProgress) => onProgress?.(p)
  const tmpPath = getSidecarPath() + '.tmp'

  emit({ phase: 'connecting', percent: 0, bytesDownloaded: 0, bytesTotal: 0 })

  try {
    // 获取最新版本
    const releaseRes = await httpsGet('https://api.github.com/repos/nicepkg/arkloop/releases/latest')
    const releaseBody = await new Promise<string>((resolve, reject) => {
      const chunks: Buffer[] = []
      releaseRes.on('data', (c: Buffer) => chunks.push(c))
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

    // 下载二进制
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

    // 设置可执行权限
    if (process.platform !== 'win32') {
      fs.chmodSync(tmpPath, 0o755)
    }

    // 原子替换
    fs.renameSync(tmpPath, getSidecarPath())

    // 写入版本信息
    fs.writeFileSync(VERSION_FILE, JSON.stringify({
      version,
      downloadedAt: new Date().toISOString(),
    }))

    emit({ phase: 'done', percent: 100, bytesDownloaded, bytesTotal })
  } catch (err) {
    try { fs.unlinkSync(tmpPath) } catch {}
    const message = err instanceof Error ? err.message : String(err)
    emit({ phase: 'error', percent: 0, bytesDownloaded: 0, bytesTotal: 0, error: message })
    throw err
  }
}

function resolveBinaryPath(): string {
  // 优先使用下载的二进制
  const downloaded = getSidecarPath()
  if (fs.existsSync(downloaded)) return downloaded

  // dev 模式回退
  if (!app.isPackaged) {
    const devPath = path.resolve(
      __dirname, '..', '..', '..', '..', 'services', 'desktop', 'bin', 'desktop',
    )
    if (fs.existsSync(devPath)) return devPath
  }

  // 打包模式回退
  return path.join(process.resourcesPath, 'sidecar', 'desktop')
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

async function waitForHealthy(port: number): Promise<boolean> {
  const deadline = Date.now() + HEALTH_TIMEOUT_MS
  while (Date.now() < deadline) {
    if (await healthCheck(port)) return true
    await new Promise((r) => setTimeout(r, HEALTH_POLL_MS))
  }
  return false
}

export async function startSidecar(port: number): Promise<void> {
  if (proc) return

  const binPath = resolveBinaryPath()
  if (!fs.existsSync(binPath)) {
    throw new Error('sidecar binary not found, call downloadSidecar() first')
  }
  setStatus('starting')

  proc = spawn(binPath, [], {
    env: {
      ...process.env,
      ARKLOOP_API_GO_ADDR: `127.0.0.1:${port}`,
      // Desktop local commonly runs behind fake-IP DNS/proxy stacks (for example Clash).
      // Trust the RFC2544 fake-IP range by default here so upstream model discovery keeps
      // working, while still allowing an explicit env override back to "false".
      ARKLOOP_OUTBOUND_TRUST_FAKE_IP: process.env.ARKLOOP_OUTBOUND_TRUST_FAKE_IP ?? 'true',
    },
    stdio: ['ignore', 'pipe', 'pipe'],
  })

  proc.stdout?.on('data', (chunk: Buffer) => {
    process.stdout.write(`[sidecar] ${chunk.toString()}`)
  })
  proc.stderr?.on('data', (chunk: Buffer) => {
    process.stderr.write(`[sidecar] ${chunk.toString()}`)
  })
  proc.on('error', () => {})
  process.stdout.on('error', () => {})
  process.stderr.on('error', () => {})

  proc.on('exit', (code) => {
    proc = null
    if (status === 'stopped') return
    console.error(`sidecar exited: code=${code}`)
    if (restartCount < MAX_RESTARTS) {
      restartCount++
      setStatus('crashed')
      setTimeout(() => startSidecar(port), 1000)
    } else {
      setStatus('crashed')
    }
  })

  const ok = await waitForHealthy(port)
  if (ok) {
    restartCount = 0
    setStatus('running')
  } else {
    setStatus('crashed')
    stopSidecar()
  }
}

export function stopSidecar(): Promise<void> {
  return new Promise((resolve) => {
    if (!proc) {
      setStatus('stopped')
      resolve()
      return
    }
    setStatus('stopped')
    const p = proc
    proc = null

    const killTimer = setTimeout(() => {
      try { p.kill('SIGKILL') } catch {}
      resolve()
    }, 5000)

    p.on('exit', () => {
      clearTimeout(killTimer)
      resolve()
    })

    try { p.kill('SIGTERM') } catch {}
  })
}
