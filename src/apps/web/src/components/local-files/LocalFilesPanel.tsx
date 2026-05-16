import { memo, useCallback, useEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react'
import { ArrowLeft, ArrowRight, FileText, List, MoreHorizontal, Search, X } from 'lucide-react'
import { rightPanelIconButtonCls, rightPanelIconSize } from '../rightPanelControls'
import { DropdownAction } from '../DropdownAction'
import { ResourcePreviewPanel } from '../resource-preview/ResourcePreviewPanel'
import { isPreviewModeToggleable } from '../resource-preview/rendererKind'
import type { LocalFileResourceRef } from '../resource-preview/types'
import { SettingsSegmentedControl } from '../settings/_SettingsSegmentedControl'
import { filenameFromPath, normalizeMimeType } from '../resource-preview/mime'
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
  const [previewMode, setPreviewMode] = useState<'preview' | 'source'>('preview')
  const [actionsOpen, setActionsOpen] = useState(false)
  const [selection, setSelection] = useState<{ rootPath: string; file: LocalFileResourceRef } | null>(null)
  const contentRef = useRef<HTMLDivElement | null>(null)
  const actionsRef = useRef<HTMLDivElement | null>(null)
  const controlledSelection = onPreviewResourceChange !== undefined
  const selectedFile = controlledSelection
    ? (previewResource?.rootPath === rootPath ? previewResource : null)
    : (selection?.rootPath === rootPath ? selection.file : null)
  const selectedFilename = selectedFile?.filename ?? selectedFile?.name ?? filenameFromPath(selectedFile?.path ?? '')
  const selectedMimeType = selectedFile ? normalizeMimeType(selectedFile.mimeType, selectedFilename) : ''
  const canTogglePreviewMode = selectedFile ? isPreviewModeToggleable({ filename: selectedFilename, mimeType: selectedMimeType }) : false
  const previewLabel = 'Preview'
  const markdownLabel = 'Markdown'
  const closeLabel = 'Close'

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
    setActionsOpen(false)
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

  useEffect(() => {
    setPreviewMode('preview')
    setActionsOpen(false)
  }, [selectedFile?.path, selectedFile?.rootPath])

  useEffect(() => {
    if (!actionsOpen) return
    const handlePointerDown = (event: PointerEvent) => {
      if (actionsRef.current?.contains(event.target as Node)) return
      setActionsOpen(false)
    }
    window.addEventListener('pointerdown', handlePointerDown)
    return () => window.removeEventListener('pointerdown', handlePointerDown)
  }, [actionsOpen])

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
        {selectedFile ? (
          <>
            <div className="local-files-panel__toolbar-title">
              <FileText size={rightPanelIconSize} aria-hidden="true" />
              <span>{selectedFilename}</span>
            </div>
            <div className="local-files-panel__toolbar-actions">
              {canTogglePreviewMode ? (
                <SettingsSegmentedControl<'preview' | 'source'>
                  value={previewMode}
                  onChange={setPreviewMode}
                  options={[
                    { value: 'preview', label: previewLabel, ariaLabel: previewLabel },
                    { value: 'source', label: markdownLabel, ariaLabel: markdownLabel },
                  ]}
                />
              ) : null}
              <div ref={actionsRef} className="local-files-panel__actions">
                <button
                  type="button"
                  title="More"
                  aria-label="More"
                  aria-expanded={actionsOpen}
                  onClick={() => setActionsOpen((open) => !open)}
                  className={`${rightPanelIconButtonCls} local-files-panel__tool${actionsOpen ? ' local-files-panel__tool--active' : ''}`}
                >
                  <MoreHorizontal size={rightPanelIconSize} />
                </button>
                <div className="local-files-panel__actions-menu" data-open={actionsOpen}>
                  <DropdownAction icon={<X size={14} />} label={closeLabel} onClick={handleClosePreview} />
                </div>
              </div>
            </div>
          </>
        ) : null}
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
            <ResourcePreviewPanel
              resource={selectedFile}
              accessToken={accessToken}
              chrome="content-only"
              mode={canTogglePreviewMode ? previewMode : 'preview'}
              onModeChange={setPreviewMode}
              onClose={handleClosePreview}
            />
          ) : (
            <div className="local-files-panel__empty">No file selected</div>
          )}
        </div>
      </div>
    </section>
  )
})
