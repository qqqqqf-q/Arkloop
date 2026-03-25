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
  sidecar: ComponentStatus
  openviking: ComponentStatus
  opencli?: ComponentStatus
  sandbox: { kernel: ComponentStatus; rootfs: ComponentStatus }
}

type DesktopManifest = {
  version: string
  sidecar: { version: string }
  openviking: { image: string; version: string }
  sandbox: {
    kernel: { version: string; filename: string }
    rootfs: { version: string; filename: string }
  }
}

type LocalVersions = {
  sidecar?: { version: string; updated_at: string }
  openviking?: { version: string; updated_at: string }
  opencli?: { version: string; updated_at: string }
  sandbox?: {
    kernel?: { version: string; updated_at: string }
    rootfs?: { version: string; updated_at: string }
  }
}

const GITHUB_REPO = 'qqqqqf-q/Arkloop'
const GITHUB_API_LATEST_RELEASE = `https://api.github.com/repos/${GITHUB_REPO}/releases/latest`
const VERSIONS_FILE = path.join(os.homedir(), '.arkloop', 'versions.json')
const VM_DIR = path.join(os.homedir(), '.arkloop', 'vm')

// 旧 sidecar 版本文件，用于迁移
const LEGACY_SIDECAR_VERSION_FILE = path.join(os.homedir(), '.arkloop', 'bin', 'sidecar.version.json')

export function loadLocalVersions(): LocalVersions {
  try {
    const raw = fs.readFileSync(VERSIONS_FILE, 'utf-8')
    const v = JSON.parse(raw) as LocalVersions

    // 迁移: 如果 versions.json 中无 sidecar 版本，尝试从旧文件读取
    if (!v.sidecar) {
      try {
        const legacyRaw = fs.readFileSync(LEGACY_SIDECAR_VERSION_FILE, 'utf-8')
        const legacy = JSON.parse(legacyRaw) as { version?: string; downloadedAt?: string }
        if (legacy.version) {
          v.sidecar = { version: legacy.version, updated_at: legacy.downloadedAt ?? new Date().toISOString() }
        }
      } catch {
        // 旧文件不存在则跳过
      }
    }

    return v
  } catch {
    // versions.json 不存在或解析失败时，尝试从旧文件迁移 sidecar 版本
    try {
      const legacyRaw = fs.readFileSync(LEGACY_SIDECAR_VERSION_FILE, 'utf-8')
      const legacy = JSON.parse(legacyRaw) as { version?: string; downloadedAt?: string }
      if (legacy.version) {
        return {
          sidecar: { version: legacy.version, updated_at: legacy.downloadedAt ?? new Date().toISOString() },
        }
      }
    } catch {
      // 无旧文件
    }
    return {}
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

  return JSON.parse(manifestBody) as DesktopManifest
}

const OPENCLI_GITHUB_API = 'https://api.github.com/repos/nashsu/opencli-rs/releases/latest'

async function fetchOpenCLILatestVersion(): Promise<string | null> {
  try {
    const res = await httpsGet(OPENCLI_GITHUB_API)
    const body = await new Promise<string>((resolve, reject) => {
      const chunks: Buffer[] = []
      res.on('data', (c: Buffer) => chunks.push(c))
      res.on('end', () => resolve(Buffer.concat(chunks).toString()))
      res.on('error', reject)
    })
    if (res.statusCode !== 200) return null
    const data = JSON.parse(body) as { tag_name?: string }
    return data.tag_name?.replace(/^v/, '') ?? null
  } catch {
    return null
  }
}

export async function checkForUpdates(): Promise<UpdateStatus> {
  const [manifest, opencliLatest] = await Promise.all([
    fetchManifest(),
    fetchOpenCLILatestVersion(),
  ])
  const local = loadLocalVersions()

  const sidecarCurrent = local.sidecar?.version ?? null
  const ovCurrent = local.openviking?.version ?? null
  const opencliCurrent = local.opencli?.version ?? null
  const kernelCurrent = local.sandbox?.kernel?.version ?? null
  const rootfsCurrent = local.sandbox?.rootfs?.version ?? null

  return {
    sidecar: {
      current: sidecarCurrent,
      latest: manifest.sidecar.version,
      available: !!(manifest.sidecar.version && manifest.sidecar.version !== sidecarCurrent),
    },
    openviking: {
      current: ovCurrent,
      latest: manifest.openviking.version,
      available: !!(manifest.openviking.version && manifest.openviking.version !== ovCurrent),
    },
    opencli: {
      current: opencliCurrent,
      latest: opencliLatest,
      available: !!(opencliLatest && opencliLatest !== opencliCurrent),
    },
    sandbox: {
      kernel: {
        current: kernelCurrent,
        latest: manifest.sandbox.kernel.version,
        available: !!(manifest.sandbox.kernel.version && manifest.sandbox.kernel.version !== kernelCurrent),
      },
      rootfs: {
        current: rootfsCurrent,
        latest: manifest.sandbox.rootfs.version,
        available: !!(manifest.sandbox.rootfs.version && manifest.sandbox.rootfs.version !== rootfsCurrent),
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
  component: 'sidecar' | 'openviking' | 'opencli' | 'sandbox_kernel' | 'sandbox_rootfs',
  onProgress?: (p: DownloadProgress) => void,
): Promise<void> {
  const manifest = await fetchManifest()
  const now = new Date().toISOString()

  if (component === 'sidecar') {
    // 延迟导入避免循环依赖
    const { downloadSidecar } = await import('./sidecar')
    await downloadSidecar(onProgress)

    // 更新 versions.json 中的 sidecar 版本
    const local = loadLocalVersions()
    saveLocalVersions({
      ...local,
      sidecar: { version: manifest.sidecar.version, updated_at: now },
    })
    return
  }

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

  if (component === 'opencli') {
    const { downloadOpenCLI } = await import('./sidecar')
    await downloadOpenCLI(onProgress)

    const latestVersion = await fetchOpenCLILatestVersion()
    if (latestVersion) {
      const local = loadLocalVersions()
      saveLocalVersions({
        ...local,
        opencli: { version: latestVersion, updated_at: now },
      })
    }
    return
  }

  if (component === 'sandbox_kernel') {
    const { version, filename } = manifest.sandbox.kernel
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
    const { version, filename } = manifest.sandbox.rootfs
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
}
