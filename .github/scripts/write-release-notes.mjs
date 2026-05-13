import { execFileSync, spawnSync } from 'node:child_process'
import fs from 'node:fs'

const MAX_PROMPT_CHARS = 120_000
const DOWNLOADS = `## 下载

- Windows：下载 \`Arkloop-win-x64.exe\`
- macOS Apple Silicon：下载 \`Arkloop-mac-arm64.dmg\`
- macOS Intel：下载 \`Arkloop-mac-x64.dmg\`
- Linux x64：下载 \`Arkloop-linux-amd64.deb\`、\`Arkloop-linux-x86_64.rpm\`、\`Arkloop-linux-x86_64.AppImage\` 或 \`arkloop-bin-*.pkg.tar.zst\`
`

function readEnv(name) {
  const value = process.env[name]?.trim()
  if (!value) {
    throw new Error(`missing env: ${name}`)
  }
  return value
}

function git(args, options = {}) {
  return execFileSync('git', args, {
    encoding: 'utf8',
    stdio: ['ignore', 'pipe', options.allowFailure ? 'ignore' : 'pipe'],
  }).trim()
}

function tryGit(args) {
  try {
    return git(args, { allowFailure: true })
  } catch {
    return ''
  }
}

function resolvePreviousTag() {
  return tryGit(['describe', '--tags', '--match', 'v*', '--abbrev=0', 'HEAD^'])
}

function collectCommits(range) {
  const raw = git(['log', range, '--no-merges', '--pretty=format:%H%x1f%h%x1f%s%x1f%b%x1e'])
  return raw
    .split('\x1e')
    .map((entry) => entry.trim())
    .filter(Boolean)
    .map((entry) => {
      const [hash, shortHash, subject, body = ''] = entry.split('\x1f')
      return { hash, shortHash, subject, body: body.trim() }
    })
}

function collectChangedFiles(range, hasPreviousTag) {
  const args = hasPreviousTag
    ? ['diff', '--name-only', range]
    : ['log', '--name-only', '--pretty=format:', range]
  return Array.from(new Set(git(args)
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean)))
    .slice(0, 300)
}

function formatCommitLog(commits) {
  return commits.map((commit) => {
    const body = commit.body ? `\n  body: ${commit.body.replace(/\n+/g, '\n  ')}` : ''
    return `- ${commit.shortHash} ${commit.subject}${body}`
  }).join('\n')
}

function stripMarkdownFence(text) {
  const trimmed = text.trim()
  const match = trimmed.match(/^```(?:markdown|md)?\s*\n([\s\S]*?)\n```$/i)
  return (match?.[1] ?? trimmed).trim()
}

function buildPrompt({ version, previousTag, commits, changedFiles }) {
  const prompt = `你是 Arkloop 的 release changelog 生成器。

只根据输入里的 commit 生成用户可读的 Markdown 更新内容。
不要读取仓库文件，不要运行命令，不要编造 commit 中没有体现的变化。
不要输出下载信息、标题前言、代码围栏或解释过程。
按下面顺序组织，空分类不要输出：
- 新功能
- 修复
- 改进
- 文档
- 构建与 CI

要求：
- 每条使用简洁中文。
- 合并重复或连续修正，不逐条机械复述 commit。
- 保留重要迁移、兼容性或行为变化。
- 如果没有可归纳内容，只输出 "- 无变更记录。"

版本：v${version}
上一个版本：${previousTag || '无'}

Commits:
${formatCommitLog(commits)}

Changed files:
${changedFiles.join('\n')}
`
  if (prompt.length > MAX_PROMPT_CHARS) {
    throw new Error(`release changelog prompt too large: ${prompt.length}`)
  }
  return prompt
}

function runCursor(prompt) {
  readEnv('CURSOR_API_KEY')
  const model = process.env.CURSOR_MODEL?.trim() || 'composer-2'
  const run = spawnSync('cursor-agent', [
    '--trust',
    '--print',
    '--output-format',
    'json',
    '--model',
    model,
    '-p',
    prompt,
  ], {
    encoding: 'utf8',
    env: process.env,
    maxBuffer: 10 * 1024 * 1024,
  })

  if (run.status !== 0) {
    throw new Error(run.stderr.trim() || `cursor-agent failed: ${run.status}`)
  }

  let parsed
  try {
    parsed = JSON.parse(run.stdout.trim())
  } catch {
    throw new Error('invalid cursor-agent json output')
  }

  const result = typeof parsed.result === 'string' ? stripMarkdownFence(parsed.result) : ''
  if (!result) {
    throw new Error('empty cursor-agent result')
  }
  return result
}

function main() {
  const version = readEnv('RELEASE_VERSION').replace(/^v/, '')
  const previousTag = resolvePreviousTag()
  const range = previousTag ? `${previousTag}..HEAD` : 'HEAD'
  const commits = collectCommits(range)
  if (commits.length === 0) {
    throw new Error('empty release commit range')
  }

  const changedFiles = collectChangedFiles(range, Boolean(previousTag))
  const changelog = runCursor(buildPrompt({ version, previousTag, commits, changedFiles }))

  fs.writeFileSync('release-changelog.md', `${changelog}\n`, 'utf8')
  fs.writeFileSync('release-notes.md', `${DOWNLOADS}\n## 更新内容\n\n${changelog}\n`, 'utf8')
}

try {
  main()
} catch (error) {
  console.error(error instanceof Error ? error.message : String(error))
  process.exit(1)
}
