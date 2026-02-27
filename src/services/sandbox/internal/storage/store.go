package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"golang.org/x/sync/singleflight"
)

const snapshotBucket = "sandbox-snapshots"

// SnapshotStore 管理快照文件的远端存储与本地缓存。
type SnapshotStore interface {
	// Upload 将本地快照文件上传到对象存储。
	Upload(ctx context.Context, templateID string, memPath, diskPath string) error

	// Download 将快照文件下载到本地缓存目录，返回本地文件路径。
	// 若本地缓存与远端 ETag 一致，跳过网络下载直接返回缓存路径。
	Download(ctx context.Context, templateID string) (memPath, diskPath string, err error)

	// Exists 检查某 template 的快照是否存在于对象存储中。
	Exists(ctx context.Context, templateID string) (bool, error)
}

// etagCache 记录已下载快照的 ETag，存储在本地目录的 etag.json 中。
type etagCache struct {
	MemETag  string `json:"mem_etag"`
	DiskETag string `json:"disk_etag"`
}

// MinIOStore 实现 SnapshotStore，使用 MinIO 作为后端并维护本地缓存。
type MinIOStore struct {
	client       *minio.Client
	cacheBaseDir string                // 本地缓存根目录，通常为 {socketBaseDir}/_snapshots
	dlGroup      singleflight.Group    // 防止同一 templateID 并发下载
}

// NewMinIOStore 创建 MinIOStore。endpoint 支持 http:// 或 https:// 前缀，也可以不带前缀。
// cacheBaseDir 是本地快照缓存目录（不同 templateID 会在其下创建子目录）。
func NewMinIOStore(ctx context.Context, endpoint, accessKey, secretKey, cacheBaseDir string) (*MinIOStore, error) {
	endpoint = strings.TrimSpace(endpoint)
	accessKey = strings.TrimSpace(accessKey)
	secretKey = strings.TrimSpace(secretKey)
	cacheBaseDir = strings.TrimSpace(cacheBaseDir)

	if endpoint == "" {
		return nil, fmt.Errorf("s3 endpoint must not be empty")
	}
	if accessKey == "" {
		return nil, fmt.Errorf("s3 access key must not be empty")
	}
	if secretKey == "" {
		return nil, fmt.Errorf("s3 secret key must not be empty")
	}
	if cacheBaseDir == "" {
		return nil, fmt.Errorf("cache base dir must not be empty")
	}

	secure, host := parseEndpoint(endpoint)

	client, err := minio.New(host, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: secure,
	})
	if err != nil {
		return nil, fmt.Errorf("create minio client: %w", err)
	}

	store := &MinIOStore{client: client, cacheBaseDir: cacheBaseDir}
	if err := store.ensureBucket(ctx); err != nil {
		return nil, err
	}
	return store, nil
}

// ensureBucket 确保 sandbox-snapshots bucket 存在，不存在则创建。
func (s *MinIOStore) ensureBucket(ctx context.Context) error {
	exists, err := s.client.BucketExists(ctx, snapshotBucket)
	if err != nil {
		return fmt.Errorf("check bucket %q: %w", snapshotBucket, err)
	}
	if exists {
		return nil
	}
	if err := s.client.MakeBucket(ctx, snapshotBucket, minio.MakeBucketOptions{}); err != nil {
		// 并发场景下可能已被另一进程创建
		exists2, _ := s.client.BucketExists(ctx, snapshotBucket)
		if !exists2 {
			return fmt.Errorf("make bucket %q: %w", snapshotBucket, err)
		}
	}
	return nil
}

// Upload 上传 mem.snap 和 disk.snap 到 MinIO，路径为 {templateID}/mem.snap 等。
func (s *MinIOStore) Upload(ctx context.Context, templateID string, memPath, diskPath string) error {
	if err := s.uploadFile(ctx, templateID, "mem.snap", memPath); err != nil {
		return err
	}
	return s.uploadFile(ctx, templateID, "disk.snap", diskPath)
}

func (s *MinIOStore) uploadFile(ctx context.Context, templateID, fileName, localPath string) error {
	key := templateID + "/" + fileName
	_, err := s.client.FPutObject(ctx, snapshotBucket, key, localPath, minio.PutObjectOptions{
		ContentType: "application/octet-stream",
	})
	if err != nil {
		return fmt.Errorf("upload %s: %w", key, err)
	}
	return nil
}

// Download 下载快照到本地缓存目录并返回路径。通过 ETag 比较避免重复下载。
// 使用 singleflight 保证同一 templateID 同时只有一个 goroutine 执行实际下载。
func (s *MinIOStore) Download(ctx context.Context, templateID string) (memPath, diskPath string, err error) {
	type result struct {
		memPath  string
		diskPath string
	}

	v, err, _ := s.dlGroup.Do(templateID, func() (any, error) {
		mem, disk, err := s.download(ctx, templateID)
		if err != nil {
			return nil, err
		}
		return &result{memPath: mem, diskPath: disk}, nil
	})
	if err != nil {
		return "", "", err
	}
	r := v.(*result)
	return r.memPath, r.diskPath, nil
}

// download 执行实际的下载逻辑（由 singleflight 保证同一 templateID 不并发执行）。
func (s *MinIOStore) download(ctx context.Context, templateID string) (memPath, diskPath string, err error) {
	cacheDir := filepath.Join(s.cacheBaseDir, templateID)
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return "", "", fmt.Errorf("create cache dir: %w", err)
	}

	memLocal := filepath.Join(cacheDir, "mem.snap")
	diskLocal := filepath.Join(cacheDir, "disk.snap")
	etagFile := filepath.Join(cacheDir, "etag.json")

	memKey := templateID + "/mem.snap"
	diskKey := templateID + "/disk.snap"

	// 从远端获取最新 ETag
	memStat, err := s.client.StatObject(ctx, snapshotBucket, memKey, minio.StatObjectOptions{})
	if err != nil {
		return "", "", fmt.Errorf("stat mem.snap: %w", err)
	}
	diskStat, err := s.client.StatObject(ctx, snapshotBucket, diskKey, minio.StatObjectOptions{})
	if err != nil {
		return "", "", fmt.Errorf("stat disk.snap: %w", err)
	}

	remoteMemETag := strings.Trim(memStat.ETag, `"`)
	remoteDiskETag := strings.Trim(diskStat.ETag, `"`)

	// 读取本地缓存 ETag
	cached := s.loadETagCache(etagFile)

	if cached.MemETag != remoteMemETag || !fileExists(memLocal) {
		if err := s.client.FGetObject(ctx, snapshotBucket, memKey, memLocal, minio.GetObjectOptions{}); err != nil {
			return "", "", fmt.Errorf("download mem.snap: %w", err)
		}
	}
	if cached.DiskETag != remoteDiskETag || !fileExists(diskLocal) {
		if err := s.client.FGetObject(ctx, snapshotBucket, diskKey, diskLocal, minio.GetObjectOptions{}); err != nil {
			return "", "", fmt.Errorf("download disk.snap: %w", err)
		}
	}

	// 更新本地 ETag 缓存（尽力写，失败不阻断主流程）
	_ = s.saveETagCache(etagFile, etagCache{MemETag: remoteMemETag, DiskETag: remoteDiskETag})

	return memLocal, diskLocal, nil
}

// Exists 检查对应 templateID 的 mem.snap 和 disk.snap 是否都存在于远端。
// 只要任一文件缺失就返回 false，确保 Download 不会因部分上传失败而出错。
func (s *MinIOStore) Exists(ctx context.Context, templateID string) (bool, error) {
	for _, suffix := range []string{"/mem.snap", "/disk.snap"} {
		key := templateID + suffix
		_, err := s.client.StatObject(ctx, snapshotBucket, key, minio.StatObjectOptions{})
		if err != nil {
			if isNotFound(err) {
				return false, nil
			}
			return false, fmt.Errorf("stat %s: %w", key, err)
		}
	}
	return true, nil
}

func (s *MinIOStore) loadETagCache(path string) etagCache {
	data, err := os.ReadFile(path)
	if err != nil {
		return etagCache{}
	}
	var c etagCache
	if err := json.Unmarshal(data, &c); err != nil {
		return etagCache{}
	}
	return c
}

func (s *MinIOStore) saveETagCache(path string, c etagCache) error {
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// parseEndpoint 从 endpoint 字符串中提取 host:port 和是否使用 TLS。
// 例：http://127.0.0.1:9000 → ("127.0.0.1:9000", false)
func parseEndpoint(endpoint string) (secure bool, host string) {
	if strings.HasPrefix(endpoint, "https://") {
		secure = true
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		// 不含 scheme，直接作为 host 使用
		return false, endpoint
	}
	return secure, u.Host
}

// isNotFound 检测 minio SDK 的 NoSuchKey / 404 错误。
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	resp := minio.ToErrorResponse(err)
	return resp.StatusCode == 404 || resp.Code == "NoSuchKey"
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
