export interface Config {
  port: number;
  maxBrowsers: number;
  maxContextsPerBrowser: number;
  contextIdleTimeoutMs: number;
  contextMaxLifetimeMs: number;
  browserMemoryThresholdBytes: number;
  minio: MinioConfig;
  blockedHosts: string[];
}

export interface MinioConfig {
  endpoint: string;
  accessKey: string;
  secretKey: string;
  bucketSessions: string;
  bucketScreenshots: string;
  // 截图 URL 前缀，供外部访问；默认 http://{endpoint}
  publicEndpoint: string;
  // sessions bucket lifecycle rule TTL（天）
  sessionTtlDays: number;
}

export function loadConfig(): Config {
  return {
    port: parseInt(requireEnv('BROWSER_PORT', '3000'), 10),
    maxBrowsers: parseInt(requireEnv('BROWSER_MAX_BROWSERS', '2'), 10),
    maxContextsPerBrowser: parseInt(requireEnv('BROWSER_MAX_CONTEXTS_PER_BROWSER', '20'), 10),
    contextIdleTimeoutMs: parseInt(requireEnv('BROWSER_CONTEXT_IDLE_TIMEOUT_S', '60'), 10) * 1000,
    contextMaxLifetimeMs: parseInt(requireEnv('BROWSER_CONTEXT_MAX_LIFETIME_S', '1800'), 10) * 1000,
    browserMemoryThresholdBytes: parseInt(requireEnv('BROWSER_MEMORY_THRESHOLD_BYTES', String(1024 * 1024 * 1024)), 10),
    minio: {
      endpoint: requireEnv('BROWSER_MINIO_ENDPOINT', 'minio:9000'),
      accessKey: requireEnv('BROWSER_MINIO_ACCESS_KEY', 'minioadmin'),
      secretKey: requireEnvRequired('BROWSER_MINIO_SECRET_KEY'),
      bucketSessions: requireEnv('BROWSER_MINIO_BUCKET_SESSIONS', 'browser-sessions'),
      bucketScreenshots: requireEnv('BROWSER_MINIO_BUCKET_SCREENSHOTS', 'browser-screenshots'),
      publicEndpoint: requireEnv(
        'BROWSER_MINIO_PUBLIC_ENDPOINT',
        `http://${requireEnv('BROWSER_MINIO_ENDPOINT', 'minio:9000')}`,
      ),
      sessionTtlDays: parseInt(requireEnv('BROWSER_SESSION_TTL_DAYS', '7'), 10),
    },
    blockedHosts: parseBlockedHosts(
      requireEnv('BROWSER_BLOCKED_HOSTS', 'postgres,redis,minio,openviking,api,worker,gateway,pgbouncer'),
    ),
  };
}

function requireEnv(key: string, defaultValue: string): string {
  return process.env[key]?.trim() || defaultValue;
}

function requireEnvRequired(key: string): string {
  const value = process.env[key]?.trim();
  if (!value) {
    throw new Error(`missing required environment variable: ${key}`);
  }
  return value;
}

function parseBlockedHosts(raw: string): string[] {
  return raw
    .split(',')
    .map((h) => h.trim())
    .filter((h) => h.length > 0);
}
