import { spawn } from 'child_process'
import { existsSync, rmSync } from 'fs'
import { dirname, resolve } from 'path'
import { fileURLToPath } from 'url'

const __dirname = dirname(fileURLToPath(import.meta.url))
const root = resolve(__dirname, '..')

function resolveCommand(command) {
  return process.platform === 'win32' ? `${command}.cmd` : command
}

function shouldUseShell(command) {
  return process.platform === 'win32' && command.endsWith('.cmd')
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

function getAppPath() {
  const candidates = [
    resolve(root, 'release', 'mac-arm64', 'Arkloop.app'),
    resolve(root, 'release', 'mac-universal', 'Arkloop.app'),
    resolve(root, 'release', 'mac', 'Arkloop.app'),
    resolve(root, 'release', 'mac-x64', 'Arkloop.app'),
  ]

  return candidates.find((candidate) => existsSync(candidate)) ?? null
}

async function main() {
  const skipPack = process.argv.includes('--skip-pack')
  const skipOpen = process.argv.includes('--skip-open')

  if (process.platform !== 'darwin') {
    throw new Error('preview:mac 仅支持在 macOS 上运行')
  }

  if (!skipPack) {
    rmSync(resolve(root, 'release'), { recursive: true, force: true })
    await runStep('pnpm', ['run', 'pack:app'], { cwd: root })
  }

  const appPath = getAppPath()
  if (!appPath) {
    throw new Error('未找到 Arkloop.app，请先确认 pack 已成功生成产物')
  }

  console.log(`Arkloop app: ${appPath}`)

  if (!skipOpen) {
    await runStep('open', ['-n', appPath], { cwd: root })
  }
}

main().catch((error) => {
  console.error(error)
  process.exit(1)
})
