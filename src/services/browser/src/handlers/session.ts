import type { IncomingMessage, ServerResponse } from 'node:http';
import type { BrowserPool } from '../pool/browser-pool.js';
import type { StorageClient } from '../storage/minio-client.js';

// handleSessionClose 处理 DELETE /v1/sessions/:id
// 清除 session 的 storageState、关闭活跃 context、删除关联截图。
// 完整实现在 AS-7.4 中完成。
export async function handleSessionClose(
  _req: IncomingMessage,
  res: ServerResponse,
  _sessionId: string,
  _pool: BrowserPool,
  _storage: StorageClient,
): Promise<void> {
  res.writeHead(501, { 'Content-Type': 'application/json' });
  res.end(JSON.stringify({ code: 'not_implemented', message: 'session close not yet implemented (AS-7.4)' }));
}
