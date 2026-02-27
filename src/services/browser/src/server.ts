import { createServer, type IncomingMessage, type ServerResponse } from 'node:http';
import { BrowserPool } from './pool/browser-pool.js';
import type { StorageClient } from './storage/minio-client.js';
import { handleNavigate, type NavigateRequest } from './handlers/navigate.js';
import { handleInteract, type InteractRequest } from './handlers/interact.js';
import { handleExtract, type ExtractRequest } from './handlers/extract.js';
import { handleScreenshot, type ScreenshotRequest } from './handlers/screenshot.js';
import { handleSessionClose } from './handlers/session.js';

// 从 request body 读取并解析 JSON。
async function readBody<T>(req: IncomingMessage): Promise<T> {
  return new Promise((resolve, reject) => {
    const chunks: Buffer[] = [];
    req.on('data', (chunk: Buffer) => chunks.push(chunk));
    req.on('end', () => {
      const raw = Buffer.concat(chunks).toString('utf-8');
      if (!raw) {
        resolve({} as T);
        return;
      }
      try {
        resolve(JSON.parse(raw) as T);
      } catch {
        reject(new Error('invalid JSON body'));
      }
    });
    req.on('error', reject);
  });
}

function writeJSON(res: ServerResponse, status: number, body: unknown): void {
  const payload = JSON.stringify(body);
  res.writeHead(status, {
    'Content-Type': 'application/json; charset=utf-8',
    'Content-Length': Buffer.byteLength(payload),
  });
  res.end(payload);
}

function extractSessionId(url: string): string | null {
  // DELETE /v1/sessions/:id → 提取 :id
  const match = /^\/v1\/sessions\/([^/?]+)$/.exec(url);
  return match ? decodeURIComponent(match[1]) : null;
}

export function createHttpServer(pool: BrowserPool, storage: StorageClient): ReturnType<typeof createServer> {
  const server = createServer(async (req: IncomingMessage, res: ServerResponse) => {
    const url = req.url ?? '/';
    const method = req.method ?? 'GET';

    try {
      // 健康检查
      if (method === 'GET' && url === '/healthz') {
        writeJSON(res, 200, { status: 'ok' });
        return;
      }

      if (method === 'POST' && url === '/v1/navigate') {
        const body = await readBody<NavigateRequest>(req);
        await handleNavigate(req, res, body, pool, storage);
        return;
      }

      if (method === 'POST' && url === '/v1/interact') {
        const body = await readBody<InteractRequest>(req);
        await handleInteract(req, res, body, pool, storage);
        return;
      }

      if (method === 'POST' && url === '/v1/extract') {
        const body = await readBody<ExtractRequest>(req);
        await handleExtract(req, res, body, pool, storage);
        return;
      }

      if (method === 'POST' && url === '/v1/screenshot') {
        const body = await readBody<ScreenshotRequest>(req);
        await handleScreenshot(req, res, body, pool, storage);
        return;
      }

      if (method === 'DELETE') {
        const sessionId = extractSessionId(url);
        if (sessionId !== null) {
          await handleSessionClose(req, res, sessionId, pool, storage);
          return;
        }
      }

      writeJSON(res, 404, { code: 'not_found', message: `${method} ${url}` });
    } catch (err) {
      const message = err instanceof Error ? err.message : 'internal error';
      writeJSON(res, 500, { code: 'internal_error', message });
    }
  });

  return server;
}
