import type { ServerResponse } from 'node:http';
import type { RequestContext } from '../server.js';
import type { BrowserPool } from '../pool/browser-pool.js';
import type { SessionManager } from '../pool/session-manager.js';
import type { StorageClient } from '../storage/minio-client.js';
import { getActivePage, capturePageState } from './shared.js';

export type WaitUntil = 'load' | 'domcontentloaded' | 'networkidle';

export interface NavigateRequest {
  url: string;
  wait_until?: WaitUntil;
  timeout_ms?: number;
  fresh_session?: boolean;
}

export interface NavigateResponse {
  page_url: string;
  page_title: string;
  screenshot_url: string;
  content_text: string;
  accessibility_tree: string;
}

export async function handleNavigate(
  res: ServerResponse,
  body: NavigateRequest,
  ctx: RequestContext,
  pool: BrowserPool,
  sessionManager: SessionManager,
  storage: StorageClient,
  contentTextMaxLength: number,
): Promise<void> {
  if (!body.url) {
    writeError(res, 400, 'missing_param', 'url is required');
    return;
  }

  const waitUntil = body.wait_until ?? 'load';
  const timeout = body.timeout_ms ?? 30_000;
  const handle = await pool.getContext(ctx.orgId, ctx.sessionId, body.fresh_session);

  try {
    const page = await getActivePage(handle.context);
    await page.goto(body.url, { waitUntil, timeout });

    const state = await capturePageState(page, storage, ctx.runId, contentTextMaxLength);

    // 持久化 storageState
    const storageState = await handle.context.storageState();
    await sessionManager.saveState({ orgId: ctx.orgId, sessionId: ctx.sessionId }, storageState);

    const payload: NavigateResponse = {
      page_url: state.pageUrl,
      page_title: state.pageTitle,
      screenshot_url: state.screenshotUrl,
      content_text: state.contentText,
      accessibility_tree: state.accessibilityTree,
    };
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify(payload));
  } catch (err) {
    const message = err instanceof Error ? err.message : 'navigate failed';
    const code = isTimeoutError(err) ? 'timeout' : 'browser_error';
    writeError(res, code === 'timeout' ? 504 : 500, code, message);
  } finally {
    pool.releaseContext(ctx.sessionId);
  }
}

function isTimeoutError(err: unknown): boolean {
  if (!(err instanceof Error)) return false;
  return err.name === 'TimeoutError' || err.message.includes('Timeout');
}

function writeError(res: ServerResponse, status: number, code: string, message: string): void {
  res.writeHead(status, { 'Content-Type': 'application/json' });
  res.end(JSON.stringify({ code, message }));
}
