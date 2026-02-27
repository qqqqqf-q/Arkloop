import type { IncomingMessage, ServerResponse } from 'node:http';
import type { BrowserPool } from '../pool/browser-pool.js';
import type { StorageClient } from '../storage/minio-client.js';

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

// InteractResponse 格式与 NavigateResponse 相同（截图 + 页面状态）。
export interface InteractResponse {
  page_url: string;
  page_title: string;
  screenshot_url: string;
  content_text: string;
  accessibility_tree: string;
}

// handleInteract 在 AS-7.4 中完整实现。
export async function handleInteract(
  _req: IncomingMessage,
  res: ServerResponse,
  _body: InteractRequest,
  _pool: BrowserPool,
  _storage: StorageClient,
): Promise<void> {
  res.writeHead(501, { 'Content-Type': 'application/json' });
  res.end(JSON.stringify({ code: 'not_implemented', message: 'interact not yet implemented (AS-7.4)' }));
}
