package objectstore

import "context"

const ArtifactBucket = "sandbox-artifacts"
const SessionStateBucket = "sandbox-session-state"
const EnvironmentStateBucket = "sandbox-environments"
const SkillStoreBucket = "sandbox-skills"

type Store interface {
Put(ctx context.Context, key string, data []byte) error
PutObject(ctx context.Context, key string, data []byte, options PutOptions) error
Get(ctx context.Context, key string) ([]byte, error)
GetWithContentType(ctx context.Context, key string) ([]byte, string, error)
Head(ctx context.Context, key string) (ObjectInfo, error)
Delete(ctx context.Context, key string) error
}

type BlobStore interface {
Put(ctx context.Context, key string, data []byte) error
PutIfAbsent(ctx context.Context, key string, data []byte) (bool, error)
Get(ctx context.Context, key string) ([]byte, error)
Head(ctx context.Context, key string) (ObjectInfo, error)
Delete(ctx context.Context, key string) error
ListPrefix(ctx context.Context, prefix string) ([]ObjectInfo, error)
WriteJSONAtomic(ctx context.Context, key string, value any) error
}

type LifecycleConfigurator interface {
SetLifecycleExpirationDays(ctx context.Context, days int) error
}

type BucketOpener interface {
Open(ctx context.Context, bucket string) (Store, error)
}

// S3Config holds credentials and endpoint for an S3-compatible object store.
type S3Config struct {
Endpoint  string
AccessKey string
SecretKey string
Region    string
}

type PutOptions struct {
ContentType string
Metadata    map[string]string
}

type ObjectInfo struct {
Key         string
ContentType string
Metadata    map[string]string
Size        int64
ETag        string
}
