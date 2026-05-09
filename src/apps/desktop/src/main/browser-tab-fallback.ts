function escapeHtml(value: string): string {
  return value
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&#39;')
}

export function buildBrowserTabFailureFallbackHtml(
  failedUrl: string,
  errorDescription: string,
): string {
  const safeUrl = escapeHtml(failedUrl || 'Unknown URL')
  const safeError = escapeHtml(errorDescription || 'Unknown error')

  return `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>Unable to open this page</title>
    <style>
      :root {
        color-scheme: dark light;
        font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      }
      body {
        margin: 0;
        min-height: 100vh;
        display: flex;
        align-items: center;
        justify-content: center;
        background: #0f1115;
        color: #f5f7fb;
      }
      .card {
        width: min(560px, calc(100vw - 32px));
        border-radius: 20px;
        border: 1px solid rgba(255,255,255,0.08);
        background: rgba(23, 28, 36, 0.92);
        padding: 28px;
        box-sizing: border-box;
        box-shadow: 0 16px 40px rgba(0,0,0,0.35);
      }
      h1 {
        margin: 0 0 12px;
        font-size: 24px;
        line-height: 1.2;
      }
      p {
        margin: 0;
        color: rgba(245,247,251,0.72);
        line-height: 1.6;
      }
      .meta {
        margin-top: 18px;
        display: grid;
        gap: 12px;
      }
      .label {
        display: block;
        margin-bottom: 6px;
        font-size: 12px;
        text-transform: uppercase;
        letter-spacing: 0.08em;
        color: rgba(245,247,251,0.48);
      }
      code {
        display: block;
        overflow-wrap: anywhere;
        border-radius: 12px;
        background: rgba(255,255,255,0.04);
        padding: 12px 14px;
        color: #f5f7fb;
        font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
        font-size: 13px;
      }
    </style>
  </head>
  <body>
    <main class="card">
      <h1>Unable to open this page</h1>
      <p>Try editing the address or retrying the request.</p>
      <div class="meta">
        <div>
          <span class="label">URL</span>
          <code>${safeUrl}</code>
        </div>
        <div>
          <span class="label">Error</span>
          <code>${safeError}</code>
        </div>
      </div>
    </main>
  </body>
</html>`
}

export function buildBrowserTabFailureFallbackUrl(
  failedUrl: string,
  errorDescription: string,
): string {
  const html = buildBrowserTabFailureFallbackHtml(failedUrl, errorDescription)
  return `data:text/html;charset=utf-8,${encodeURIComponent(html)}`
}
