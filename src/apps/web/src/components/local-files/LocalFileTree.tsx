import { memo, useCallback, useEffect, useMemo, useState } from 'react'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { getDesktopApi, type LocalFileEntry } from '@arkloop/shared/desktop'
import type { LocalFileResourceRef } from '../resource-preview/types'
import './LocalFileTree.css'
import { localFileDecorationClass, resolveLocalFileIconUrl } from './fileIconResolver'

export type LocalFileTreeProps = {
  rootPath: string
  onOpenFile: (ref: LocalFileResourceRef) => void
  onPinFile?: (ref: LocalFileResourceRef) => void
  selectedPath?: string
  searchQuery?: string
  className?: string
}

type DirectoryState =
  | { status: 'loading'; entries: LocalFileEntry[] }
  | { status: 'ready'; entries: LocalFileEntry[] }
  | { status: 'error'; entries: LocalFileEntry[] }

type TreeState = {
  rootPath: string
  expanded: Set<string>
  directories: Record<string, DirectoryState>
}

const ROOT_KEY = ''
const treeIndentBase = 5
const treeIndentStep = 12
const treeMetaOffset = 24
const directoryCache = new Map<string, DirectoryState>()
const directoryCacheInsertOrder: string[] = []
const MAX_CACHE_ENTRIES = 200

function cacheSet(key: string, value: DirectoryState): void {
  if (directoryCache.has(key)) {
    directoryCache.set(key, value)
    return
  }
  while (directoryCacheInsertOrder.length >= MAX_CACHE_ENTRIES) {
    const oldest = directoryCacheInsertOrder.shift()
    if (oldest) directoryCache.delete(oldest)
  }
  directoryCache.set(key, value)
  directoryCacheInsertOrder.push(key)
}

function cacheKey(rootPath: string, subPath?: string): string {
  return `${rootPath}\n${directoryKey(subPath)}`
}

function cachedDirectories(rootPath: string): Record<string, DirectoryState> {
  if (!rootPath) return {}
  const state = directoryCache.get(cacheKey(rootPath))
  return state ? { [ROOT_KEY]: state } : { [ROOT_KEY]: { status: 'loading', entries: [] } }
}

function LocalFileGlyph({ entry, expanded }: { entry: LocalFileEntry; expanded: boolean }) {
  if (entry.type === 'dir') {
    return expanded ? <ChevronDown size={16} /> : <ChevronRight size={16} />
  }

  const iconUrl = resolveLocalFileIconUrl(entry)
  return iconUrl ? (
    <img className="local-file-tree__icon-image" src={iconUrl} alt="" draggable={false} aria-hidden="true" />
  ) : (
    <span className="local-file-tree__icon-fallback" aria-hidden="true" />
  )
}

function directoryKey(path: string | undefined): string {
  return path?.replace(/^\/+|\/+$/g, '') ?? ROOT_KEY
}

function displayPath(rootPath: string, path?: string): string {
  const cleanRoot = rootPath.replace(/[\\/]+$/g, '')
  const cleanPath = path?.replace(/^[/\\]+/g, '') ?? ''
  return cleanPath ? `${cleanRoot}/${cleanPath}` : rootPath
}

function sortEntries(entries: LocalFileEntry[]): LocalFileEntry[] {
  return [...entries].sort((a, b) => {
    if (a.type !== b.type) return a.type === 'dir' ? -1 : 1
    return a.name.localeCompare(b.name, undefined, { numeric: true, sensitivity: 'base' })
  })
}

function entryMatchesSearch(entry: LocalFileEntry, query: string, directories: Record<string, DirectoryState>): boolean {
  if (!query) return true
  if (entry.name.toLowerCase().includes(query)) return true
  if (entry.type !== 'dir') return false
  const childState = directories[directoryKey(entry.path)]
  if (!childState || childState.status !== 'ready') return false
  return childState.entries.some((child) => entryMatchesSearch(child, query, directories))
}

export const LocalFileTree = memo(function LocalFileTree({ rootPath, onOpenFile, onPinFile, selectedPath, searchQuery = '', className }: LocalFileTreeProps) {
  const [treeState, setTreeState] = useState<TreeState>(() => ({
    rootPath,
    expanded: new Set([ROOT_KEY]),
    directories: cachedDirectories(rootPath),
  }))

  const expanded = useMemo(
    () => (treeState.rootPath === rootPath ? treeState.expanded : new Set([ROOT_KEY])),
    [rootPath, treeState.expanded, treeState.rootPath],
  )
  const directories = useMemo(
    () => (treeState.rootPath === rootPath ? treeState.directories : {}),
    [rootPath, treeState.directories, treeState.rootPath],
  )
  const rootState = directories[ROOT_KEY]
  const normalizedSearchQuery = searchQuery.trim().toLowerCase()

  const loadDirectory = useCallback((subPath?: string) => {
    const key = directoryKey(subPath)
    if (!rootPath) return

    setTreeState((current) => {
      const sameRoot = current.rootPath === rootPath
      const directoriesForRoot = sameRoot ? current.directories : {}
      const cached = directoryCache.get(cacheKey(rootPath, subPath))
      return {
        rootPath,
        expanded: sameRoot ? current.expanded : new Set([ROOT_KEY]),
        directories: {
          ...directoriesForRoot,
          [key]: { status: 'loading', entries: cached?.entries ?? directoriesForRoot[key]?.entries ?? [] },
        },
      }
    })

    const fs = getDesktopApi()?.fs
    if (!fs) {
      setTreeState((current) => ({
        ...current,
        directories: {
          ...current.directories,
          [key]: { status: 'error', entries: current.directories[key]?.entries ?? [] },
        },
      }))
      return
    }

    fs.listDir(rootPath, subPath)
      .then((result) => {
        const nextState: DirectoryState = { status: 'ready', entries: sortEntries(result.entries) }
        cacheSet(cacheKey(rootPath, subPath), nextState)
        setTreeState((current) => ({
          ...current,
          directories: {
            ...current.directories,
            [key]: nextState,
          },
        }))
      })
      .catch(() => {
        setTreeState((current) => ({
          ...current,
          directories: {
            ...current.directories,
            [key]: { status: 'error', entries: current.directories[key]?.entries ?? [] },
          },
        }))
      })
  }, [rootPath])

  useEffect(() => {
    if (!rootPath) return
    let cancelled = false
    queueMicrotask(() => {
      if (!cancelled) loadDirectory()
    })
    return () => {
      cancelled = true
    }
  }, [loadDirectory, rootPath])

  const toggleDirectory = useCallback((entry: LocalFileEntry) => {
    const key = directoryKey(entry.path)
    setTreeState((current) => {
      const sameRoot = current.rootPath === rootPath
      const next = new Set(sameRoot ? current.expanded : [ROOT_KEY])
      if (next.has(key)) {
        next.delete(key)
      } else {
        next.add(key)
      }
      return {
        rootPath,
        expanded: next,
        directories: sameRoot ? current.directories : {},
      }
    })
    if (!directories[key]) loadDirectory(entry.path)
  }, [directories, loadDirectory, rootPath])

  const resourceFromEntry = useCallback((entry: LocalFileEntry): LocalFileResourceRef => ({
      kind: 'local-file',
      rootPath,
      path: entry.path,
      name: entry.name,
    }), [rootPath])

  const openFile = useCallback((entry: LocalFileEntry) => {
    onOpenFile(resourceFromEntry(entry))
  }, [onOpenFile, resourceFromEntry])

  const pinFile = useCallback((entry: LocalFileEntry) => {
    onPinFile?.(resourceFromEntry(entry))
  }, [onPinFile, resourceFromEntry])

  const renderDirectoryRows = (parentPath: string | undefined, level: number) => {
    const key = directoryKey(parentPath)
    const state = directories[key]
    const entries = (state?.entries ?? []).filter((entry) => entryMatchesSearch(entry, normalizedSearchQuery, directories))
    const indent = treeIndentBase + level * treeIndentStep + treeMetaOffset

    if (!state || (state.status === 'loading' && entries.length === 0)) {
      return <div className="local-file-tree__meta" style={{ paddingLeft: indent }}>Loading</div>
    }

    if (state.status === 'error') {
      return <div className="local-file-tree__meta" style={{ paddingLeft: indent }}>Unable to read</div>
    }

    if (entries.length === 0) {
      return <div className="local-file-tree__meta" style={{ paddingLeft: indent }}>Empty</div>
    }

    return entries.map((entry) => {
      const entryDirectoryKey = directoryKey(entry.path)
      const isExpanded = expanded.has(entryDirectoryKey)
      const isDirectory = entry.type === 'dir'
      const decorationClass = localFileDecorationClass(entry)
      const selectedClass = !isDirectory && entry.path === selectedPath ? 'local-file-tree__row--selected' : ''

      return (
        <div key={`${entry.type}:${entry.path}`} className="local-file-tree__item">
          <button
            type="button"
            className={['local-file-tree__row', decorationClass, selectedClass].filter(Boolean).join(' ')}
            style={{ paddingLeft: treeIndentBase + level * treeIndentStep }}
            title={displayPath(rootPath, entry.path)}
            data-path={entry.path}
            aria-expanded={isDirectory ? isExpanded : undefined}
            onClick={() => (isDirectory ? toggleDirectory(entry) : openFile(entry))}
            onDoubleClick={() => {
              if (!isDirectory) pinFile(entry)
            }}
          >
            <span className="local-file-tree__glyph" aria-hidden="true">
              <LocalFileGlyph entry={entry} expanded={isExpanded} />
            </span>
            <span className="local-file-tree__name">{entry.name}</span>
          </button>
          {isDirectory && (isExpanded || normalizedSearchQuery) ? renderDirectoryRows(entry.path, level + 1) : null}
        </div>
      )
    })
  }

  return (
    <section className={['local-file-tree', className].filter(Boolean).join(' ')} aria-label="Local files">
      {rootPath ? (
        <div className="local-file-tree__body">
          {rootState ? renderDirectoryRows(undefined, 0) : null}
        </div>
      ) : (
        <div className="local-file-tree__meta local-file-tree__meta--standalone">No folder selected</div>
      )}
    </section>
  )
})
