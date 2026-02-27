import { loadConfig } from './config.js';
import { MinioClient } from './storage/minio-client.js';
import { BrowserPool } from './pool/browser-pool.js';
import { SessionManager } from './pool/session-manager.js';
import { createHttpServer } from './server.js';

process.on('uncaughtException', (err) => {
  process.stderr.write(JSON.stringify({ level: 'error', event: 'uncaught_exception', error: err.message }) + '\n');
  process.exit(1);
});

process.on('unhandledRejection', (reason) => {
  const message = reason instanceof Error ? reason.message : String(reason);
  process.stderr.write(JSON.stringify({ level: 'error', event: 'unhandled_rejection', error: message }) + '\n');
  process.exit(1);
});

const config = loadConfig();
const storage = new MinioClient(config.minio);
const pool = new BrowserPool({
  maxBrowsers: config.maxBrowsers,
  maxContextsPerBrowser: config.maxContextsPerBrowser,
  contextIdleTimeoutMs: config.contextIdleTimeoutMs,
  contextMaxLifetimeMs: config.contextMaxLifetimeMs,
  browserMemoryThresholdBytes: config.browserMemoryThresholdBytes,
  blockedHosts: config.blockedHosts,
  storage,
});
const sessionManager = new SessionManager(storage);

await storage.init();
await pool.init();

const server = createHttpServer(pool, storage, sessionManager, config.contentTextMaxLength);

server.listen(config.port, '0.0.0.0', () => {
  process.stdout.write(JSON.stringify({ level: 'info', event: 'server_started', port: config.port }) + '\n');
});

process.on('SIGTERM', async () => {
  server.close();
  await pool.shutdown();
  process.stdout.write(JSON.stringify({ level: 'info', event: 'server_stopped' }) + '\n');
  process.exit(0);
});

process.on('SIGINT', async () => {
  server.close();
  await pool.shutdown();
  process.exit(0);
});
