import * as fs from 'fs'
import * as https from 'https'
import * as http from 'http'
import * as os from 'os'
import * as path from 'path'
import { execFileSync } from 'child_process'
import { loadConfig, loadVersionsFile, saveVersionsFile, type VersionsState } from './config'

export type ComponentStatus = {
  current: string | null
  latest: string | null
  available: boolean
}

export type UpdateStatus = {
  sandbox: { kernel: ComponentStatus; rootfs: ComponentStatus }
  bins: { rtk: ComponentStatus; opencli: ComponentStatus }
}

type BinSpec = { version: string; repo: string }

type DesktopManifest = {
  version: string
  sandbox?: {
    kernel?: { version: string; filename: string }
    rootfs?: { version: string; filename: string }
  }
  bins?: {
    rtk?: BinSpec
    opencli?: BinSpec
  }
}

function assertNonEmptyString(value: unknown, field: string): string {
  if (typeof value !== 'string' || !value.trim()) {
    throw new Error(`invalid desktop manifest: ${field}`)
  }
  return value
}

function parseBinSpec(raw: unknown, field: string): BinSpec | undefined {
  if (!raw || typeof raw !== 'object') return undefined
  const obj = raw as Record<string, unknown>
  if (typeof obj.version !== 'string' || !obj.version.trim()) return undefined
  if (typeof obj.repo !== 'string' || !obj.repo.trim()) return undefined
  return { version: obj.version, repo: obj.repo }
}

function parseBins(raw: unknown): Pick<DesktopManifest, 'bins'> {
  if (!raw || typeof raw !== 'object') return {}
  const obj = raw as Record<string, unknown>
  const rtk = parseBinSpec(obj.rtk, 'bins.rtk')
  const opencli = parseBinSpec(obj.opencli, 'bins.opencli')
  if (!rtk && !opencli) return {}
  return { bins: { ...(rtk ? { rtk } : {}), ...(opencli ? { opencli } : {}) } }
}

function parseDesktopManifest(raw: unknown): DesktopManifest {
  if (!raw || typeof raw !== 'object') {
    throw new Error('invalid desktop manifest: root object')
  }

  const manifest = raw as Record<string, unknown>
  const sandbox = manifest.sandbox as Record<string, unknown> | undefined
  const kernel = sandbox?.kernel as Record<string, unknown> | undefined
  const rootfs = sandbox?.rootfs as Record<string, unknown> | undefined
  const parsedSandbox: DesktopManifest['sandbox'] = {}

  if (kernel?.version != null || kernel?.filename != null) {
    parsedSandbox.kernel = {
      version: assertNonEmptyString(kernel?.version, 'sandbox.kernel.version'),
      filename: assertNonEmptyString(kernel?.filename, 'sandbox.kernel.filename'),
    }
  }

  if (rootfs?.version != null || rootfs?.filename != null) {
    parsedSandbox.rootfs = {
      version: assertNonEmptyString(rootfs?.version, 'sandbox.rootfs.version'),
      filename: assertNonEmptyString(rootfs?.filename, 'sandbox.rootfs.filename'),
    }
  }

  return {
    version: assertNonEmptyString(manifest.version, 'version'),
    ...(parsedSandbox.kernel || parsedSandbox.rootfs ? { sandbox: parsedSandbox } : {}),
    ...parseBins(manifest.bins),
  }
}

type LocalVersions = VersionsState

const GITHUB_REPO = 'qqqqqf-q/Arkloop'
const GITHUB_LATEST_MANIFEST_URL = `https://github.com/${GITHUB_REPO}/releases/latest/download/desktop-manifest.json`
const VM_DIR = path.join(os.homedir(), '.arkloop', 'vm')
const OPENCLI_VERSION_FILE_LEGACY = path.join(os.homedir(), '.arkloop', 'bin', 'opencli.version.json')

function rtkBinaryName(): string {
  return process.platform === 'win32' ? 'rtk.exe' : 'rtk'
}

function rtkWindowsAssetName(): string {
  return 'rtk-x86_64-pc-windows-msvc.zip'
}

function isReleaseMissingStatus(statusCode: number | undefined): boolean {
  return statusCode === 404
}

function isReleaseTemporaryFailureStatus(statusCode: number | undefined): boolean {
  return statusCode === 403 || statusCode === 429
}

export function loadLocalVersions(): LocalVersions {
  return loadVersionsFile()
}

export function saveLocalVersions(v: LocalVersions): void {
  saveVersionsFile(v)
}

function buildComponentStatus(current: string | null, latest: string | null): ComponentStatus {
  return {
    current,
    latest,
    available: !!(current && latest && latest !== current),
  }
}

export function getCachedUpdateStatus(): UpdateStatus {
  const local = loadLocalVersions()
  const cache = local.update_check

  const kernelCurrent = normalizeComponentVersion(local.sandbox?.kernel?.version ?? null)
  const rootfsCurrent = normalizeComponentVersion(local.sandbox?.rootfs?.version ?? null)
  const rtkCurrent = normalizeComponentVersion(local.rtk?.version ?? null)
  const opencliCurrent = normalizeComponentVersion(resolveLocalOpenCLIVersion(local))

  const kernelLatest = normalizeComponentVersion(cache?.sandbox_kernel ?? null)
  const rootfsLatest = normalizeComponentVersion(cache?.sandbox_rootfs ?? null)
  const rtkLatest = normalizeComponentVersion(cache?.rtk ?? null)
  const opencliLatest = normalizeComponentVersion(cache?.opencli ?? null)

  return {
    sandbox: {
      kernel: buildComponentStatus(kernelCurrent, kernelLatest),
      rootfs: buildComponentStatus(rootfsCurrent, rootfsLatest),
    },
    bins: {
      rtk: buildComponentStatus(rtkCurrent, rtkLatest),
      opencli: buildComponentStatus(opencliCurrent, opencliLatest),
    },
  }
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

async function fetchManifest(): Promise<DesktopManifest> {
  const manifestRes = await httpsGet(GITHUB_LATEST_MANIFEST_URL)
  const manifestBody = await new Promise<string>((resolve, reject) => {
    const chunks: Buffer[] = []
    manifestRes.on('data', (c: Buffer) => chunks.push(c))
    manifestRes.on('end', () => resolve(Buffer.concat(chunks).toString()))
    manifestRes.on('error', reject)
  })
  if (manifestRes.statusCode !== 200) {
    if (isReleaseMissingStatus(manifestRes.statusCode)) {
      throw new Error('no release published')
    }
    if (isReleaseTemporaryFailureStatus(manifestRes.statusCode)) {
      throw new Error(`release info temporarily unavailable: ${manifestRes.statusCode}`)
    }
    throw new Error(`failed to fetch manifest: ${manifestRes.statusCode}`)
  }

  return parseDesktopManifest(JSON.parse(manifestBody))
}

function resolveLocalOpenCLIVersion(local: LocalVersions): string | null {
  if (local.opencli?.version) return local.opencli.version
  try {
    const raw = fs.readFileSync(OPENCLI_VERSION_FILE_LEGACY, 'utf-8')
    return (JSON.parse(raw) as { version?: string }).version ?? null
  } catch {
    return null
  }
}

function extractVersionToken(text: string): string | null {
  const match = text.match(/\b(?:v)?(\d+(?:\.\d+)+(?:[-+._A-Za-z0-9]*)?)\b/)
  return match?.[1] ?? null
}

function normalizeComponentVersion(value: string | null | undefined): string | null {
  if (typeof value !== 'string') return null
  const trimmed = value.trim()
  if (!trimmed) return null
  const token = extractVersionToken(trimmed)
  if (token && (token === trimmed || `v${token}` === trimmed || `V${token}` === trimmed)) {
    return token
  }
  return trimmed
}

function hasComponentVersionShape(value: string | undefined): boolean {
  if (!value) return false
  return extractVersionToken(value) === value || extractVersionToken(value) === value.replace(/^v/, '')
}

function readCommandVersion(binaryPath: string, args: string[]): string | null {
  if (!fs.existsSync(binaryPath)) return null
  try {
    const output = execFileSync(binaryPath, args, {
      encoding: 'utf-8',
      timeout: 15_000,
    }).trim()
    return output ? extractVersionToken(output) : null
  } catch {
    return null
  }
}

function readSidecarVersion(): { version: string; updated_at: string } | null {
  const filePath = path.join(os.homedir(), '.arkloop', 'bin', 'sidecar.version.json')
  try {
    const raw = fs.readFileSync(filePath, 'utf-8')
    const parsed = JSON.parse(raw) as { version?: string; downloadedAt?: string }
    if (typeof parsed.version !== 'string' || !parsed.version.trim()) return null
    return {
      version: parsed.version.trim(),
      updated_at: parsed.downloadedAt ?? new Date().toISOString(),
    }
  } catch {
    return null
  }
}

function readOpenCLIVersion(): { version: string; updated_at: string } | null {
  const binaryPath = path.join(os.homedir(), '.arkloop', 'bin', process.platform === 'win32' ? 'autocli.exe' : 'autocli')
  const detectedVersion = readCommandVersion(binaryPath, ['--version'])
  if (detectedVersion) {
    const updatedAt = fs.existsSync(binaryPath)
      ? fs.statSync(binaryPath).mtime.toISOString()
      : new Date().toISOString()
    return { version: detectedVersion, updated_at: updatedAt }
  }
  try {
    const raw = fs.readFileSync(OPENCLI_VERSION_FILE_LEGACY, 'utf-8')
    const parsed = JSON.parse(raw) as { version?: string; downloadedAt?: string }
    if (typeof parsed.version !== 'string' || !parsed.version.trim()) return null
    return {
      version: parsed.version.trim(),
      updated_at: parsed.downloadedAt ?? new Date().toISOString(),
    }
  } catch {
    return null
  }
}

function readRTKVersion(): { version: string; updated_at: string } | null {
  const binaryPath = path.join(os.homedir(), '.arkloop', 'bin', rtkBinaryName())
  const detectedVersion = readCommandVersion(binaryPath, ['--version'])
  if (!detectedVersion) return null
  return {
    version: detectedVersion,
    updated_at: fs.statSync(binaryPath).mtime.toISOString(),
  }
}

function readSandboxVersions(): { kernel?: { version: string; updated_at: string }; rootfs?: { version: string; updated_at: string } } | null {
  if (!fs.existsSync(VM_DIR)) return null
  const candidates: {
    kernel?: { version: string; updated_at: string }
    rootfs?: { version: string; updated_at: string }
  } = {}
  try {
    const files = fs.readdirSync(VM_DIR)
    for (const file of files) {
      const filePath = path.join(VM_DIR, file)
      const stat = fs.statSync(filePath)
      if (!stat.isFile()) continue
      const version = extractVersionToken(file)
      if (!version) continue
      const lower = file.toLowerCase()
      const entry = { version, updated_at: stat.mtime.toISOString() }
      if (lower.includes('kernel')) {
        if (!candidates.kernel || stat.mtime > new Date(candidates.kernel.updated_at)) {
          candidates.kernel = entry
        }
      } else if (lower.includes('rootfs')) {
        if (!candidates.rootfs || stat.mtime > new Date(candidates.rootfs.updated_at)) {
          candidates.rootfs = entry
        }
      }
    }
  } catch {
    return null
  }
  return candidates.kernel || candidates.rootfs ? candidates : null
}

export async function syncLocalVersions(): Promise<LocalVersions> {
  const next = { ...loadLocalVersions() }

  const sidecar = readSidecarVersion()
  if (sidecar) next.sidecar = sidecar

  const opencli = readOpenCLIVersion()
  if (opencli) next.opencli = opencli

  const rtk = readRTKVersion()
  if (rtk) next.rtk = rtk

  if (next.sandbox) {
    const cleanedSandbox = { ...next.sandbox }
    if (cleanedSandbox.kernel && !hasComponentVersionShape(cleanedSandbox.kernel.version)) {
      delete cleanedSandbox.kernel
    }
    if (cleanedSandbox.rootfs && !hasComponentVersionShape(cleanedSandbox.rootfs.version)) {
      delete cleanedSandbox.rootfs
    }
    next.sandbox = (cleanedSandbox.kernel || cleanedSandbox.rootfs) ? cleanedSandbox : undefined
  }

  const detectedSandbox = readSandboxVersions()
  if (detectedSandbox) {
    if (detectedSandbox.kernel && !next.sandbox?.kernel) {
      next.sandbox = { ...next.sandbox, kernel: detectedSandbox.kernel }
    }
    if (detectedSandbox.rootfs && !next.sandbox?.rootfs) {
      next.sandbox = { ...next.sandbox, rootfs: detectedSandbox.rootfs }
    }
  }

  saveLocalVersions(next)
  return next
}

export async function checkForUpdates(): Promise<UpdateStatus> {
  const local = loadLocalVersions()
  const manifest = await fetchManifest()

  const kernelCurrent = normalizeComponentVersion(local.sandbox?.kernel?.version ?? null)
  const rootfsCurrent = normalizeComponentVersion(local.sandbox?.rootfs?.version ?? null)
  const kernelLatest = normalizeComponentVersion(manifest.sandbox?.kernel?.version ?? null)
  const rootfsLatest = normalizeComponentVersion(manifest.sandbox?.rootfs?.version ?? null)
  const rtkCurrent = normalizeComponentVersion(local.rtk?.version ?? null)
  const rtkLatest = normalizeComponentVersion(manifest.bins?.rtk?.version ?? null)
  const opencliCurrent = normalizeComponentVersion(resolveLocalOpenCLIVersion(local))
  const opencliLatest = normalizeComponentVersion(manifest.bins?.opencli?.version ?? null)

  const next: UpdateStatus = {
    sandbox: {
      kernel: buildComponentStatus(kernelCurrent, kernelLatest),
      rootfs: buildComponentStatus(rootfsCurrent, rootfsLatest),
    },
    bins: {
      rtk: buildComponentStatus(rtkCurrent, rtkLatest),
      opencli: buildComponentStatus(opencliCurrent, opencliLatest),
    },
  }

  saveVersionsFile({
    ...local,
    update_check: {
      checked_at: new Date().toISOString(),
      sandbox_kernel: kernelLatest,
      sandbox_rootfs: rootfsLatest,
      rtk: rtkLatest,
      opencli: opencliLatest,
    },
  })

  return next
}

export type DownloadProgress = {
  phase: 'connecting' | 'downloading' | 'verifying' | 'done' | 'error'
  percent: number
  bytesDownloaded: number
  bytesTotal: number
  error?: string
}

async function downloadFile(
  url: string,
  destPath: string,
  onProgress?: (p: DownloadProgress) => void,
): Promise<void> {
  const emit = (p: DownloadProgress) => onProgress?.(p)
  const tmpPath = `${destPath}.tmp`

  emit({ phase: 'connecting', percent: 0, bytesDownloaded: 0, bytesTotal: 0 })

  const res = await httpsGet(url)
  if (res.statusCode !== 200) {
    res.resume()
    throw new Error(`download failed: ${res.statusCode}`)
  }

  const bytesTotal = parseInt(res.headers['content-length'] ?? '0', 10)
  let bytesDownloaded = 0

  emit({ phase: 'downloading', percent: 0, bytesDownloaded: 0, bytesTotal })

  fs.mkdirSync(path.dirname(destPath), { recursive: true })
  const ws = fs.createWriteStream(tmpPath)
  await new Promise<void>((resolve, reject) => {
    res.on('data', (chunk: Buffer) => {
      bytesDownloaded += chunk.length
      const percent = bytesTotal > 0 ? Math.round((bytesDownloaded / bytesTotal) * 100) : 0
      emit({ phase: 'downloading', percent, bytesDownloaded, bytesTotal })
    })
    res.pipe(ws)
    ws.on('finish', resolve)
    ws.on('error', reject)
    res.on('error', reject)
  })

  emit({ phase: 'verifying', percent: 100, bytesDownloaded, bytesTotal })
  fs.renameSync(tmpPath, destPath)
  emit({ phase: 'done', percent: 100, bytesDownloaded, bytesTotal })
}

export async function applyUpdate(
  component: 'sandbox_kernel' | 'sandbox_rootfs' | 'rtk' | 'opencli',
  onProgress?: (p: DownloadProgress) => void,
): Promise<void> {
  const manifest = await fetchManifest()
  const now = new Date().toISOString()

  if (component === 'sandbox_kernel') {
    const sandboxKernel = manifest.sandbox?.kernel
    if (!sandboxKernel) {
      throw new Error('sandbox kernel update not published')
    }
    const { version, filename } = sandboxKernel
    const destPath = path.join(VM_DIR, filename)
    await downloadFile(
      `https://github.com/${GITHUB_REPO}/releases/download/v${version}/${filename}`,
      destPath,
      onProgress,
    )
    const local = loadLocalVersions()
    saveLocalVersions({
      ...local,
      sandbox: {
        ...local.sandbox,
        kernel: { version, updated_at: now },
      },
    })
    return
  }

  if (component === 'sandbox_rootfs') {
    const sandboxRootfs = manifest.sandbox?.rootfs
    if (!sandboxRootfs) {
      throw new Error('sandbox rootfs update not published')
    }
    const { version, filename } = sandboxRootfs
    const destPath = path.join(VM_DIR, filename)
    await downloadFile(
      `https://github.com/${GITHUB_REPO}/releases/download/v${version}/${filename}`,
      destPath,
      onProgress,
    )
    const local = loadLocalVersions()
    saveLocalVersions({
      ...local,
      sandbox: {
        ...local.sandbox,
        rootfs: { version, updated_at: now },
      },
    })
    return
  }

  if (component === 'opencli') {
    const spec = manifest.bins?.opencli
    if (!spec) throw new Error('opencli update not published')
    const { downloadOpenCLI } = await import('./sidecar')
    await downloadOpenCLI(onProgress, spec.version)
    const local = loadLocalVersions()
    saveLocalVersions({ ...local, opencli: { version: spec.version, updated_at: now } })
    return
  }

  if (component === 'rtk') {
    const spec = manifest.bins?.rtk
    if (!spec) throw new Error('rtk update not published')
    const rtkDir = path.join(os.homedir(), '.arkloop', 'bin')
    const rtkDest = path.join(rtkDir, rtkBinaryName())
    fs.mkdirSync(rtkDir, { recursive: true })
    if (process.platform === 'win32') {
      const assetUrl = `https://github.com/${spec.repo}/releases/download/v${spec.version}/${rtkWindowsAssetName()}`
      await downloadAndInstallRTKWindows(assetUrl, rtkDest, onProgress)
    } else {
      const scriptUrl = `https://raw.githubusercontent.com/${spec.repo}/refs/tags/v${spec.version}/install.sh`
      await downloadAndInstallRTK(scriptUrl, rtkDest, onProgress)
    }
    const local = loadLocalVersions()
    saveLocalVersions({ ...local, rtk: { version: spec.version, updated_at: now } })
    return
  }

  throw new Error(`unknown component: ${component}`)
}

async function downloadAndInstallRTK(
  scriptUrl: string,
  destBin: string,
  onProgress?: (p: DownloadProgress) => void,
): Promise<void> {
  const emit = (p: DownloadProgress) => onProgress?.(p)
  emit({ phase: 'connecting', percent: 0, bytesDownloaded: 0, bytesTotal: 0 })

  const res = await httpsGet(scriptUrl)
  if (res.statusCode !== 200) {
    res.resume()
    throw new Error(`rtk install script download failed: ${res.statusCode}`)
  }

  const chunks: Buffer[] = []
  await new Promise<void>((resolve, reject) => {
    res.on('data', (c: Buffer) => chunks.push(c))
    res.on('end', () => resolve())
    res.on('error', reject)
  })
  const script = Buffer.concat(chunks)

  emit({ phase: 'downloading', percent: 50, bytesDownloaded: script.length, bytesTotal: script.length })

  const destDir = path.dirname(destBin)
  const tmpScript = path.join(destDir, 'rtk-install.sh.tmp')
  fs.writeFileSync(tmpScript, script, { mode: 0o755 })

  try {
    execFileSync('sh', [tmpScript], {
      env: { ...process.env, INSTALL_DIR: destDir },
      timeout: 120_000,
    })
    // install script may place binary in ~/.local/bin
    if (!fs.existsSync(destBin)) {
      const home = os.homedir()
      const localBin = path.join(home, '.local', 'bin', 'rtk')
      if (fs.existsSync(localBin)) {
        fs.renameSync(localBin, destBin)
        fs.chmodSync(destBin, 0o755)
      }
    }
    if (!fs.existsSync(destBin)) {
      throw new Error('rtk binary not found after install')
    }
  } finally {
    try { fs.unlinkSync(tmpScript) } catch {}
  }

  emit({ phase: 'done', percent: 100, bytesDownloaded: script.length, bytesTotal: script.length })
}

async function downloadAndInstallRTKWindows(
  assetUrl: string,
  destBin: string,
  onProgress?: (p: DownloadProgress) => void,
): Promise<void> {
  const emit = (p: DownloadProgress) => onProgress?.(p)
  const destDir = path.dirname(destBin)
  fs.mkdirSync(destDir, { recursive: true })

  const archivePath = path.join(destDir, 'rtk.archive.zip')
  await downloadFile(assetUrl, archivePath, onProgress)

  emit({ phase: 'verifying', percent: 100, bytesDownloaded: 0, bytesTotal: 0 })
  try {
    execFileSync(
      'powershell.exe',
      [
        '-NoProfile',
        '-Command',
        `Expand-Archive -LiteralPath '${archivePath.replace(/'/g, "''")}' -DestinationPath '${destDir.replace(/'/g, "''")}' -Force`,
      ],
      { timeout: 120_000 },
    )
    if (!fs.existsSync(destBin)) {
      throw new Error('rtk binary not found after zip extraction')
    }
  } finally {
    try { fs.unlinkSync(archivePath) } catch {}
  }

  emit({ phase: 'done', percent: 100, bytesDownloaded: 100, bytesTotal: 100 })
}
