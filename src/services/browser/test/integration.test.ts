/**
 * Browser Service 集成测试
 *
 * 前置条件：
 *   docker compose up browser minio
 *
 * 运行方式：
 *   BROWSER_SERVICE_URL=http://localhost:3100 pnpm test
 *   （不设置 BROWSER_SERVICE_URL 默认使用 http://localhost:3100）
 */

import { describe, it, expect, beforeAll, afterAll } from 'vitest';

const BASE_URL = process.env['BROWSER_SERVICE_URL'] ?? 'http://localhost:3100';

function sessionHeaders(sessionId: string, orgId = 'org-test', runId = 'run-test') {
  return {
    'Content-Type': 'application/json',
    'X-Session-ID': sessionId,
    'X-Org-ID': orgId,
    'X-Run-ID': runId,
  };
}

async function post(path: string, body: unknown, headers: Record<string, string>) {
  return fetch(`${BASE_URL}${path}`, {
    method: 'POST',
    headers,
    body: JSON.stringify(body),
  });
}

// ─── healthz ──────────────────────────────────────────────────────────────────

describe('GET /healthz', () => {
  it('returns 200 ok', async () => {
    const res = await fetch(`${BASE_URL}/healthz`);
    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body).toEqual({ status: 'ok' });
  });
});

// ─── header validation ────────────────────────────────────────────────────────

describe('missing required headers', () => {
  it('POST /v1/navigate returns 400 without headers', async () => {
    const res = await fetch(`${BASE_URL}/v1/navigate`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ url: 'https://example.com' }),
    });
    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.code).toBe('missing_headers');
  });

  it('POST /v1/interact returns 400 without headers', async () => {
    const res = await fetch(`${BASE_URL}/v1/interact`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ action: 'scroll' }),
    });
    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.code).toBe('missing_headers');
  });
});

// ─── navigate ─────────────────────────────────────────────────────────────────

describe('POST /v1/navigate', () => {
  it('returns 400 when url is missing', async () => {
    const headers = sessionHeaders('nav-missing-url-session');
    const res = await post('/v1/navigate', {}, headers);
    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.code).toBe('missing_param');
  });

  it('navigates to example.com and returns full page state', async () => {
    const headers = sessionHeaders('nav-example-session');
    const res = await post('/v1/navigate', { url: 'https://example.com', wait_until: 'load' }, headers);
    expect(res.status).toBe(200);
    const body = await res.json();

    expect(body.page_url).toContain('example.com');
    expect(typeof body.page_title).toBe('string');
    expect(body.page_title.length).toBeGreaterThan(0);
    // screenshot_url は MinIO に保存された URL を返す
    expect(body.screenshot_url).toMatch(/^http/);
    expect(typeof body.content_text).toBe('string');
    expect(body.content_text.length).toBeGreaterThan(0);
    expect(typeof body.accessibility_tree).toBe('string');
  });

  it('fresh_session=true creates a new context', async () => {
    const headers = sessionHeaders('nav-fresh-session');
    // 先建立一个 session
    await post('/v1/navigate', { url: 'https://example.com' }, headers);
    // 用 fresh_session 重置
    const res = await post('/v1/navigate', { url: 'https://example.com', fresh_session: true }, headers);
    expect(res.status).toBe(200);
  });
});

// ─── full flow: navigate → interact → extract → screenshot → delete ───────────

describe('full session flow', () => {
  const SESSION_ID = `flow-session-${Date.now()}`;
  const headers = sessionHeaders(SESSION_ID);

  beforeAll(async () => {
    // 所有后续测试依赖此 navigate
    const res = await post('/v1/navigate', { url: 'https://example.com', wait_until: 'load' }, headers);
    if (res.status !== 200) {
      throw new Error(`navigate 失败: ${res.status} ${await res.text()}`);
    }
  });

  afterAll(async () => {
    // 清理 session
    await fetch(`${BASE_URL}/v1/sessions/${SESSION_ID}`, {
      method: 'DELETE',
      headers,
    });
  });

  // interact ──────────────────────────────────────────────────────────────────

  it('interact: scroll down', async () => {
    const res = await post('/v1/interact', { action: 'scroll', value: '300' }, headers);
    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.page_url).toContain('example.com');
    expect(body.screenshot_url).toMatch(/^http/);
  });

  it('interact: hover on link', async () => {
    const res = await post('/v1/interact', { action: 'hover', selector: 'a' }, headers);
    expect(res.status).toBe(200);
  });

  it('interact: invalid action returns 400', async () => {
    const res = await post('/v1/interact', { action: 'fly' as never }, headers);
    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.code).toBe('invalid_action');
  });

  // extract ───────────────────────────────────────────────────────────────────

  it('extract: text mode', async () => {
    const res = await post('/v1/extract', { mode: 'text' }, headers);
    expect(res.status).toBe(200);
    const body = await res.json();
    expect(typeof body.content).toBe('string');
    expect(body.content.length).toBeGreaterThan(0);
    expect(typeof body.word_count).toBe('number');
    expect(body.word_count).toBeGreaterThan(0);
  });

  it('extract: text mode with selector', async () => {
    const res = await post('/v1/extract', { mode: 'text', selector: 'h1' }, headers);
    expect(res.status).toBe(200);
    const body = await res.json();
    expect(typeof body.content).toBe('string');
  });

  it('extract: accessibility mode', async () => {
    const res = await post('/v1/extract', { mode: 'accessibility' }, headers);
    expect(res.status).toBe(200);
    const body = await res.json();
    expect(typeof body.content).toBe('string');
    // accessibility tree 应包含 role 标记
    expect(body.content).toContain('[');
  });

  it('extract: html_clean mode', async () => {
    const res = await post('/v1/extract', { mode: 'html_clean' }, headers);
    expect(res.status).toBe(200);
    const body = await res.json();
    expect(typeof body.content).toBe('string');
    expect(body.content.length).toBeGreaterThan(0);
    // 应去除 script/style 标签
    expect(body.content).not.toMatch(/<script/i);
    expect(body.content).not.toMatch(/<style/i);
  });

  it('extract: invalid mode returns 400', async () => {
    const res = await post('/v1/extract', { mode: 'pdf' as never }, headers);
    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.code).toBe('invalid_mode');
  });

  // screenshot ────────────────────────────────────────────────────────────────

  it('screenshot: default (viewport, jpeg)', async () => {
    const res = await post('/v1/screenshot', { quality: 80 }, headers);
    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.screenshot_url).toMatch(/^http/);
    // jpeg 路径扩展名应为 .jpeg
    expect(body.screenshot_url).toContain('.jpeg');
    expect(body.width).toBeGreaterThan(0);
    expect(body.height).toBeGreaterThan(0);
  });

  it('screenshot: full_page png (quality=100)', async () => {
    const res = await post('/v1/screenshot', { full_page: true, quality: 100 }, headers);
    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.screenshot_url).toMatch(/^http/);
    expect(body.screenshot_url).toContain('.png');
    // 全页高度 >= viewport 高度
    expect(body.height).toBeGreaterThan(0);
  });

  it('screenshot: selector', async () => {
    const res = await post('/v1/screenshot', { selector: 'h1' }, headers);
    // example.com 有 h1，应成功
    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.screenshot_url).toMatch(/^http/);
  });
});

// ─── no active page ───────────────────────────────────────────────────────────

describe('no active page guard', () => {
  it('interact on fresh context returns 400', async () => {
    const headers = sessionHeaders(`no-page-session-${Date.now()}`);
    const res = await post('/v1/interact', { action: 'scroll' }, headers);
    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.code).toBe('no_active_page');
  });

  it('extract on fresh context returns 400', async () => {
    const headers = sessionHeaders(`no-page-extract-session-${Date.now()}`);
    const res = await post('/v1/extract', { mode: 'text' }, headers);
    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.code).toBe('no_active_page');
  });

  it('screenshot on fresh context returns 400', async () => {
    const headers = sessionHeaders(`no-page-screenshot-session-${Date.now()}`);
    const res = await post('/v1/screenshot', {}, headers);
    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.code).toBe('no_active_page');
  });
});

// ─── DELETE session ───────────────────────────────────────────────────────────

describe('DELETE /v1/sessions/:id', () => {
  it('closes and deletes session, returns 204', async () => {
    const SESSION_ID = `delete-session-${Date.now()}`;
    const headers = sessionHeaders(SESSION_ID);
    // 先 navigate 建立 session
    await post('/v1/navigate', { url: 'https://example.com' }, headers);

    const res = await fetch(`${BASE_URL}/v1/sessions/${SESSION_ID}`, {
      method: 'DELETE',
      headers,
    });
    expect(res.status).toBe(204);
  });

  it('deleting non-existent session returns 204 (idempotent)', async () => {
    const SESSION_ID = `ghost-session-${Date.now()}`;
    const headers = sessionHeaders(SESSION_ID);
    const res = await fetch(`${BASE_URL}/v1/sessions/${SESSION_ID}`, {
      method: 'DELETE',
      headers,
    });
    expect(res.status).toBe(204);
  });
});

// ─── 404 ──────────────────────────────────────────────────────────────────────

describe('unknown routes', () => {
  it('GET /unknown returns 404', async () => {
    const res = await fetch(`${BASE_URL}/unknown`);
    expect(res.status).toBe(404);
    const body = await res.json();
    expect(body.code).toBe('not_found');
  });
});
