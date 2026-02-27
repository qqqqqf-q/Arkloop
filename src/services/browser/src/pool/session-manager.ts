import type { StorageClient } from '../storage/minio-client.js';

export interface SessionKey {
  orgId: string;
  sessionId: string;
}

export interface SessionManagerConfig {
  storage: StorageClient;
  ttlDays?: number;
}

export class SessionManager {
  private readonly config: SessionManagerConfig;

  constructor(config: SessionManagerConfig) {
    this.config = config;
  }

  async loadState(key: SessionKey): Promise<object | null> {
    return this.config.storage.loadSessionState(key.orgId, key.sessionId);
  }

  async saveState(key: SessionKey, state: object): Promise<void> {
    return this.config.storage.saveSessionState(key.orgId, key.sessionId, state);
  }

  async deleteState(key: SessionKey): Promise<void> {
    return this.config.storage.deleteSessionState(key.orgId, key.sessionId);
  }
}
