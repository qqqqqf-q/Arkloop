import type { ServerResponse } from 'node:http';
import type { RequestContext } from '../server.js';
import type { BrowserPool } from '../pool/browser-pool.js';
import type { SessionManager } from '../pool/session-manager.js';
import type { StorageClient } from '../storage/minio-client.js';

export interface ScreenshotRequest {
  full_page?: boolean;
  selector?: string | null;
  quality?: number;
}

export interface ScreenshotResponse {
  screenshot_url: string;
  width: number;
  height: number;
}

export async function handleScreenshot(
  res: ServerResponse,
  body: ScreenshotRequest,
  ctx: RequestContext,
  pool: BrowserPool,
  _sessionManager: SessionManager,
  storage: StorageClient,
): Promise<void> {
  const handle = await pool.getContext(ctx.orgId, ctx.sessionId);

  try {
    const pages = handle.context.pages();
    if (pages.length === 0) {
      writeError(res, 400, 'no_active_page', 'no active page, call navigate first');
      return;
    }
    const page = pages[0];

    const quality = body.quality ?? 80;
    // quality 仅 jpeg 支持，png 忽略
    const useJpeg = quality < 100;

    let imageBuffer: Buffer;
    let width: number;
    let height: number;

    if (body.selector) {
      const locator = page.locator(body.selector);
      const box = await locator.boundingBox();
      width = box ? Math.round(box.width) : 0;
      height = box ? Math.round(box.height) : 0;
      const raw = await locator.screenshot({
        type: useJpeg ? 'jpeg' : 'png',
        quality: useJpeg ? quality : undefined,
      });
      imageBuffer = Buffer.from(raw);
    } else {
      const raw = await page.screenshot({
        fullPage: body.full_page ?? false,
        type: useJpeg ? 'jpeg' : 'png',
        quality: useJpeg ? quality : undefined,
      });
      imageBuffer = Buffer.from(raw);
      const viewport = page.viewportSize();
      width = viewport?.width ?? 0;
      height = viewport?.height ?? 0;
      // 全页截图时高度取实际内容高度
      if (body.full_page) {
        const scrollHeight = await page.evaluate(() => document.documentElement.scrollHeight);
        height = scrollHeight;
      }
    }

    const step = Date.now();
    const screenshotUrl = await storage.uploadScreenshot(ctx.runId, step, imageBuffer);

    const payload: ScreenshotResponse = {
      screenshot_url: screenshotUrl,
      width,
      height,
    };
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify(payload));
  } catch (err) {
    const message = err instanceof Error ? err.message : 'screenshot failed';
    writeError(res, 500, 'browser_error', message);
  } finally {
    pool.releaseContext(ctx.sessionId);
  }
}

function writeError(res: ServerResponse, status: number, code: string, message: string): void {
  res.writeHead(status, { 'Content-Type': 'application/json' });
  res.end(JSON.stringify({ code, message }));
}
