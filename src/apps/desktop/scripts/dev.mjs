import { spawn } from 'child_process'
import { createRequire } from 'module'
import { resolve, dirname } from 'path'
import { mkdirSync } from 'fs'
import { createServer } from 'net'
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

function canListenOnPort(port) {
  return new Promise((resolvePromise) => {
    const server = createServer()
    server.once('error', () => resolvePromise(false))
    server.once('listening', () => {
      server.close(() => resolvePromise(true))
    })
    server.listen(port, '127.0.0.1')
  })
}

async function findAvailablePort(startPort) {
  for (let port = startPort; port < startPort + 20; port += 1) {
    if (await canListenOnPort(port)) {
      return port
    }
  }
  throw new Error(`no available vite port found from ${startPort}`)
}

async function main() {
  const vitePort = await findAvailablePort(5173)
  const viteUrl = `http://localhost:${vitePort}`

  console.log('building desktop sidecar...')
  mkdirSync(resolve(desktopBin, '..'), { recursive: true })
  await runStep('go', ['build', '-tags', 'desktop', '-ldflags', '-extldflags=-Wl,-no_warn_duplicate_libraries', '-o', desktopBin, './src/services/desktop/cmd/desktop'], {
    cwd: workspaceRoot,
  })

  // Start Vite directly with sidecar proxy target, overriding .env.local
  console.log('starting vite dev server...')
  const viteCommand = resolveCommand('pnpm')
  const vite = spawn(viteCommand, ['exec', 'vite', '--port', String(vitePort), '--strictPort'], {
    cwd: webRoot,
    stdio: 'inherit',
    shell: shouldUseShell(viteCommand),
    env: {
      ...process.env,
      ARKLOOP_API_PROXY_TARGET: 'http://127.0.0.1:19001',
      ARKLOOP_DESKTOP_SHELL_DEV: 'true',
    },
  })

  vite.on('error', (err) => {
    console.error('vite failed to start:', err)
    process.exit(1)
  })

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
