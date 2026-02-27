import type { ServerResponse } from 'node:http';
import type { RequestContext } from '../server.js';
import type { BrowserPool } from '../pool/browser-pool.js';
import type { SessionManager } from '../pool/session-manager.js';
import { formatAccessibilityTree, truncateText } from './shared.js';

export type ExtractMode = 'text' | 'accessibility' | 'html_clean';

export interface ExtractRequest {
  mode: ExtractMode;
  selector?: string | null;
}

export interface ExtractResponse {
  content: string;
  word_count: number;
}

const VALID_MODES = new Set<ExtractMode>(['text', 'accessibility', 'html_clean']);

// 去除 script/style/svg/noscript 及其内容，保留语义 HTML 结构
const STRIP_TAGS_RE = /<(script|style|svg|noscript)\b[^>]*>[\s\S]*?<\/\1>/gi;

export async function handleExtract(
  res: ServerResponse,
  body: ExtractRequest,
  ctx: RequestContext,
  pool: BrowserPool,
  _sessionManager: SessionManager,
  contentTextMaxLength: number,
): Promise<void> {
  if (!body.mode || !VALID_MODES.has(body.mode)) {
    writeError(res, 400, 'invalid_mode', `mode must be one of: ${[...VALID_MODES].join(', ')}`);
    return;
  }

  const handle = await pool.getContext(ctx.orgId, ctx.sessionId);

  try {
    const pages = handle.context.pages();
    if (pages.length === 0) {
      writeError(res, 400, 'no_active_page', 'no active page, call navigate first');
      return;
    }
    const page = pages[0];

    let content: string;

    switch (body.mode) {
      case 'text': {
        const target = body.selector ?? 'body';
        const rawText = await page.innerText(target);
        content = truncateText(rawText, contentTextMaxLength);
        break;
      }

      case 'accessibility': {
        const snapshot = await page.accessibility.snapshot();
        content = snapshot ? formatAccessibilityTree(snapshot) : '';
        break;
      }

      case 'html_clean': {
        let html: string;
        if (body.selector) {
          html = await page.locator(body.selector).innerHTML();
        } else {
          html = await page.content();
        }
        // 去除 script/style/svg/noscript，保留其余语义标签
        content = html
          .replace(STRIP_TAGS_RE, '')
          .replace(/\s{2,}/g, ' ')
          .trim();
        content = truncateText(content, contentTextMaxLength);
        break;
      }
    }

    // 用空格分隔统计词数（中文按字符粒度，英文按空格）
    const wordCount = content.split(/\s+/).filter(Boolean).length;

    const payload: ExtractResponse = { content, word_count: wordCount };
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify(payload));
  } catch (err) {
    const message = err instanceof Error ? err.message : 'extract failed';
    writeError(res, 500, 'browser_error', message);
  } finally {
    pool.releaseContext(ctx.sessionId);
  }
}

function writeError(res: ServerResponse, status: number, code: string, message: string): void {
  res.writeHead(status, { 'Content-Type': 'application/json' });
  res.end(JSON.stringify({ code, message }));
}
