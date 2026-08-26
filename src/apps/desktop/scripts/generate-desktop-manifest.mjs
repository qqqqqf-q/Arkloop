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

function buildManifest() {
  const version = readEnv('ARKLOOP_RELEASE_VERSION').replace(/^v/, '')
  const outputPath = readEnv('ARKLOOP_DESKTOP_MANIFEST_OUTPUT')

  const rtkVersion = readOptionalEnv('ARKLOOP_RTK_VERSION')?.replace(/^v/, '') ?? null
  const rtkRepo = readOptionalEnv('ARKLOOP_RTK_REPO') ?? null
  const opencliVersion = readOptionalEnv('ARKLOOP_OPENCLI_VERSION')?.replace(/^v/, '') ?? null
  const opencliRepo = readOptionalEnv('ARKLOOP_OPENCLI_REPO') ?? null

  const bins = {}
  if (rtkVersion && rtkRepo) {
    bins.rtk = { version: rtkVersion, repo: rtkRepo }
  }
  if (opencliVersion && opencliRepo) {
    bins.opencli = { version: opencliVersion, repo: opencliRepo }
  }

  const manifest = {
    version,
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
