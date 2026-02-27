import type { IncomingMessage, ServerResponse } from 'node:http';
import type { BrowserPool } from '../pool/browser-pool.js';
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

// handleScreenshot 在 AS-7.4 中完整实现。
export async function handleScreenshot(
  _req: IncomingMessage,
  res: ServerResponse,
  _body: ScreenshotRequest,
  _pool: BrowserPool,
  _storage: StorageClient,
): Promise<void> {
  res.writeHead(501, { 'Content-Type': 'application/json' });
  res.end(JSON.stringify({ code: 'not_implemented', message: 'screenshot not yet implemented (AS-7.4)' }));
}
