import { spawn } from 'child_process'
import { createRequire } from 'module'
import { resolve, dirname } from 'path'
import { fileURLToPath } from 'url'

const __dirname = dirname(fileURLToPath(import.meta.url))
const root = resolve(__dirname, '..')
const webRoot = resolve(root, '..', 'web')

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

async function main() {
  const viteUrl = 'http://localhost:5173'

  // Start Vite directly with sidecar proxy target, overriding .env.local
  console.log('[dev] starting vite dev server...')
  const vite = spawn('npx', ['vite'], {
    cwd: webRoot,
    stdio: 'inherit',
    env: {
      ...process.env,
      ARKLOOP_API_PROXY_TARGET: 'http://127.0.0.1:19001',
    },
  })

  vite.on('error', (err) => {
    console.error('[dev] vite failed to start:', err)
    process.exit(1)
  })

  console.log('[dev] waiting for vite dev server...')
  await waitForVite(viteUrl)
  console.log('[dev] vite ready, compiling electron...')

  const require = createRequire(import.meta.url)
  const tscPath = require.resolve('typescript/bin/tsc')

  const tscMain = spawn('node', [tscPath, '-p', 'tsconfig.main.json'], { cwd: root, stdio: 'inherit' })
  await new Promise((res, rej) => {
    tscMain.on('exit', (code) => (code === 0 ? res() : rej(new Error(`tsc main: ${code}`))))
  })

  const tscPreload = spawn('node', [tscPath, '-p', 'tsconfig.preload.json'], { cwd: root, stdio: 'inherit' })
  await new Promise((res, rej) => {
    tscPreload.on('exit', (code) => (code === 0 ? res() : rej(new Error(`tsc preload: ${code}`))))
  })

  console.log('[dev] starting electron...')

  const electronPath = resolve(root, 'node_modules', '.bin', 'electron')
  const electron = spawn(electronPath, ['.'], {
    cwd: root,
    stdio: 'inherit',
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
