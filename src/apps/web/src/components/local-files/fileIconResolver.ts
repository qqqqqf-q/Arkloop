import { generateManifest, type Manifest } from 'material-icon-theme'
import type { LocalFileEntry } from '@arkloop/shared/desktop'
import fileIconUrl from '../../../node_modules/material-icon-theme/icons/file.svg?url'
import claudeIconUrl from '../../../node_modules/material-icon-theme/icons/claude.svg?url'
import cssIconUrl from '../../../node_modules/material-icon-theme/icons/css.svg?url'
import dockerIconUrl from '../../../node_modules/material-icon-theme/icons/docker.svg?url'
import gitIconUrl from '../../../node_modules/material-icon-theme/icons/git.svg?url'
import goIconUrl from '../../../node_modules/material-icon-theme/icons/go.svg?url'
import goModIconUrl from '../../../node_modules/material-icon-theme/icons/go-mod.svg?url'
import javascriptIconUrl from '../../../node_modules/material-icon-theme/icons/javascript.svg?url'
import jsonIconUrl from '../../../node_modules/material-icon-theme/icons/json.svg?url'
import markdownIconUrl from '../../../node_modules/material-icon-theme/icons/markdown.svg?url'
import nodejsIconUrl from '../../../node_modules/material-icon-theme/icons/nodejs.svg?url'
import pnpmIconUrl from '../../../node_modules/material-icon-theme/icons/pnpm.svg?url'
import readmeIconUrl from '../../../node_modules/material-icon-theme/icons/readme.svg?url'
import reactTsIconUrl from '../../../node_modules/material-icon-theme/icons/react_ts.svg?url'
import settingsIconUrl from '../../../node_modules/material-icon-theme/icons/settings.svg?url'
import tsconfigIconUrl from '../../../node_modules/material-icon-theme/icons/tsconfig.svg?url'
import tuneIconUrl from '../../../node_modules/material-icon-theme/icons/tune.svg?url'
import typescriptIconUrl from '../../../node_modules/material-icon-theme/icons/typescript.svg?url'
import viteIconUrl from '../../../node_modules/material-icon-theme/icons/vite.svg?url'
import xmlIconUrl from '../../../node_modules/material-icon-theme/icons/xml.svg?url'
import yamlIconUrl from '../../../node_modules/material-icon-theme/icons/yaml.svg?url'

const iconAssetUrls: Record<string, string> = {
  'file.svg': fileIconUrl,
  'claude.svg': claudeIconUrl,
  'css.svg': cssIconUrl,
  'docker.svg': dockerIconUrl,
  'git.svg': gitIconUrl,
  'go.svg': goIconUrl,
  'go-mod.svg': goModIconUrl,
  'javascript.svg': javascriptIconUrl,
  'json.svg': jsonIconUrl,
  'markdown.svg': markdownIconUrl,
  'nodejs.svg': nodejsIconUrl,
  'pnpm.svg': pnpmIconUrl,
  'readme.svg': readmeIconUrl,
  'react_ts.svg': reactTsIconUrl,
  'settings.svg': settingsIconUrl,
  'tsconfig.svg': tsconfigIconUrl,
  'tune.svg': tuneIconUrl,
  'typescript.svg': typescriptIconUrl,
  'vite.svg': viteIconUrl,
  'xml.svg': xmlIconUrl,
  'yaml.svg': yamlIconUrl,
}

const manifest = generateManifest() as Manifest
const iconDefinitions = manifest.iconDefinitions ?? {}
const fileNames = manifest.fileNames ?? {}
const fileExtensions = manifest.fileExtensions ?? {}

const extensionIconFallbacks: Record<string, string> = {
  css: 'css.svg',
  env: 'tune.svg',
  js: 'javascript.svg',
  jsx: 'javascript.svg',
  ts: 'typescript.svg',
  tsx: 'react_ts.svg',
}

function iconFileName(iconId: string | undefined): string | undefined {
  const id = iconId || manifest.file
  const iconDefinition = id ? iconDefinitions[id] : undefined
  const iconPath = iconDefinition?.iconPath
  if (!iconPath) return undefined
  return iconPath.split('/').pop()
}

function extensionCandidates(filename: string): string[] {
  const parts = filename.toLowerCase().split('.').filter(Boolean)
  if (filename.startsWith('.') && parts.length === 1) return [parts[0]]
  if (parts.length <= 1) return []
  const candidates: string[] = []
  for (let index = 1; index < parts.length; index += 1) {
    candidates.push(parts.slice(index).join('.'))
  }
  return candidates
}

function resolveFileIconId(name: string): string | undefined {
  const exact = fileNames[name]
  if (exact) return exact

  const lowerName = name.toLowerCase()
  const lowerExact = fileNames[lowerName]
  if (lowerExact) return lowerExact

  for (const extension of extensionCandidates(lowerName)) {
    const iconId = fileExtensions[extension]
    if (iconId) return iconId
  }

  return manifest.file
}

export function resolveLocalFileIconName(entry: LocalFileEntry): string | undefined {
  if (entry.type === 'dir') return undefined
  return iconFileName(resolveFileIconId(entry.name))
}

export function resolveLocalFileIconUrl(entry: LocalFileEntry): string | undefined {
  if (entry.type === 'dir') return undefined
  const iconName = resolveLocalFileIconName(entry)
  if (entry.type === 'file') {
    for (const extension of extensionCandidates(entry.name)) {
      const fallback = extensionIconFallbacks[extension]
      if (fallback) return iconAssetUrls[fallback]
    }
  }
  if (!iconName) return fileIconUrl
  return iconAssetUrls[iconName] ?? fileIconUrl
}

export function localFileDecorationClass(entry: LocalFileEntry): string {
  const lowerName = entry.name.toLowerCase()
  if (lowerName.startsWith('.')) return 'local-file-tree__row--muted'
  if (lowerName === 'node_modules' || lowerName === 'vendor' || lowerName === 'dist' || lowerName === 'build') {
    return 'local-file-tree__row--muted'
  }
  if (lowerName === 'src') return 'local-file-tree__row--accent'
  return ''
}
