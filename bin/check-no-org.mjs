#!/usr/bin/env node
import { execFileSync } from 'node:child_process'
import fs from 'node:fs'
import path from 'node:path'
import process from 'node:process'
import { fileURLToPath } from 'node:url'

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const historicalMigrationMax = 112
const sourceRoots = [
  'src/',
  'bin/',
  '.github/',
  'package.json',
]
const sourceExtensions = new Set(['.go', '.ts', '.tsx', '.js', '.mjs', '.cjs', '.json', '.yml', '.yaml', '.sh', '.sql'])
const consoleRoots = ['src/apps/console/src/']
const frontendApiRoots = [
  'src/apps/console/src/api/',
  'src/apps/web/src/api',
  'src/apps/shared/src/api/',
]

const rules = [
  {
    name: 'org scope',
    appliesTo(file) {
      return isCurrentSourceFile(file) || isNonHistoricalMigration(file)
    },
    regex: /\b(?:ScopeOrg|CredentialScopeOrg|LlmCredentialScopeOrg)\b|scope\s*[:=][^\n]{0,80}["'`]org["'`]|scope\s+must\s+be\s+org|["'`]org["'`]\s*\|\s*["'`]platform["'`]/,
  },
  {
    name: 'public dto org_id',
    appliesTo(file) {
      return isCurrentSourceFile(file)
    },
    regex: /json:"org_id(?:,[^"]*)?"/,
  },
  {
    name: 'frontend dto org_id',
    appliesTo(file) {
      return isFrontendApiFile(file)
    },
    regex: /\borg_id\s*:/,
  },
  {
    name: 'console org entry',
    appliesTo(file) {
      return isConsoleFile(file)
    },
    regex: /\bOrgsPage\b|\blistMyOrgs\b|["'`]\/orgs\b["'`]|["'`]orgs["'`]|\bOrganizations\b/,
  },
]

main()

function main() {
  const files = collectTargetFiles()
  if (files.length === 0) {
    console.log('check-no-org: no changed files')
    return
  }

  const lineMap = buildChangedLineMap(files)
  const findings = []
  for (const file of files) {
    const lines = readTextLines(file)
    if (!lines) {
      continue
    }
    const changedLines = lineMap.get(file)
    for (let index = 0; index < lines.length; index += 1) {
      const lineNumber = index + 1
      if (changedLines instanceof Set && !changedLines.has(lineNumber)) {
        continue
      }
      const line = lines[index]
      for (const rule of rules) {
        if (!rule.appliesTo(file)) {
          continue
        }
        if (rule.regex.test(line)) {
          findings.push({
            file,
            line: lineNumber,
            rule: rule.name,
            content: line.trim(),
          })
        }
      }
    }
  }

  if (findings.length === 0) {
    console.log(`check-no-org: ok (${files.length} files)`)
    return
  }

  console.error('check-no-org: blocked')
  for (const finding of findings) {
    console.error(`${finding.file}:${finding.line}: ${finding.rule}: ${finding.content}`)
  }
  process.exit(1)
}

function buildChangedLineMap(files) {
  const map = new Map()
  const untracked = new Set(gitLines(['ls-files', '--others', '--exclude-standard']))
  for (const file of files) {
    if (untracked.has(file)) {
      map.set(file, null)
      continue
    }

    const changedLines = new Set()
    addChangedLines(changedLines, gitText(['diff', '--unified=0', '--no-color', 'HEAD', '--', file]))
    addChangedLines(changedLines, gitText(['diff', '--cached', '--unified=0', '--no-color', '--', file]))

    if (process.env.GITHUB_ACTIONS === 'true') {
      const baseRef = process.env.GITHUB_BASE_REF?.trim()
      if (baseRef) {
        addChangedLines(changedLines, gitText(['diff', '--unified=0', '--no-color', `origin/${baseRef}...HEAD`, '--', file]))
      } else {
        addChangedLines(changedLines, gitText(['diff', '--unified=0', '--no-color', 'HEAD^', 'HEAD', '--', file]))
      }
    }

    map.set(file, changedLines.size === 0 ? null : changedLines)
  }
  return map
}

function addChangedLines(target, diffText) {
  if (!diffText) {
    return
  }
  for (const line of diffText.split(/\r?\n/)) {
    const match = line.match(/^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@/)
    if (!match) {
      continue
    }
    const start = Number(match[1])
    const count = Number(match[2] ?? '1')
    for (let offset = 0; offset < count; offset += 1) {
      target.add(start + offset)
    }
  }
}

function collectTargetFiles() {
  const changed = process.env.ARKLOOP_GUARD_FULL_SCAN === '1'
    ? listAllCandidateFiles()
    : listChangedCandidateFiles()
  return [...new Set(changed)].filter((file) => shouldInspect(file)).sort()
}

function listAllCandidateFiles() {
  return [
    ...gitLines(['ls-files']),
    ...gitLines(['ls-files', '--others', '--exclude-standard']),
  ]
}

function listChangedCandidateFiles() {
  const files = new Set()
  addAll(files, gitLines(['diff', '--name-only', '--diff-filter=AMR', 'HEAD', '--']))
  addAll(files, gitLines(['diff', '--name-only', '--diff-filter=AMR', '--cached', '--']))
  addAll(files, gitLines(['ls-files', '--others', '--exclude-standard']))

  if (process.env.GITHUB_ACTIONS === 'true') {
    const baseRef = process.env.GITHUB_BASE_REF?.trim()
    if (baseRef) {
      addAll(files, gitLines(['diff', '--name-only', '--diff-filter=AMR', `origin/${baseRef}...HEAD`]))
    } else {
      addAll(files, gitLines(['diff', '--name-only', '--diff-filter=AMR', 'HEAD^', 'HEAD']))
    }
  }

  if (files.size === 0) {
    addAll(files, gitLines(['diff', '--name-only', '--diff-filter=AMR', 'HEAD^', 'HEAD']))
  }

  return [...files]
}

function shouldInspect(file) {
  if (!file || file === 'bin/check-no-org.mjs' || file.includes('node_modules/') || file.includes('/dist/') || file.startsWith('output/')) {
    return false
  }
  if (!sourceRoots.some((root) => file === root || file.startsWith(root))) {
    return false
  }
  if (isHistoricalMigration(file)) {
    return false
  }
  const ext = path.extname(file)
  if (ext) {
    return sourceExtensions.has(ext)
  }
  return file.startsWith('bin/')
}

function isCurrentSourceFile(file) {
  return !file.includes('/migrate/migrations/')
}

function isConsoleFile(file) {
  return consoleRoots.some((root) => file.startsWith(root))
}

function isFrontendApiFile(file) {
  return frontendApiRoots.some((root) => file.startsWith(root))
}

function isHistoricalMigration(file) {
  const match = file.match(/src\/services\/api\/internal\/migrate\/migrations\/(\d+)_/)
  if (!match) {
    return false
  }
  return Number(match[1]) <= historicalMigrationMax
}

function isNonHistoricalMigration(file) {
  const match = file.match(/src\/services\/api\/internal\/migrate\/migrations\/(\d+)_/)
  if (!match) {
    return false
  }
  return Number(match[1]) > historicalMigrationMax
}

function readTextLines(file) {
  const absolutePath = path.join(repoRoot, file)
  try {
    const text = fs.readFileSync(absolutePath, 'utf8')
    return text.split(/\r?\n/)
  } catch {
    return null
  }
}

function gitLines(args) {
  const output = gitText(args)
  if (!output) {
    return []
  }
  return output.split(/\r?\n/).map((line) => line.trim()).filter(Boolean)
}

function gitText(args) {
  try {
    return execFileSync('git', args, {
      cwd: repoRoot,
      encoding: 'utf8',
      stdio: ['ignore', 'pipe', 'ignore'],
    })
  } catch {
    return ''
  }
}

function addAll(target, items) {
  for (const item of items) {
    target.add(item)
  }
}
