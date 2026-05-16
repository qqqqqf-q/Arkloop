import { app, BrowserWindow, dialog, type MessageBoxOptions, type MessageBoxReturnValue } from 'electron'
import * as fs from 'fs'
import * as os from 'os'
import * as path from 'path'
import { execFileSync } from 'child_process'
import { loadConfig, saveConfig } from './config'

export type CommandLineToolStatus = {
  available: boolean
  installed: boolean
  sourcePath: string | null
  targetPath: string
}

const ARKLOOP_HOME = path.join(os.homedir(), '.arkloop')

function cliBinaryName(): string {
  return process.platform === 'win32' ? 'ark.exe' : 'ark'
}

function executable(pathname: string): boolean {
  try {
    fs.accessSync(pathname, fs.constants.X_OK)
    return true
  } catch {
    return false
  }
}

function fileExists(pathname: string): boolean {
  try {
    return fs.statSync(pathname).isFile()
  } catch {
    return false
  }
}

function bundledCliPath(): string {
  return path.join(process.resourcesPath, 'cli', cliBinaryName())
}

function bundledWebRoot(sourcePath: string): string {
  return path.resolve(path.dirname(sourcePath), '..', 'renderer')
}

function devCliCandidates(): string[] {
  return [
    path.resolve(__dirname, '..', '..', '..', '..', '..', 'bin', cliBinaryName()),
    path.resolve(process.cwd(), 'bin', cliBinaryName()),
  ]
}

export function resolveCommandLineToolSource(): string | null {
  const candidates = app.isPackaged ? [bundledCliPath()] : [...devCliCandidates(), bundledCliPath()]
  for (const candidate of candidates) {
    if (executable(candidate) || fileExists(candidate)) return candidate
  }
  return null
}

export function commandLineToolTargetPath(): string {
  if (process.platform === 'darwin') return '/usr/local/bin/ark'
  if (process.platform === 'win32') return path.join(process.env.LOCALAPPDATA || ARKLOOP_HOME, 'Arkloop', 'bin', 'ark.exe')
  return path.join(os.homedir(), '.local', 'bin', 'ark')
}

function commandPathCandidates(command: string): string[] {
  const pathEnv = process.env.PATH || ''
  const dirs = pathEnv.split(path.delimiter).filter(Boolean)
  if (process.platform !== 'win32') {
    return dirs.map((dir) => path.join(dir, command))
  }

  const extensions = (process.env.PATHEXT || '.EXE;.CMD;.BAT;.COM')
    .split(';')
    .filter(Boolean)
  const names = path.extname(command) ? [command] : [command, ...extensions.map((ext) => `${command}${ext}`)]
  return dirs.flatMap((dir) => names.map((name) => path.join(dir, name)))
}

function resolveInstalledCommandPath(): string | null {
  for (const candidate of commandPathCandidates(cliBinaryName())) {
    if (executable(candidate) || fileExists(candidate)) return candidate
  }
  return null
}

function targetLooksInstalled(targetPath: string): boolean {
  return fileExists(targetPath) || executable(targetPath)
}

export function getCommandLineToolStatus(): CommandLineToolStatus {
  const sourcePath = resolveCommandLineToolSource()
  const targetPath = commandLineToolTargetPath()
  const installedPath = resolveInstalledCommandPath()
  return {
    available: sourcePath !== null,
    installed: targetLooksInstalled(targetPath) || installedPath !== null,
    sourcePath,
    targetPath,
  }
}

function shellSingleQuote(value: string): string {
  return `'${value.replace(/'/g, `'\\''`)}'`
}

function ensureSourceExecutable(sourcePath: string): void {
  if (process.platform !== 'win32') {
    fs.chmodSync(sourcePath, 0o755)
  }
}

function installMacCommandLineTool(sourcePath: string, targetPath: string): void {
  const script = [
    `mkdir -p ${shellSingleQuote(path.dirname(targetPath))}`,
    `ln -sf ${shellSingleQuote(sourcePath)} ${shellSingleQuote(targetPath)}`,
  ].join(' && ')

  try {
    fs.mkdirSync(path.dirname(targetPath), { recursive: true })
    fs.rmSync(targetPath, { force: true })
    fs.symlinkSync(sourcePath, targetPath)
    return
  } catch {}

  execFileSync('osascript', [
    '-e',
    `do shell script ${JSON.stringify(script)} with administrator privileges`,
  ])
}

function installUserCommandLineTool(sourcePath: string, targetPath: string): void {
  fs.mkdirSync(path.dirname(targetPath), { recursive: true })
  if (process.platform === 'win32') {
    fs.copyFileSync(sourcePath, targetPath)
    const webRoot = bundledWebRoot(sourcePath)
    if (fileExists(path.join(webRoot, 'index.html'))) {
      fs.cpSync(webRoot, path.join(path.dirname(targetPath), 'web'), { recursive: true, force: true })
    }
    return
  }
  fs.rmSync(targetPath, { force: true })
  fs.symlinkSync(sourcePath, targetPath)
}

export async function installCommandLineTool(): Promise<CommandLineToolStatus> {
  const sourcePath = resolveCommandLineToolSource()
  if (!sourcePath) {
    throw new Error('ark binary is not bundled')
  }
  ensureSourceExecutable(sourcePath)
  const targetPath = commandLineToolTargetPath()
  if (process.platform === 'darwin') {
    installMacCommandLineTool(sourcePath, targetPath)
  } else {
    installUserCommandLineTool(sourcePath, targetPath)
  }
  return getCommandLineToolStatus()
}

function shouldPromptForCommandLineTool(): boolean {
  if (!app.isPackaged) return false
  const config = loadConfig()
  if (config.desktop.commandLineToolPromptDisabled) return false
  const status = getCommandLineToolStatus()
  return status.available && !status.installed
}

function promptCopy(targetPath: string) {
  const isZh = app.getLocale().toLowerCase().startsWith('zh')
  if (isZh) {
    return {
      title: '安装 ark 命令行工具？',
      message: '安装 ark 命令行工具？',
      detail: process.platform === 'darwin'
        ? `macOS 会请求管理员密码来创建 ${targetPath}。`
        : `Arkloop 会创建 ${targetPath}。`,
      install: '安装',
      never: '不再询问',
      skip: '跳过',
      done: '命令行工具已安装。',
      failed: '命令行工具安装失败。',
    }
  }
  return {
    title: 'Install the ark command-line tool?',
    message: 'Install the ark command-line tool?',
    detail: process.platform === 'darwin'
      ? `macOS will prompt for your administrator password to create ${targetPath}.`
      : `Arkloop will create ${targetPath}.`,
    install: 'Install',
    never: "Don't ask again",
    skip: 'Skip',
    done: 'Command-line tool installed.',
    failed: 'Command-line tool installation failed.',
  }
}

function showCliDialog(
  win: BrowserWindow | null,
  options: MessageBoxOptions,
): Promise<MessageBoxReturnValue> {
  if (win) return dialog.showMessageBox(win, options)
  return dialog.showMessageBox(options)
}

export async function maybePromptInstallCommandLineTool(win: BrowserWindow | null): Promise<void> {
  if (!shouldPromptForCommandLineTool()) return

  const copy = promptCopy(commandLineToolTargetPath())
  const result = await showCliDialog(win, {
    type: 'question',
    title: copy.title,
    message: copy.message,
    detail: copy.detail,
    buttons: [copy.install, copy.never, copy.skip],
    defaultId: 0,
    cancelId: 2,
    noLink: true,
  })

  if (result.response === 1) {
    const config = loadConfig()
    saveConfig({
      ...config,
      desktop: { ...config.desktop, commandLineToolPromptDisabled: true },
    })
    return
  }
  if (result.response !== 0) return

  try {
    await installCommandLineTool()
    await showCliDialog(win, {
      type: 'info',
      title: copy.done,
      message: copy.done,
      buttons: ['OK'],
      noLink: true,
    })
  } catch (error) {
    await showCliDialog(win, {
      type: 'error',
      title: copy.failed,
      message: copy.failed,
      detail: error instanceof Error ? error.message : String(error),
      buttons: ['OK'],
      noLink: true,
    })
  }
}
