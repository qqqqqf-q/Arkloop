import pg from 'pg';

export interface Config {
  port: number;
  maxBrowsers: number;
  maxContextsPerBrowser: number;
  contextIdleTimeoutMs: number;
  contextMaxLifetimeMs: number;
  maxBodyBytes: number;
  browserMemoryThresholdBytes: number;
  contentTextMaxLength: number;
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

export async function loadConfig(): Promise<Config> {
  const platformSettings = await loadPlatformSettings([
    'browser.max_body_bytes',
    'browser.context_max_lifetime_s',
  ]);

  const maxBodyBytes = resolveIntSetting(
    ['ARKLOOP_BROWSER_MAX_BODY_BYTES', 'BROWSER_MAX_BODY_BYTES'],
    platformSettings['browser.max_body_bytes'],
    1024 * 1024,
  );
  const contextMaxLifetimeS = resolveIntSetting(
    ['ARKLOOP_BROWSER_CONTEXT_MAX_LIFETIME_S', 'BROWSER_CONTEXT_MAX_LIFETIME_S'],
    platformSettings['browser.context_max_lifetime_s'],
    1800,
  );

  return {
    port: parseInt(requireEnv('BROWSER_PORT', '3000'), 10),
    maxBrowsers: parseInt(requireEnv('BROWSER_MAX_BROWSERS', '2'), 10),
    maxContextsPerBrowser: parseInt(requireEnv('BROWSER_MAX_CONTEXTS_PER_BROWSER', '20'), 10),
    contextIdleTimeoutMs: parseInt(requireEnv('BROWSER_CONTEXT_IDLE_TIMEOUT_S', '60'), 10) * 1000,
    contextMaxLifetimeMs: contextMaxLifetimeS * 1000,
    maxBodyBytes,
    browserMemoryThresholdBytes: parseInt(requireEnv('BROWSER_MEMORY_THRESHOLD_BYTES', String(1024 * 1024 * 1024)), 10),
    contentTextMaxLength: parseInt(requireEnv('BROWSER_CONTENT_TEXT_MAX_LENGTH', '8000'), 10),
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

function resolveIntSetting(envKeys: string[], dbValue: string | undefined, fallback: number): number {
  for (const key of envKeys) {
    const raw = process.env[key]?.trim();
    if (!raw) continue;
    const v = Number.parseInt(raw, 10);
    if (Number.isFinite(v) && v > 0) return v;
  }

  if (dbValue !== undefined) {
    const v = Number.parseInt(dbValue.trim(), 10);
    if (Number.isFinite(v) && v > 0) return v;
  }

  return fallback;
}

async function loadPlatformSettings(keys: string[]): Promise<Record<string, string>> {
  const url = process.env.ARKLOOP_DATABASE_URL?.trim();
  if (!url) return {};

  try {
    const { Client } = pg;
    const client = new Client({ connectionString: url });
    await client.connect();

    try {
      const res = await client.query<{ key: string; value: string }>(
        'SELECT key, value FROM platform_settings WHERE key = ANY($1)',
        [keys],
      );

      const out: Record<string, string> = {};
      for (const row of res.rows) {
        if (row?.key && row?.value) {
          out[row.key] = row.value;
        }
      }
      return out;
    } finally {
      await client.end().catch(() => undefined);
    }
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err);
    process.stderr.write(
      JSON.stringify({ level: 'warn', event: 'platform_settings_load_failed', error: message }) + '\n',
    );
    return {};
  }
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
