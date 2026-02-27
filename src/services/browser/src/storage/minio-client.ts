import {
  S3Client,
  CreateBucketCommand,
  HeadBucketCommand,
  PutObjectCommand,
  GetObjectCommand,
  DeleteObjectCommand,
  PutBucketLifecycleConfigurationCommand,
  PutBucketPolicyCommand,
} from '@aws-sdk/client-s3';

// storageState 路径：browser-sessions/{org_id}/{session_id}/state.json
// 截图路径：browser-screenshots/{run_id}/{step}.png

export interface StorageClient {
  init(): Promise<void>;
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
  publicEndpoint: string;
  sessionTtlDays: number;
}

export function sessionStatePath(orgId: string, sessionId: string): string {
  return `${orgId}/${sessionId}/state.json`;
}

export function screenshotPath(runId: string, step: number): string {
  return `${runId}/${step}.png`;
}

export class MinioClient implements StorageClient {
  private readonly config: MinioClientConfig;
  private readonly s3: S3Client;

  constructor(config: MinioClientConfig) {
    this.config = config;
    this.s3 = new S3Client({
      endpoint: `http://${config.endpoint}`,
      region: 'us-east-1',
      credentials: {
        accessKeyId: config.accessKey,
        secretAccessKey: config.secretKey,
      },
      forcePathStyle: true,
    });
  }

  async init(): Promise<void> {
    await this.ensureBucket(this.config.bucketSessions);
    await this.ensureBucket(this.config.bucketScreenshots);
    await this.setSessionsLifecycle();
    await this.setScreenshotsPublicPolicy();
  }

  private async ensureBucket(bucket: string): Promise<void> {
    try {
      await this.s3.send(new HeadBucketCommand({ Bucket: bucket }));
    } catch (err: unknown) {
      const status = (err as { $metadata?: { httpStatusCode?: number } }).$metadata?.httpStatusCode;
      if (status === 404 || status === 403) {
        await this.s3.send(new CreateBucketCommand({ Bucket: bucket }));
        return;
      }
      throw err;
    }
  }

  private async setSessionsLifecycle(): Promise<void> {
    await this.s3.send(
      new PutBucketLifecycleConfigurationCommand({
        Bucket: this.config.bucketSessions,
        LifecycleConfiguration: {
          Rules: [
            {
              ID: 'session-state-ttl',
              Status: 'Enabled',
              Filter: { Prefix: '' },
              Expiration: { Days: this.config.sessionTtlDays },
            },
          ],
        },
      }),
    );
  }

  private async setScreenshotsPublicPolicy(): Promise<void> {
    const policy = {
      Version: '2012-10-17',
      Statement: [
        {
          Effect: 'Allow',
          Principal: '*',
          Action: 's3:GetObject',
          Resource: `arn:aws:s3:::${this.config.bucketScreenshots}/*`,
        },
      ],
    };
    await this.s3.send(
      new PutBucketPolicyCommand({
        Bucket: this.config.bucketScreenshots,
        Policy: JSON.stringify(policy),
      }),
    );
  }

  async uploadScreenshot(runId: string, step: number, imageBuffer: Buffer): Promise<string> {
    const key = screenshotPath(runId, step);
    await this.s3.send(
      new PutObjectCommand({
        Bucket: this.config.bucketScreenshots,
        Key: key,
        Body: imageBuffer,
        ContentType: 'image/png',
      }),
    );
    return `${this.config.publicEndpoint}/${this.config.bucketScreenshots}/${key}`;
  }

  async saveSessionState(orgId: string, sessionId: string, state: object): Promise<void> {
    await this.s3.send(
      new PutObjectCommand({
        Bucket: this.config.bucketSessions,
        Key: sessionStatePath(orgId, sessionId),
        Body: JSON.stringify(state),
        ContentType: 'application/json',
      }),
    );
  }

  async loadSessionState(orgId: string, sessionId: string): Promise<object | null> {
    try {
      const resp = await this.s3.send(
        new GetObjectCommand({
          Bucket: this.config.bucketSessions,
          Key: sessionStatePath(orgId, sessionId),
        }),
      );
      const body = await resp.Body?.transformToString('utf-8');
      if (!body) return null;
      return JSON.parse(body) as object;
    } catch (err: unknown) {
      const name = (err as { name?: string }).name;
      const status = (err as { $metadata?: { httpStatusCode?: number } }).$metadata?.httpStatusCode;
      if (name === 'NoSuchKey' || status === 404) return null;
      throw err;
    }
  }

  async deleteSessionState(orgId: string, sessionId: string): Promise<void> {
    await this.s3.send(
      new DeleteObjectCommand({
        Bucket: this.config.bucketSessions,
        Key: sessionStatePath(orgId, sessionId),
      }),
    );
  }
}

