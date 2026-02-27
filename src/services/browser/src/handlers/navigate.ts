import type { IncomingMessage, ServerResponse } from 'node:http';
import type { BrowserPool } from '../pool/browser-pool.js';
import type { StorageClient } from '../storage/minio-client.js';

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

// handleNavigate 在 AS-7.4 中完整实现。
export async function handleNavigate(
  _req: IncomingMessage,
  res: ServerResponse,
  _body: NavigateRequest,
  _pool: BrowserPool,
  _storage: StorageClient,
): Promise<void> {
  res.writeHead(501, { 'Content-Type': 'application/json' });
  res.end(JSON.stringify({ code: 'not_implemented', message: 'navigate not yet implemented (AS-7.4)' }));
}
