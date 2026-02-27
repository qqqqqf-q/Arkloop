import type { IncomingMessage, ServerResponse } from 'node:http';
import type { BrowserPool } from '../pool/browser-pool.js';
import type { StorageClient } from '../storage/minio-client.js';

export type ExtractMode = 'text' | 'accessibility' | 'html_clean';

export interface ExtractRequest {
  mode: ExtractMode;
  selector?: string | null;
}

export interface ExtractResponse {
  content: string;
  word_count: number;
}

// handleExtract 在 AS-7.4 中完整实现。
export async function handleExtract(
  _req: IncomingMessage,
  res: ServerResponse,
  _body: ExtractRequest,
  _pool: BrowserPool,
  _storage: StorageClient,
): Promise<void> {
  res.writeHead(501, { 'Content-Type': 'application/json' });
  res.end(JSON.stringify({ code: 'not_implemented', message: 'extract not yet implemented (AS-7.4)' }));
}
