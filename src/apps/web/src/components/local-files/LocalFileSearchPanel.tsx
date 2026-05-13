import { useEffect, useMemo, useState } from 'react'
import { Search } from 'lucide-react'
import { getDesktopApi, type LocalFileEntry } from '@arkloop/shared/desktop'
import type { LocalFileResourceRef } from '../resource-preview/types'
import { resolveLocalFileIconUrl } from './fileIconResolver'
import './LocalFileSearchPanel.css'

type SearchResult = LocalFileEntry & {
  parentPath: string
}

type Props = {
  rootPath: string
  query: string
  selectedPath?: string
  onOpenFile: (ref: LocalFileResourceRef) => void
  onPinFile?: (ref: LocalFileResourceRef) => void
}

function resultPath(entry: LocalFileEntry): string {
  return entry.path.replace(/^[/\\]+/g, '')
}

function parentPath(path: string): string {
  const parts = resultPath({ name: '', path, type: 'file' }).split('/')
  parts.pop()
  return parts.join('/')
}

function SearchResultIcon({ entry }: { entry: LocalFileEntry }) {
  const iconUrl = resolveLocalFileIconUrl(entry)
  return iconUrl ? (
    <img className="local-file-search__icon-image" src={iconUrl} alt="" draggable={false} aria-hidden="true" />
  ) : (
    <span className="local-file-search__icon-fallback" aria-hidden="true" />
  )
}

async function searchDirectory(rootPath: string, query: string): Promise<SearchResult[]> {
  const fs = getDesktopApi()?.fs
  if (!fs) throw new Error('desktop fs is unavailable')

  const results: SearchResult[] = []
  const queue: string[] = [undefined as unknown as string]

  while (queue.length > 0 && results.length < 200) {
    const currentPath = queue.shift()
    const result = await fs.listDir(rootPath, currentPath)
    if ('error' in result) continue

    for (const entry of result.entries) {
      const lowerName = entry.name.toLowerCase()
      const lowerPath = resultPath(entry).toLowerCase()
      if (lowerName.includes(query) || lowerPath.includes(query)) {
        results.push({ ...entry, parentPath: parentPath(entry.path) })
      }
      if (entry.type === 'dir' && !entry.name.startsWith('.') && entry.name !== 'node_modules') {
        queue.push(entry.path)
      }
      if (results.length >= 200) break
    }
  }

  return results
}

export function LocalFileSearchPanel({ rootPath, query, selectedPath, onOpenFile, onPinFile }: Props) {
  const normalizedQuery = useMemo(() => query.trim().toLowerCase(), [query])
  const [state, setState] = useState<{ query: string; status: 'idle' | 'loading' | 'ready' | 'error'; results: SearchResult[] }>({
    query: '',
    status: 'idle',
    results: [],
  })

  useEffect(() => {
    let cancelled = false
    queueMicrotask(() => {
      if (cancelled) return
      if (!rootPath || !normalizedQuery) {
        setState({ query: normalizedQuery, status: 'idle', results: [] })
        return
      }

      setState((current) => ({ query: normalizedQuery, status: 'loading', results: current.query === normalizedQuery ? current.results : [] }))
      void searchDirectory(rootPath, normalizedQuery)
        .then((results) => {
          if (!cancelled) setState({ query: normalizedQuery, status: 'ready', results })
        })
        .catch(() => {
          if (!cancelled) setState({ query: normalizedQuery, status: 'error', results: [] })
        })
    })
    return () => {
      cancelled = true
    }
  }, [normalizedQuery, rootPath])

  if (!normalizedQuery) {
    return (
      <div className="local-file-search__empty">
        <Search size={16} aria-hidden="true" />
        <span>Search files</span>
      </div>
    )
  }

  if (state.status === 'loading') {
    return <div className="local-file-search__meta">Searching</div>
  }

  if (state.status === 'error') {
    return <div className="local-file-search__meta">Unable to search</div>
  }

  if (state.results.length === 0) {
    return <div className="local-file-search__meta">No results</div>
  }

  return (
    <div className="local-file-search__results">
      {state.results.map((entry) => {
        const isFile = entry.type === 'file'
        const selected = isFile && entry.path === selectedPath
        const resource: LocalFileResourceRef = { kind: 'local-file', rootPath, path: entry.path, name: entry.name }
        return (
          <button
            key={`${entry.type}:${entry.path}`}
            type="button"
            disabled={!isFile}
            title={entry.path}
            className={`local-file-search__row${selected ? ' local-file-search__row--selected' : ''}`}
            onClick={() => {
              if (!isFile) return
              onOpenFile(resource)
            }}
            onDoubleClick={() => {
              if (!isFile) return
              onPinFile?.(resource)
            }}
          >
            <SearchResultIcon entry={entry} />
            <span className="local-file-search__text">
              <span className="local-file-search__name">{entry.name}</span>
              {entry.parentPath ? <span className="local-file-search__path">{entry.parentPath}</span> : null}
            </span>
          </button>
        )
      })}
    </div>
  )
}
