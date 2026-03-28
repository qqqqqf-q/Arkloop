import { spawn } from 'child_process'
import { dirname, resolve } from 'path'
import { fileURLToPath } from 'url'

const __dirname = dirname(fileURLToPath(import.meta.url))
const desktopRoot = resolve(__dirname, '..')
const webRoot = resolve(desktopRoot, '..', 'web')

function runStep(command, args, options = {}) {
  return new Promise((resolvePromise, rejectPromise) => {
    const child = spawn(command, args, { stdio: 'inherit', ...options })
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

async function main() {
  await runStep('pnpm', ['run', 'build'], {
    cwd: webRoot,
    env: {
      ...process.env,
      ARKLOOP_WEB_BASE: './',
    },
  })
}

main().catch((error) => {
  console.error(error)
  process.exit(1)
})
