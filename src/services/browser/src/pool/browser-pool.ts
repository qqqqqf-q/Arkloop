import { chromium, type Browser, type BrowserContext, type BrowserContextOptions } from 'playwright';
import { promises as fs } from 'node:fs';
import type { StorageClient } from '../storage/minio-client.js';
import { checkNetworkAccess } from '../security/network-filter.js';

export interface BrowserPoolConfig {
  maxBrowsers: number;
  maxContextsPerBrowser: number;
  contextIdleTimeoutMs: number;
  contextMaxLifetimeMs: number;
  browserMemoryThresholdBytes: number;
  blockedHosts: string[];
  storage: StorageClient;
}

export interface ActiveContext {
  context: BrowserContext;
  sessionId: string;
  orgId: string;
  lastActive: number;
  createdAt: number;
  idleTimer: NodeJS.Timeout | null;
  lifetimeTimer: NodeJS.Timeout;
  browserEntry: BrowserEntry;
}

export interface ContextHandle {
  context: BrowserContext;
  sessionId: string;
  orgId: string;
}

interface BrowserEntry {
  browser: Browser;
  contextCount: number;
  createdAt: number;
}

const MEMORY_CHECK_INTERVAL_MS = 30_000;
// Chromium 在 Docker 环境必须禁用 sandbox，/dev/shm 通常不足。
const CHROMIUM_LAUNCH_ARGS = ['--no-sandbox', '--disable-dev-shm-usage'];

async function getBrowserRss(pid: number): Promise<number | null> {
  if (process.platform !== 'linux') return null;
  try {
    const status = await fs.readFile(`/proc/${pid}/status`, 'utf-8');
    const match = /VmRSS:\s+(\d+)\s+kB/.exec(status);
    return match ? parseInt(match[1], 10) * 1024 : null;
  } catch {
    return null;
  }
}

export class BrowserPool {
  private readonly config: BrowserPoolConfig;
  private readonly browsers: BrowserEntry[] = [];
  private readonly activeContexts = new Map<string, ActiveContext>();
  // pendingContexts 防止同 sessionId 并发请求时重复创建 BrowserContext。
  private readonly pendingContexts = new Map<string, Promise<ContextHandle>>();
  private memoryCheckTimer: NodeJS.Timeout | null = null;
  private shuttingDown = false;

  constructor(config: BrowserPoolConfig) {
    this.config = config;
  }

  async init(): Promise<void> {
    await this.launchNewBrowser();
    this.startMemoryMonitor();
  }

  async getContext(orgId: string, sessionId: string, freshSession = false): Promise<ContextHandle> {
    if (!freshSession) {
      const active = this.activeContexts.get(sessionId);
      if (active) {
        if (active.idleTimer !== null) {
          clearTimeout(active.idleTimer);
          active.idleTimer = null;
        }
        active.lastActive = Date.now();
        return { context: active.context, sessionId: active.sessionId, orgId: active.orgId };
      }

      const pending = this.pendingContexts.get(sessionId);
      if (pending) return pending;
    } else {
      // 等待已有 pending 创建完成，避免覆盖导致 context 泄漏
      const pending = this.pendingContexts.get(sessionId);
      if (pending) {
        try { await pending; } catch { /* 忽略，随后 forceClose 会处理 */ }
      }
      await this.forceCloseContext(sessionId);
    }

    const creation = this.createContext(orgId, sessionId, freshSession);
    this.pendingContexts.set(sessionId, creation);
    try {
      return await creation;
    } finally {
      this.pendingContexts.delete(sessionId);
    }
  }

  private async createContext(orgId: string, sessionId: string, freshSession = false): Promise<ContextHandle> {
    // fresh_session=true 时跳过加载已有状态，从空白 context 开始
    const state = freshSession ? null : await this.config.storage.loadSessionState(orgId, sessionId);
    const entry = await this.pickBrowser();
    const contextOptions: BrowserContextOptions = state
      ? { storageState: state as BrowserContextOptions['storageState'] }
      : {};
    const context = await entry.browser.newContext(contextOptions);
    entry.contextCount++;

    await context.route('**/*', async (route) => {
      let hostname: string;
      try {
        hostname = new URL(route.request().url()).hostname;
      } catch {
        await route.continue();
        return;
      }
      const result = await checkNetworkAccess(hostname, this.config.blockedHosts);
      if (result.blocked) {
        process.stdout.write(
          JSON.stringify({ level: 'warn', event: 'ssrf_blocked', reason: result.reason, target: result.target, session_id: sessionId }) + '\n',
        );
        await route.abort('blockedbyclient');
        return;
      }
      await route.continue();
    });

    const lifetimeTimer = setTimeout(() => {
      void this.expireContext(sessionId, 'lifetime');
    }, this.config.contextMaxLifetimeMs);
    // Node.js unref 防止 lifetime timer 阻止进程退出。
    lifetimeTimer.unref();

    const active: ActiveContext = {
      context,
      sessionId,
      orgId,
      lastActive: Date.now(),
      createdAt: Date.now(),
      idleTimer: null,
      lifetimeTimer,
      browserEntry: entry,
    };
    this.activeContexts.set(sessionId, active);
    return { context, sessionId, orgId };
  }

  releaseContext(sessionId: string): void {
    const active = this.activeContexts.get(sessionId);
    if (!active) return;

    if (active.idleTimer !== null) {
      clearTimeout(active.idleTimer);
    }
    active.idleTimer = setTimeout(() => {
      void this.expireContext(sessionId, 'idle');
    }, this.config.contextIdleTimeoutMs);
    active.idleTimer.unref();
  }

  // forceCloseContext 关闭 context 但不持久化 storageState。
  // 用于 fresh_session 和 closeAndDeleteContext。
  private async forceCloseContext(sessionId: string): Promise<void> {
    const active = this.activeContexts.get(sessionId);
    if (!active) return;

    this.activeContexts.delete(sessionId);
    if (active.idleTimer !== null) clearTimeout(active.idleTimer);
    clearTimeout(active.lifetimeTimer);

    try {
      await active.context.close();
    } catch {
      // context 可能已断线，忽略
    }
    active.browserEntry.contextCount--;
  }

  // closeAndDeleteContext 供 DELETE /v1/sessions/:id handler 调用：
  // 关闭 context（不持久化）并删除 MinIO 上的 state.json。
  async closeAndDeleteContext(sessionId: string, orgId: string): Promise<void> {
    await this.forceCloseContext(sessionId);
    await this.config.storage.deleteSessionState(orgId, sessionId);
  }

  private async expireContext(sessionId: string, reason: 'idle' | 'lifetime' | 'drain' | 'shutdown'): Promise<void> {
    const active = this.activeContexts.get(sessionId);
    if (!active) return;

    // 先从 map 移除，防止重入
    this.activeContexts.delete(sessionId);

    if (active.idleTimer !== null) clearTimeout(active.idleTimer);
    clearTimeout(active.lifetimeTimer);

    try {
      const state = await active.context.storageState();
      await this.config.storage.saveSessionState(active.orgId, sessionId, state);
    } catch (err) {
      process.stderr.write(
        JSON.stringify({ level: 'error', event: 'context_persist_failed', session_id: sessionId, reason, error: String(err) }) + '\n',
      );
    }

    try {
      await active.context.close();
    } catch {
      // context 可能已断线，忽略关闭错误
    }

    active.browserEntry.contextCount--;
    process.stdout.write(
      JSON.stringify({ level: 'info', event: 'context_closed', session_id: sessionId, reason }) + '\n',
    );
  }

  private async pickBrowser(): Promise<BrowserEntry> {
    // 选 contextCount 最少且未超限的 browser 实例
    const available = this.browsers.filter((b) => b.contextCount < this.config.maxContextsPerBrowser);
    if (available.length > 0) {
      return available.reduce((min, b) => (b.contextCount < min.contextCount ? b : min));
    }
    // 所有实例已满，尝试 launch 新实例
    if (this.browsers.length < this.config.maxBrowsers) {
      return this.launchNewBrowser();
    }
    // 超出最大 browser 数，选 contextCount 最小的实例（允许超出 maxContextsPerBrowser）
    process.stderr.write(
      JSON.stringify({ level: 'warn', event: 'browser_pool_overloaded', browsers: this.browsers.length, max: this.config.maxBrowsers }) + '\n',
    );
    return this.browsers.reduce((min, b) => (b.contextCount < min.contextCount ? b : min));
  }

  private async launchNewBrowser(): Promise<BrowserEntry> {
    const browser = await chromium.launch({ args: CHROMIUM_LAUNCH_ARGS });
    const entry: BrowserEntry = { browser, contextCount: 0, createdAt: Date.now() };
    this.browsers.push(entry);

    browser.on('disconnected', () => {
      if (this.shuttingDown) return;
      const idx = this.browsers.indexOf(entry);
      // idx === -1 说明 drainBrowser 已先行处理，不重复重建
      if (idx === -1) return;
      this.browsers.splice(idx, 1);
      process.stderr.write(JSON.stringify({ level: 'warn', event: 'browser_disconnected' }) + '\n');
      void this.launchNewBrowser();
    });

    return entry;
  }

  private startMemoryMonitor(): void {
    if (process.platform !== 'linux') return;

    this.memoryCheckTimer = setInterval(() => {
      void this.checkBrowserMemory();
    }, MEMORY_CHECK_INTERVAL_MS);
    this.memoryCheckTimer.unref();
  }

  private async checkBrowserMemory(): Promise<void> {
    for (const entry of [...this.browsers]) {
      // browser.process() 返回底层 Chromium 进程（Playwright 内部 API，类型定义可能未完全暴露）。
      const pid = (entry.browser as unknown as { process(): { pid?: number } | null }).process()?.pid;
      if (!pid) continue;

      const rss = await getBrowserRss(pid);
      if (rss === null) continue;

      if (rss > this.config.browserMemoryThresholdBytes) {
        process.stdout.write(
          JSON.stringify({ level: 'warn', event: 'browser_oom_drain', pid, rss, threshold: this.config.browserMemoryThresholdBytes }) + '\n',
        );
        await this.drainBrowser(entry);
      }
    }
  }

  private async drainBrowser(entry: BrowserEntry): Promise<void> {
    // 找出该 browser 上的所有 context，并发持久化关闭
    const sessions = [...this.activeContexts.entries()]
      .filter(([, ac]) => ac.browserEntry === entry)
      .map(([sid]) => sid);

    await Promise.all(sessions.map((sid) => this.expireContext(sid, 'drain')));

    const idx = this.browsers.indexOf(entry);
    if (idx !== -1) this.browsers.splice(idx, 1);

    try {
      await entry.browser.close();
    } catch {
      // 强制关闭，忽略错误
    }

    // 重建一个新的 browser 实例
    if (!this.shuttingDown && this.browsers.length < this.config.maxBrowsers) {
      await this.launchNewBrowser();
    }
  }

  async shutdown(): Promise<void> {
    this.shuttingDown = true;

    if (this.memoryCheckTimer !== null) {
      clearInterval(this.memoryCheckTimer);
      this.memoryCheckTimer = null;
    }

    // 并发持久化并关闭所有 active contexts
    await Promise.all([...this.activeContexts.keys()].map((sid) => this.expireContext(sid, 'shutdown')));

    // 关闭所有 browser 实例
    await Promise.all(
      this.browsers.map(async (entry) => {
        try {
          await entry.browser.close();
        } catch {
          // 忽略关闭错误
        }
      }),
    );
    this.browsers.length = 0;
  }
}

