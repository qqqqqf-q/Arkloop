import fs from 'fs'
import path from 'path'
import process from 'process'

function readEnv(name) {
  const value = process.env[name]?.trim()
  if (!value) {
    throw new Error(`missing required env: ${name}`)
  }
  return value
}

function readOptionalEnv(name) {
  return process.env[name]?.trim() || null
}

function ensureFile(filePath) {
  if (!fs.existsSync(filePath)) {
    throw new Error(`missing asset: ${filePath}`)
  }
}

function buildManifest() {
  const version = readEnv('ARKLOOP_RELEASE_VERSION').replace(/^v/, '')
  const outputPath = readEnv('ARKLOOP_DESKTOP_MANIFEST_OUTPUT')
  const releaseDir = path.resolve(readEnv('ARKLOOP_RELEASE_DIR'))

  const openvikingImage = readEnv('ARKLOOP_OPENVIKING_IMAGE')
  const openvikingVersion = readOptionalEnv('ARKLOOP_OPENVIKING_VERSION') || version
  const releaseLabel = readOptionalEnv('ARKLOOP_RELEASE_LABEL')
  const sandboxKernelFilename = readOptionalEnv('ARKLOOP_SANDBOX_KERNEL_FILENAME')
  const sandboxKernelVersion = readOptionalEnv('ARKLOOP_SANDBOX_KERNEL_VERSION')?.replace(/^v/, '') ?? null
  const sandboxRootfsFilename = readOptionalEnv('ARKLOOP_SANDBOX_ROOTFS_FILENAME')
  const sandboxRootfsVersion = readOptionalEnv('ARKLOOP_SANDBOX_ROOTFS_VERSION')?.replace(/^v/, '') ?? null

  const rtkVersion = readOptionalEnv('ARKLOOP_RTK_VERSION')?.replace(/^v/, '') ?? null
  const rtkRepo = readOptionalEnv('ARKLOOP_RTK_REPO') ?? null
  const opencliVersion = readOptionalEnv('ARKLOOP_OPENCLI_VERSION')?.replace(/^v/, '') ?? null
  const opencliRepo = readOptionalEnv('ARKLOOP_OPENCLI_REPO') ?? null

  const sandbox = {}

  if (sandboxKernelFilename || sandboxKernelVersion) {
    if (!sandboxKernelFilename || !sandboxKernelVersion) {
      throw new Error('sandbox kernel manifest fields must be provided together')
    }
    ensureFile(path.join(releaseDir, sandboxKernelFilename))
    sandbox.kernel = {
      version: sandboxKernelVersion,
      filename: sandboxKernelFilename,
    }
  }

  if (sandboxRootfsFilename || sandboxRootfsVersion) {
    if (!sandboxRootfsFilename || !sandboxRootfsVersion) {
      throw new Error('sandbox rootfs manifest fields must be provided together')
    }
    ensureFile(path.join(releaseDir, sandboxRootfsFilename))
    sandbox.rootfs = {
      version: sandboxRootfsVersion,
      filename: sandboxRootfsFilename,
    }
  }

  const bins = {}
  if (rtkVersion && rtkRepo) {
    bins.rtk = { version: rtkVersion, repo: rtkRepo }
  }
  if (opencliVersion && opencliRepo) {
    bins.opencli = { version: opencliVersion, repo: opencliRepo }
  }

  const manifest = {
    version,
    ...(releaseLabel ? { release_name: `${version} ${releaseLabel}` } : {}),
    openviking: {
      image: openvikingImage,
      version: openvikingVersion,
    },
    ...(Object.keys(sandbox).length > 0 ? { sandbox } : {}),
    ...(Object.keys(bins).length > 0 ? { bins } : {}),
  }

  fs.mkdirSync(path.dirname(outputPath), { recursive: true })
  fs.writeFileSync(outputPath, `${JSON.stringify(manifest, null, 2)}\n`, 'utf8')
}

try {
  buildManifest()
} catch (error) {
  console.error(error instanceof Error ? error.message : String(error))
  process.exit(1)
}
