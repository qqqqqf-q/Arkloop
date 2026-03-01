import { createServer, type IncomingMessage, type ServerResponse } from 'node:http';
import { BrowserPool } from './pool/browser-pool.js';
import type { SessionManager } from './pool/session-manager.js';
import type { StorageClient } from './storage/minio-client.js';
import { handleNavigate, type NavigateRequest } from './handlers/navigate.js';
import { handleInteract, type InteractRequest } from './handlers/interact.js';
import { handleExtract, type ExtractRequest } from './handlers/extract.js';
import { handleScreenshot, type ScreenshotRequest } from './handlers/screenshot.js';
import { handleSessionClose } from './handlers/session.js';

export interface RequestContext {
  sessionId: string;
  orgId: string;
  runId: string;
}

async function readBody<T>(req: IncomingMessage, maxBodyBytes: number): Promise<T> {
  return new Promise((resolve, reject) => {
    const chunks: Buffer[] = [];
    let totalBytes = 0;
    req.on('data', (chunk: Buffer) => {
      totalBytes += chunk.length;
      if (totalBytes > maxBodyBytes) {
        req.destroy();
        reject(new BodyTooLargeError());
        return;
      }
      chunks.push(chunk);
    });
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

class BodyTooLargeError extends Error {
  constructor() {
    super('request body too large');
  }
}

function writeJSON(res: ServerResponse, status: number, body: unknown): void {
  const payload = JSON.stringify(body);
  res.writeHead(status, {
    'Content-Type': 'application/json; charset=utf-8',
    'Content-Length': Buffer.byteLength(payload),
  });
  res.end(payload);
}

function extractRequestContext(req: IncomingMessage): RequestContext | null {
  const sessionId = req.headers['x-session-id'];
  const orgId = req.headers['x-org-id'];
  const runId = req.headers['x-run-id'];
  if (typeof sessionId !== 'string' || !sessionId) return null;
  if (typeof orgId !== 'string' || !orgId) return null;
  if (typeof runId !== 'string' || !runId) return null;
  return { sessionId, orgId, runId };
}

function extractSessionIdFromUrl(url: string): string | null {
  const match = /^\/v1\/sessions\/([^/?]+)$/.exec(url);
  return match ? decodeURIComponent(match[1]) : null;
}

export function createHttpServer(
  pool: BrowserPool,
  storage: StorageClient,
  sessionManager: SessionManager,
  maxBodyBytes: number,
  contentTextMaxLength: number,
): ReturnType<typeof createServer> {
  const server = createServer(async (req: IncomingMessage, res: ServerResponse) => {
    const url = req.url ?? '/';
    const method = req.method ?? 'GET';

    try {
      if (method === 'GET' && url === '/healthz') {
        writeJSON(res, 200, { status: 'ok' });
        return;
      }

      if (method === 'POST' && url === '/v1/navigate') {
        const ctx = extractRequestContext(req);
        if (!ctx) { writeJSON(res, 400, { code: 'missing_headers', message: 'X-Session-ID, X-Org-ID, X-Run-ID required' }); return; }
        const body = await readBody<NavigateRequest>(req, maxBodyBytes);
        await handleNavigate(res, body, ctx, pool, sessionManager, storage, contentTextMaxLength);
        return;
      }

      if (method === 'POST' && url === '/v1/interact') {
        const ctx = extractRequestContext(req);
        if (!ctx) { writeJSON(res, 400, { code: 'missing_headers', message: 'X-Session-ID, X-Org-ID, X-Run-ID required' }); return; }
        const body = await readBody<InteractRequest>(req, maxBodyBytes);
        await handleInteract(res, body, ctx, pool, sessionManager, storage, contentTextMaxLength);
        return;
      }

      if (method === 'POST' && url === '/v1/extract') {
        const ctx = extractRequestContext(req);
        if (!ctx) { writeJSON(res, 400, { code: 'missing_headers', message: 'X-Session-ID, X-Org-ID, X-Run-ID required' }); return; }
        const body = await readBody<ExtractRequest>(req, maxBodyBytes);
        await handleExtract(res, body, ctx, pool, sessionManager, contentTextMaxLength);
        return;
      }

      if (method === 'POST' && url === '/v1/screenshot') {
        const ctx = extractRequestContext(req);
        if (!ctx) { writeJSON(res, 400, { code: 'missing_headers', message: 'X-Session-ID, X-Org-ID, X-Run-ID required' }); return; }
        const body = await readBody<ScreenshotRequest>(req, maxBodyBytes);
        await handleScreenshot(res, body, ctx, pool, sessionManager, storage);
        return;
      }

      if (method === 'DELETE') {
        const sessionId = extractSessionIdFromUrl(url);
        if (sessionId !== null) {
          const ctx = extractRequestContext(req);
          if (!ctx) { writeJSON(res, 400, { code: 'missing_headers', message: 'X-Session-ID, X-Org-ID, X-Run-ID required' }); return; }
          await handleSessionClose(res, ctx, pool, storage);
          return;
        }
      }

      writeJSON(res, 404, { code: 'not_found', message: `${method} ${url}` });
    } catch (err) {
      if (err instanceof BodyTooLargeError) {
        writeJSON(res, 413, { code: 'body_too_large', message: 'request body too large' });
        return;
      }
      const message = err instanceof Error ? err.message : 'internal error';
      writeJSON(res, 500, { code: 'internal_error', message });
    }
  });

  return server;
}
