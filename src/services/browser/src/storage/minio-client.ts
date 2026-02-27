// storageState 路径：browser-sessions/{org_id}/{session_id}/state.json
// 截图路径：browser-screenshots/{run_id}/{step}.png

export interface StorageClient {
  uploadScreenshot(runId: string, step: number, imageBuffer: Buffer): Promise<string>;
  saveSessionState(orgId: string, sessionId: string, state: object): Promise<void>;
  loadSessionState(orgId: string, sessionId: string): Promise<object | null>;
  deleteSessionState(orgId: string, sessionId: string): Promise<void>;
}

export interface MinioClientConfig {
  endpoint: string;
  accessKey: string;
  secretKey: string;
  bucketSessions: string;
  bucketScreenshots: string;
}

export function sessionStatePath(orgId: string, sessionId: string): string {
  return `${orgId}/${sessionId}/state.json`;
}

export function screenshotPath(runId: string, step: number): string {
  return `${runId}/${step}.png`;
}

// MinioClient 完整实现在 AS-7.3 中完成。
export class MinioClient implements StorageClient {
  private readonly config: MinioClientConfig;

  constructor(config: MinioClientConfig) {
    this.config = config;
  }

  async uploadScreenshot(_runId: string, _step: number, _imageBuffer: Buffer): Promise<string> {
    throw new Error(`MinioClient not implemented (AS-7.3): ${this.config.endpoint}`);
  }

  async saveSessionState(_orgId: string, _sessionId: string, _state: object): Promise<void> {
    throw new Error('MinioClient not implemented (AS-7.3)');
  }

  async loadSessionState(_orgId: string, _sessionId: string): Promise<object | null> {
    throw new Error('MinioClient not implemented (AS-7.3)');
  }

  async deleteSessionState(_orgId: string, _sessionId: string): Promise<void> {
    throw new Error('MinioClient not implemented (AS-7.3)');
  }
}
