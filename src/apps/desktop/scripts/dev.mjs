import { spawn } from 'child_process'
import { createRequire } from 'module'
import { resolve, dirname } from 'path'
import { mkdirSync } from 'fs'
import { fileURLToPath } from 'url'

const __dirname = dirname(fileURLToPath(import.meta.url))
const root = resolve(__dirname, '..')
const webRoot = resolve(root, '..', 'web')
const workspaceRoot = resolve(root, '..', '..', '..')
const desktopBin = resolve(
  workspaceRoot,
  'src',
  'services',
  'desktop',
  'bin',
  process.platform === 'win32' ? 'desktop.exe' : 'desktop',
)

function resolveCommand(command) {
  if (process.platform !== 'win32') return command
  return command === 'pnpm' ? 'pnpm.cmd' : command
}

function shouldUseShell(command) {
  return process.platform === 'win32' && command.endsWith('.cmd')
}

function resolveElectronPath() {
  return process.platform === 'win32'
    ? resolve(root, 'node_modules', '.bin', 'electron.cmd')
    : resolve(root, 'node_modules', '.bin', 'electron')
}

function runStep(command, args, options = {}) {
  return new Promise((resolvePromise, rejectPromise) => {
    const resolvedCommand = resolveCommand(command)
    const child = spawn(resolvedCommand, args, {
      stdio: 'inherit',
      shell: shouldUseShell(resolvedCommand),
      ...options,
    })
    child.on('error', rejectPromise)
    child.on('exit', (code) => {
      if (code === 0) {
        resolvePromise()
        return
      }
      rejectPromise(new Error(`${command} ${args.join(' ')} exited with code ${code}`))
    })
  })
}

async function waitForVite(url, timeoutMs = 30000) {
  const deadline = Date.now() + timeoutMs
  while (Date.now() < deadline) {
    try {
      const res = await fetch(url)
      if (res.ok) return true
    } catch {}
    await new Promise((r) => setTimeout(r, 500))
  }
  throw new Error(`vite dev server not ready after ${timeoutMs}ms`)
}

function normalizeDevUrl(url) {
  return url.endsWith('/') ? url.slice(0, -1) : url
}

const ANSI_ESCAPE_PATTERN = /\x1B(?:[@-Z\\-_]|\[[0-?]*[ -/]*[@-~])/g

function stripAnsi(value) {
  return value.replace(ANSI_ESCAPE_PATTERN, '')
}

function parseViteDevUrl(output) {
  const match = stripAnsi(output).match(/https?:\/\/(?:localhost|127\.0\.0\.1|\[::1\]):\d+\/?/)
  return match ? normalizeDevUrl(match[0]) : null
}

function startViteDevServer() {
  return new Promise((resolvePromise, rejectPromise) => {
    const viteCommand = resolveCommand('pnpm')
    const vite = spawn(viteCommand, ['exec', 'vite', '--port', '5173'], {
      cwd: webRoot,
      stdio: ['inherit', 'pipe', 'pipe'],
      shell: shouldUseShell(viteCommand),
      env: {
        ...process.env,
        ARKLOOP_API_PROXY_TARGET: 'http://127.0.0.1:19001',
        ARKLOOP_DESKTOP_SHELL_DEV: 'true',
      },
    })

    let settled = false
    let outputBuffer = ''

    const settle = (url) => {
      if (settled) return
      settled = true
      resolvePromise({ vite, url })
    }

    const rejectStart = (error) => {
      if (settled) return
      settled = true
      rejectPromise(error)
    }

    const handleOutput = (chunk, stream) => {
      stream.write(chunk)
      outputBuffer = `${outputBuffer}${chunk.toString()}`
      const url = parseViteDevUrl(outputBuffer)
      if (url) settle(url)
      if (outputBuffer.length > 4096) {
        outputBuffer = outputBuffer.slice(-4096)
      }
    }

    vite.stdout.on('data', (chunk) => handleOutput(chunk, process.stdout))
    vite.stderr.on('data', (chunk) => handleOutput(chunk, process.stderr))
    vite.on('error', rejectStart)
    vite.on('exit', (code) => {
      if (settled) return
      rejectStart(new Error(`vite exited with code ${code ?? 0}`))
    })
  })
}

async function main() {
  console.log('building desktop sidecar...')
  mkdirSync(resolve(desktopBin, '..'), { recursive: true })
  const darwinLdflags = process.platform === 'darwin' ? ['-ldflags', '-extldflags=-Wl,-no_warn_duplicate_libraries'] : []
  await runStep('go', ['build', '-tags', 'desktop', ...darwinLdflags, '-o', desktopBin, './src/services/desktop/cmd/desktop'], {
    cwd: workspaceRoot,
  })

  console.log('starting vite dev server...')
  const { vite, url: viteUrl } = await startViteDevServer()

  console.log('waiting for vite dev server...')
  await waitForVite(viteUrl)
  console.log('vite ready, compiling electron...')

  const require = createRequire(import.meta.url)
  const tscPath = require.resolve('typescript/bin/tsc')

  await runStep('node', [tscPath, '-p', 'tsconfig.main.json'], { cwd: root })
  await runStep('node', [tscPath, '-p', 'tsconfig.preload.json'], { cwd: root })

  console.log('starting electron...')

  const electronPath = resolveElectronPath()
  const electron = spawn(electronPath, ['.', '--remote-debugging-port=9222'], {
    cwd: root,
    stdio: 'inherit',
    shell: shouldUseShell(electronPath),
    env: {
      ...process.env,
      ELECTRON_DEV: 'true',
      VITE_DEV_URL: viteUrl,
    },
  })

  electron.on('exit', (code) => {
    vite.kill()
    process.exit(code ?? 0)
  })

  for (const signal of ['SIGINT', 'SIGTERM']) {
    process.on(signal, () => {
      electron.kill()
      vite.kill()
      process.exit(0)
    })
  }
}

main().catch((err) => {
  console.error(err)
  process.exit(1)
})
