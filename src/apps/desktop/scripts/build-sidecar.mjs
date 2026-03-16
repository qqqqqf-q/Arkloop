#!/usr/bin/env node

import { execFileSync } from 'child_process'
import { mkdirSync } from 'fs'
import { resolve, dirname } from 'path'
import { fileURLToPath } from 'url'
import { parseArgs } from 'util'

const __dirname = dirname(fileURLToPath(import.meta.url))
const workspaceRoot = resolve(__dirname, '..', '..', '..', '..')
const outDir = resolve(__dirname, '..', 'sidecar-bin')

// All supported targets
const ALL_TARGETS = [
  { platform: 'darwin', arch: 'arm64' },
  { platform: 'darwin', arch: 'x64' },
  { platform: 'linux', arch: 'x64' },
  { platform: 'linux', arch: 'arm64' },
  { platform: 'win32', arch: 'x64' },
]

// Electron → Go mapping
const PLATFORM_MAP = { darwin: 'darwin', linux: 'linux', win32: 'windows' }
const ARCH_MAP = { arm64: 'arm64', x64: 'amd64' }

function binaryName(platform, arch) {
  const name = `desktop-${platform}-${arch}`
  return platform === 'win32' ? `${name}.exe` : name
}

function currentTarget() {
  const platform = process.platform
  const arch = process.arch === 'arm64' ? 'arm64' : 'x64'
  return { platform, arch }
}

function buildTarget({ platform, arch }) {
  const goos = PLATFORM_MAP[platform]
  const goarch = ARCH_MAP[arch]
  if (!goos) throw new Error(`unsupported platform: ${platform}`)
  if (!goarch) throw new Error(`unsupported arch: ${arch}`)

  const outFile = resolve(outDir, binaryName(platform, arch))
  mkdirSync(outDir, { recursive: true })

  // Darwin builds require CGO=1 because the VZ (Virtualization.framework)
  // sandbox backend uses Objective-C via CGO.  Cross-compilation to darwin
  // from a non-darwin host is not supported for this reason.
  const isDarwin = platform === 'darwin'
  const isCrossCompile = isDarwin && process.platform !== 'darwin'
  if (isCrossCompile) {
    console.warn(`[build-sidecar] WARNING: cross-compiling darwin target from ${process.platform} — CGO required but unavailable; VZ sandbox will be stubbed out`)
  }
  const cgoEnabled = isDarwin ? '1' : '0'

  console.log(`[build-sidecar] ${platform}/${arch} → GOOS=${goos} GOARCH=${goarch} CGO_ENABLED=${cgoEnabled}`)
  console.log(`[build-sidecar] output: ${outFile}`)

  execFileSync('go', [
    'build',
    '-tags', 'desktop',
    '-o', outFile,
    './src/services/desktop/cmd/desktop',
  ], {
    cwd: workspaceRoot,
    stdio: 'inherit',
    env: {
      ...process.env,
      GOOS: goos,
      GOARCH: goarch,
      CGO_ENABLED: cgoEnabled,
    },
  })

  console.log(`[build-sidecar] ✓ ${platform}/${arch} done`)
}

function printHelp() {
  console.log(`Usage: build-sidecar.mjs [options]

Options:
  --platform <name>   Target platform: darwin, linux, win32 (default: current)
  --arch <name>       Target arch: arm64, x64 (default: current)
  --all               Build for all supported platform/arch combos
  --help              Show this help

Supported targets:
${ALL_TARGETS.map(t => `  ${t.platform}/${t.arch}`).join('\n')}`)
}

// --- main ---

const { values } = parseArgs({
  options: {
    platform: { type: 'string' },
    arch: { type: 'string' },
    all: { type: 'boolean', default: false },
    help: { type: 'boolean', default: false },
  },
  strict: true,
})

if (values.help) {
  printHelp()
  process.exit(0)
}

if (values.all) {
  console.log(`[build-sidecar] building all ${ALL_TARGETS.length} targets...`)
  for (const target of ALL_TARGETS) {
    buildTarget(target)
  }
  console.log('[build-sidecar] all targets complete')
} else {
  const target = {
    platform: values.platform ?? currentTarget().platform,
    arch: values.arch ?? currentTarget().arch,
  }
  buildTarget(target)
}
