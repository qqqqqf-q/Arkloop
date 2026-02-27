import type { ServerResponse } from 'node:http';
import type { RequestContext } from '../server.js';
import type { BrowserPool } from '../pool/browser-pool.js';
import type { StorageClient } from '../storage/minio-client.js';

export async function handleSessionClose(
  res: ServerResponse,
  ctx: RequestContext,
  pool: BrowserPool,
  _storage: StorageClient,
): Promise<void> {
  try {
    await pool.closeAndDeleteContext(ctx.sessionId, ctx.orgId);
    res.writeHead(204);
    res.end();
  } catch (err) {
    const message = err instanceof Error ? err.message : 'session close failed';
    res.writeHead(500, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({ code: 'internal_error', message }));
  }
}
