import type { ServerResponse } from 'node:http';
import type { RequestContext } from '../server.js';
import type { BrowserPool } from '../pool/browser-pool.js';
import type { SessionManager } from '../pool/session-manager.js';
import type { StorageClient } from '../storage/minio-client.js';
import { getActivePage, capturePageState } from './shared.js';

export type InteractAction = 'click' | 'type' | 'scroll' | 'select' | 'hover';

export interface Coordinates {
  x: number;
  y: number;
}

export interface InteractRequest {
  action: InteractAction;
  selector?: string;
  value?: string;
  coordinates?: Coordinates;
  timeout_ms?: number;
}

export interface InteractResponse {
  page_url: string;
  page_title: string;
  screenshot_url: string;
  content_text: string;
  accessibility_tree: string;
}

const VALID_ACTIONS = new Set<InteractAction>(['click', 'type', 'scroll', 'select', 'hover']);

export async function handleInteract(
  res: ServerResponse,
  body: InteractRequest,
  ctx: RequestContext,
  pool: BrowserPool,
  sessionManager: SessionManager,
  storage: StorageClient,
  contentTextMaxLength: number,
): Promise<void> {
  if (!body.action || !VALID_ACTIONS.has(body.action)) {
    writeError(res, 400, 'invalid_action', `action must be one of: ${[...VALID_ACTIONS].join(', ')}`);
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
    const timeout = body.timeout_ms ?? 10_000;

    await performAction(page, body, timeout);

    // 等待页面稳定（可能触发了导航或网络请求）
    await page.waitForLoadState('domcontentloaded', { timeout: 5_000 }).catch(() => {});

    const state = await capturePageState(page, storage, ctx.runId, contentTextMaxLength);

    const storageState = await handle.context.storageState();
    await sessionManager.saveState({ orgId: ctx.orgId, sessionId: ctx.sessionId }, storageState);

    const payload: InteractResponse = {
      page_url: state.pageUrl,
      page_title: state.pageTitle,
      screenshot_url: state.screenshotUrl,
      content_text: state.contentText,
      accessibility_tree: state.accessibilityTree,
    };
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify(payload));
  } catch (err) {
    const message = err instanceof Error ? err.message : 'interact failed';
    const code = isTimeoutError(err) ? 'timeout' : 'browser_error';
    writeError(res, code === 'timeout' ? 504 : 500, code, message);
  } finally {
    pool.releaseContext(ctx.sessionId);
  }
}

async function performAction(
  page: Awaited<ReturnType<typeof getActivePage>>,
  body: InteractRequest,
  timeout: number,
): Promise<void> {
  switch (body.action) {
    case 'click':
      if (body.coordinates) {
        await page.mouse.click(body.coordinates.x, body.coordinates.y);
      } else if (body.selector) {
        await page.click(body.selector, { timeout });
      } else {
        throw new Error('click requires selector or coordinates');
      }
      break;

    case 'type':
      if (!body.selector) throw new Error('type requires selector');
      // fill 用于表单字段，直接替换值
      await page.fill(body.selector, body.value ?? '', { timeout });
      break;

    case 'scroll': {
      const delta = body.value ? parseInt(body.value, 10) : 500;
      if (body.coordinates) {
        await page.mouse.move(body.coordinates.x, body.coordinates.y);
        await page.mouse.wheel(0, delta);
      } else {
        await page.evaluate((dy) => window.scrollBy(0, dy), delta);
      }
      break;
    }

    case 'select':
      if (!body.selector) throw new Error('select requires selector');
      await page.selectOption(body.selector, body.value ?? '', { timeout });
      break;

    case 'hover':
      if (body.selector) {
        await page.hover(body.selector, { timeout });
      } else if (body.coordinates) {
        await page.mouse.move(body.coordinates.x, body.coordinates.y);
      } else {
        throw new Error('hover requires selector or coordinates');
      }
      break;
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
