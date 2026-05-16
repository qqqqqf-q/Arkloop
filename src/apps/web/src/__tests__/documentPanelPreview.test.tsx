import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it, vi } from 'vitest'

import { PreviewResourceView } from '../components/resource-preview/PreviewResourceView'
import type { PreviewResource } from '../components/resource-preview/types'
import type { ArtifactRef } from '../storage'

vi.mock('../components/ArtifactHtmlPreview', async () => {
  const { createElement } = await import('react')
  return {
    ArtifactHtmlPreview: ({ artifact }: { artifact: ArtifactRef }) => createElement('div', {
      'data-artifact-html-preview': artifact.key,
      'data-title': artifact.title ?? artifact.filename,
    }),
  }
})

describe('ResourcePreviewPanel artifact preview', () => {
  it('Markdown 文档中的 html artifact 应继续内联渲染', () => {
    const markdownResource: PreviewResource = {
      source: 'artifact',
      ref: {
        kind: 'artifact',
        key: 'doc.md',
        filename: 'doc.md',
        mimeType: 'text/markdown',
        size: 10,
      },
      filename: 'doc.md',
      mimeType: 'text/markdown',
      size: 10,
      text: '[预览](artifact:preview.html)',
    }
    const htmlArtifact: ArtifactRef = {
      key: 'preview.html',
      filename: 'preview.html',
      size: 20,
      mime_type: 'text/html',
    }

    const html = renderToStaticMarkup(
      <PreviewResourceView
        resource={markdownResource}
        artifacts={[htmlArtifact]}
        accessToken="token"
      />,
    )

    expect(html).toContain('data-artifact-html-preview="preview.html"')
    expect(html).toContain('data-title="preview.html"')
  })
})
