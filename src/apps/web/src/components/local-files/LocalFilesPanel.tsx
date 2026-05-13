import { memo, useCallback, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react'
import { ArrowLeft, ArrowRight, List, Search } from 'lucide-react'
import { rightPanelIconButtonCls, rightPanelIconSize } from '../rightPanelControls'
import { ResourcePreviewPanel } from '../resource-preview/ResourcePreviewPanel'
import type { LocalFileResourceRef } from '../resource-preview/types'
import { LocalFileSearchPanel } from './LocalFileSearchPanel'
import { LocalFileTree } from './LocalFileTree'
import './LocalFilesPanel.css'

type Props = {
  rootPath: string
  accessToken: string
  previewResource?: LocalFileResourceRef | null
  onPreviewResourceChange?: (resource: LocalFileResourceRef | null) => void
  onPinResource?: (resource: LocalFileResourceRef) => void
}

export const LocalFilesPanel = memo(function LocalFilesPanel({ rootPath, accessToken, previewResource, onPreviewResourceChange, onPinResource }: Props) {
  const [browserOpen, setBrowserOpen] = useState(true)
  const [searchOpen, setSearchOpen] = useState(false)
  const [searchQuery, setSearchQuery] = useState('')
  const [browserWidth, setBrowserWidth] = useState(280)
  const [selection, setSelection] = useState<{ rootPath: string; file: LocalFileResourceRef } | null>(null)
  const contentRef = useRef<HTMLDivElement | null>(null)
  const controlledSelection = onPreviewResourceChange !== undefined
  const selectedFile = controlledSelection
    ? (previewResource?.rootPath === rootPath ? previewResource : null)
    : (selection?.rootPath === rootPath ? selection.file : null)

  const handleOpenFile = useCallback((ref: LocalFileResourceRef) => {
    if (controlledSelection) {
      onPreviewResourceChange(ref)
    } else {
      setSelection({ rootPath, file: ref })
    }
  }, [controlledSelection, onPreviewResourceChange, rootPath])

  const handleClosePreview = useCallback(() => {
    if (controlledSelection) {
      onPreviewResourceChange(null)
    } else {
      setSelection(null)
    }
  }, [controlledSelection, onPreviewResourceChange])

  const handleToggleBrowser = useCallback(() => {
    if (searchOpen) {
      setSearchOpen(false)
      setBrowserOpen(true)
      return
    }
    setBrowserOpen((open) => !open)
  }, [searchOpen])

  const handleToggleSearch = useCallback(() => {
    setBrowserOpen(true)
    setSearchOpen((open) => !open)
  }, [])

  const handleBrowserResizeStart = useCallback((event: ReactPointerEvent<HTMLDivElement>) => {
    event.preventDefault()
    const content = contentRef.current
    if (!content) return
    const pointerId = event.pointerId
    event.currentTarget.setPointerCapture(pointerId)
    const rect = content.getBoundingClientRect()

    const handlePointerMove = (moveEvent: PointerEvent) => {
      const next = Math.min(Math.max(moveEvent.clientX - rect.left, 220), Math.max(260, rect.width - 320))
      setBrowserWidth(next)
    }
    const stopResize = () => {
      window.removeEventListener('pointermove', handlePointerMove)
      window.removeEventListener('pointerup', stopResize)
      window.removeEventListener('pointercancel', stopResize)
    }

    window.addEventListener('pointermove', handlePointerMove)
    window.addEventListener('pointerup', stopResize)
    window.addEventListener('pointercancel', stopResize)
  }, [])

  return (
    <section className="local-files-panel" aria-label="Files">
      <div className="local-files-panel__toolbar">
        <button
          type="button"
          title="Browse Files"
          aria-pressed={browserOpen && !searchOpen}
          onClick={handleToggleBrowser}
          className={`${rightPanelIconButtonCls} local-files-panel__tool${browserOpen && !searchOpen ? ' local-files-panel__tool--active' : ''}`}
        >
          <List size={rightPanelIconSize} />
        </button>
        <button
          type="button"
          title="Search"
          aria-pressed={searchOpen}
          onClick={handleToggleSearch}
          className={`${rightPanelIconButtonCls} local-files-panel__tool${searchOpen ? ' local-files-panel__tool--active' : ''}`}
        >
          <Search size={rightPanelIconSize} />
        </button>
        <button type="button" title="Back" disabled className={`${rightPanelIconButtonCls} local-files-panel__tool`}>
          <ArrowLeft size={rightPanelIconSize} />
        </button>
        <button type="button" title="Forward" disabled className={`${rightPanelIconButtonCls} local-files-panel__tool`}>
          <ArrowRight size={rightPanelIconSize} />
        </button>
      </div>
      <div ref={contentRef} className="local-files-panel__content">
        <div
          className={`local-files-panel__browser${browserOpen ? '' : ' local-files-panel__browser--closed'}`}
          style={{ flexBasis: browserOpen ? browserWidth : 0 }}
        >
          {searchOpen ? (
            <div className="local-files-panel__search-view">
              <div className="local-files-panel__search">
                <Search size={rightPanelIconSize} aria-hidden="true" />
                <input
                  value={searchQuery}
                  onChange={(event) => setSearchQuery(event.target.value)}
                  placeholder="Search"
                  className="local-files-panel__search-input"
                />
              </div>
              <LocalFileSearchPanel
                rootPath={rootPath}
                query={searchQuery}
                selectedPath={selectedFile?.path}
                onOpenFile={handleOpenFile}
                onPinFile={onPinResource}
              />
            </div>
          ) : (
            <LocalFileTree
              rootPath={rootPath}
              selectedPath={selectedFile?.path}
              onOpenFile={handleOpenFile}
              onPinFile={onPinResource}
            />
          )}
        </div>
        {browserOpen ? (
          <div
            role="separator"
            aria-orientation="vertical"
            title="Resize"
            onPointerDown={handleBrowserResizeStart}
            className="local-files-panel__resizer"
          />
        ) : null}
        <div className="local-files-panel__preview">
          {selectedFile ? (
            <ResourcePreviewPanel resource={selectedFile} accessToken={accessToken} onClose={handleClosePreview} />
          ) : (
            <div className="local-files-panel__empty">No file selected</div>
          )}
        </div>
      </div>
    </section>
  )
})
