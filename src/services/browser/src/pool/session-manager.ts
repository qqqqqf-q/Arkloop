import type { StorageClient } from '../storage/minio-client.js';

export interface SessionKey {
  orgId: string;
  sessionId: string;
}

export class SessionManager {
  private readonly storage: StorageClient;

  constructor(storage: StorageClient) {
    this.storage = storage;
  }

  async loadState(key: SessionKey): Promise<object | null> {
    return this.storage.loadSessionState(key.orgId, key.sessionId);
  }

  async saveState(key: SessionKey, state: object): Promise<void> {
    return this.storage.saveSessionState(key.orgId, key.sessionId, state);
  }

  async deleteState(key: SessionKey): Promise<void> {
    return this.storage.deleteSessionState(key.orgId, key.sessionId);
  }
}
