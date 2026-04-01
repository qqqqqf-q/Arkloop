import * as fs from 'fs'
import * as https from 'https'
import * as http from 'http'
import * as os from 'os'
import * as path from 'path'

export type ComponentStatus = {
  current: string | null
  latest: string | null
  available: boolean
}

export type UpdateStatus = {
  openviking: ComponentStatus
  sandbox: { kernel: ComponentStatus; rootfs: ComponentStatus }
  bins: { rtk: ComponentStatus; opencli: ComponentStatus }
}

type BinSpec = { version: string; repo: string }

type DesktopManifest = {
  version: string
  openviking: { image: string; version: string }
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
  const openviking = manifest.openviking as Record<string, unknown> | undefined
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
    openviking: {
      image: assertNonEmptyString(openviking?.image, 'openviking.image'),
      version: assertNonEmptyString(openviking?.version, 'openviking.version'),
    },
    ...(parsedSandbox.kernel || parsedSandbox.rootfs ? { sandbox: parsedSandbox } : {}),
    ...parseBins(manifest.bins),
  }
}

type LocalVersions = {
  openviking?: { version: string; updated_at: string }
  opencli?: { version: string; updated_at: string }
  rtk?: { version: string; updated_at: string }
  sandbox?: {
    kernel?: { version: string; updated_at: string }
    rootfs?: { version: string; updated_at: string }
  }
}

const GITHUB_REPO = 'qqqqqf/Arkloop'
const GITHUB_API_LATEST_RELEASE = `https://api.github.com/repos/${GITHUB_REPO}/releases/latest`
const VERSIONS_FILE = path.join(os.homedir(), '.arkloop', 'versions.json')
const VM_DIR = path.join(os.homedir(), '.arkloop', 'vm')
const OPENCLI_VERSION_FILE_LEGACY = path.join(os.homedir(), '.arkloop', 'bin', 'opencli.version.json')

function isReleaseMissingStatus(statusCode: number | undefined): boolean {
  return statusCode === 404
}

export function loadLocalVersions(): LocalVersions {
  try {
    const raw = fs.readFileSync(VERSIONS_FILE, 'utf-8')
    return JSON.parse(raw) as LocalVersions
  } catch (err) {
    if (err instanceof Error && (err as NodeJS.ErrnoException).code === 'ENOENT') return {}
    throw err
  }
}

export function saveLocalVersions(v: LocalVersions): void {
  fs.mkdirSync(path.dirname(VERSIONS_FILE), { recursive: true })
  fs.writeFileSync(VERSIONS_FILE, JSON.stringify(v, null, 2), 'utf-8')
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
  const releaseRes = await httpsGet(GITHUB_API_LATEST_RELEASE)
  const releaseBody = await new Promise<string>((resolve, reject) => {
    const chunks: Buffer[] = []
    releaseRes.on('data', (c: Buffer) => chunks.push(c))
    releaseRes.on('end', () => resolve(Buffer.concat(chunks).toString()))
    releaseRes.on('error', reject)
  })
  if (releaseRes.statusCode !== 200) {
    if (isReleaseMissingStatus(releaseRes.statusCode)) {
      throw new Error('no release published')
    }
    throw new Error(`failed to fetch release info: ${releaseRes.statusCode}`)
  }

  const release = JSON.parse(releaseBody) as { tag_name?: string; assets?: Array<{ name: string; browser_download_url: string }> }
  const assets = release.assets ?? []
  const manifestAsset = assets.find((a) => a.name === 'desktop-manifest.json')
  if (!manifestAsset) {
    throw new Error('desktop-manifest.json not found in release assets')
  }

  const manifestRes = await httpsGet(manifestAsset.browser_download_url)
  const manifestBody = await new Promise<string>((resolve, reject) => {
    const chunks: Buffer[] = []
    manifestRes.on('data', (c: Buffer) => chunks.push(c))
    manifestRes.on('end', () => resolve(Buffer.concat(chunks).toString()))
    manifestRes.on('error', reject)
  })
  if (manifestRes.statusCode !== 200) {
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

export async function checkForUpdates(): Promise<UpdateStatus> {
  const manifest = await fetchManifest()
  const local = loadLocalVersions()

  const ovCurrent = local.openviking?.version ?? null
  const kernelCurrent = local.sandbox?.kernel?.version ?? null
  const rootfsCurrent = local.sandbox?.rootfs?.version ?? null
  const kernelLatest = manifest.sandbox?.kernel?.version ?? null
  const rootfsLatest = manifest.sandbox?.rootfs?.version ?? null
  const rtkCurrent = local.rtk?.version ?? null
  const rtkLatest = manifest.bins?.rtk?.version ?? null
  const opencliCurrent = resolveLocalOpenCLIVersion(local)
  const opencliLatest = manifest.bins?.opencli?.version ?? null

  return {
    openviking: {
      current: ovCurrent,
      latest: manifest.openviking.version,
      available: !!(manifest.openviking.version && manifest.openviking.version !== ovCurrent),
    },
    sandbox: {
      kernel: {
        current: kernelCurrent,
        latest: kernelLatest,
        available: !!(kernelLatest && kernelLatest !== kernelCurrent),
      },
      rootfs: {
        current: rootfsCurrent,
        latest: rootfsLatest,
        available: !!(rootfsLatest && rootfsLatest !== rootfsCurrent),
      },
    },
    bins: {
      rtk: {
        current: rtkCurrent,
        latest: rtkLatest,
        available: !!(rtkLatest && rtkLatest !== rtkCurrent),
      },
      opencli: {
        current: opencliCurrent,
        latest: opencliLatest,
        available: !!(opencliLatest && opencliLatest !== opencliCurrent),
      },
    },
  }
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
  component: 'openviking' | 'sandbox_kernel' | 'sandbox_rootfs' | 'rtk' | 'opencli',
  onProgress?: (p: DownloadProgress) => void,
): Promise<void> {
  const manifest = await fetchManifest()
  const now = new Date().toISOString()

  if (component === 'openviking') {
    const { getBridgeBaseUrl, waitForBridgeOperation } = await import('./sidecar')
    const baseUrl = getBridgeBaseUrl()
    const body = JSON.stringify({ image: manifest.openviking.image })

    const operationId = await new Promise<string>((resolve, reject) => {
      const url = new URL(`${baseUrl}/v1/modules/openviking/upgrade`)
      const req = http.request(
        url,
        {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            'Content-Length': Buffer.byteLength(body, 'utf-8'),
          },
        },
        (res) => {
          let data = ''
          res.on('data', (c) => { data += c })
          res.on('end', () => {
            if (res.statusCode !== 202) {
              reject(new Error(`upgrade request failed: ${res.statusCode}`))
              return
            }
            try {
              const j = JSON.parse(data) as { operation_id?: string }
              if (!j.operation_id) {
                reject(new Error('no operation_id in response'))
                return
              }
              resolve(j.operation_id)
            } catch {
              reject(new Error('invalid response from bridge'))
            }
          })
        },
      )
      req.on('error', reject)
      req.setTimeout(15_000, () => {
        req.destroy()
        reject(new Error('upgrade request timeout'))
      })
      req.write(body)
      req.end()
    })

    const result = await waitForBridgeOperation(operationId, 600_000)
    if (!result.ok) {
      throw new Error(`openviking upgrade failed: ${result.error ?? 'unknown'}`)
    }

    const local = loadLocalVersions()
    saveLocalVersions({
      ...local,
      openviking: { version: manifest.openviking.version, updated_at: now },
    })
    return
  }

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
    const rtkDest = path.join(rtkDir, 'rtk')
    fs.mkdirSync(rtkDir, { recursive: true })
    const scriptUrl = `https://raw.githubusercontent.com/${spec.repo}/refs/tags/v${spec.version}/install.sh`
    await downloadAndInstallRTK(scriptUrl, rtkDest, onProgress)
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
  const { execFileSync } = await import('child_process')
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
