package objectstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	filesystemObjectsDir = "objects"
	filesystemMetaDir    = ".meta"
	defaultContentType   = "application/octet-stream"
)

type FilesystemOpener struct {
	rootDir string
}

type FilesystemStore struct {
	rootDir string
	bucket  string
}

type filesystemObjectMetadata struct {
	ContentType string            `json:"content_type,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	Size        int64             `json:"size,omitempty"`
	ETag        string            `json:"etag,omitempty"`
}

func NewFilesystemOpener(rootDir string) *FilesystemOpener {
	return &FilesystemOpener{rootDir: strings.TrimSpace(rootDir)}
}

func (o *FilesystemOpener) Open(_ context.Context, bucket string) (Store, error) {
	bucket, err := validateBucketName(bucket)
	if err != nil {
		return nil, err
	}
	rootDir := strings.TrimSpace(o.rootDir)
	if rootDir == "" {
		return nil, fmt.Errorf("filesystem root dir must not be empty")
	}
	store := &FilesystemStore{
		rootDir: rootDir,
		bucket:  bucket,
	}
	if err := os.MkdirAll(store.objectsRoot(), 0o755); err != nil {
		return nil, fmt.Errorf("create filesystem object dir: %w", err)
	}
	if err := os.MkdirAll(store.metaRoot(), 0o755); err != nil {
		return nil, fmt.Errorf("create filesystem metadata dir: %w", err)
	}
	return store, nil
}

func (s *FilesystemStore) Put(ctx context.Context, key string, data []byte) error {
	return s.PutObject(ctx, key, data, PutOptions{})
}

func (s *FilesystemStore) PutObject(_ context.Context, key string, data []byte, options PutOptions) error {
	dataPath, metadataPath, normalizedKey, err := s.objectPaths(key)
	if err != nil {
		return err
	}
	metadata := filesystemObjectMetadata{
		ContentType: strings.TrimSpace(options.ContentType),
		Metadata:    normalizeMetadata(options.Metadata),
		Size:        int64(len(data)),
		ETag:        contentETag(data),
	}
	if err := atomicWriteFile(dataPath, data, 0o600); err != nil {
		return fmt.Errorf("put object %q: %w", normalizedKey, err)
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("marshal metadata %q: %w", normalizedKey, err)
	}
	if err := atomicWriteFile(metadataPath, encoded, 0o600); err != nil {
		return fmt.Errorf("put metadata %q: %w", normalizedKey, err)
	}
	return nil
}

func (s *FilesystemStore) PutIfAbsent(ctx context.Context, key string, data []byte) (bool, error) {
	if _, err := s.Head(ctx, key); err == nil {
		return false, nil
	} else if !IsNotFound(err) {
		return false, err
	}
	if err := s.Put(ctx, key, data); err != nil {
		return false, err
	}
	return true, nil
}

func (s *FilesystemStore) Get(_ context.Context, key string) ([]byte, error) {
	dataPath, _, normalizedKey, err := s.objectPaths(key)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(dataPath)
	if err != nil {
		return nil, fmt.Errorf("get object %q: %w", normalizedKey, err)
	}
	return data, nil
}

func (s *FilesystemStore) GetWithContentType(ctx context.Context, key string) ([]byte, string, error) {
	data, err := s.Get(ctx, key)
	if err != nil {
		return nil, "", err
	}
	metadata, _, err := s.readMetadata(key)
	if err != nil && !IsNotFound(err) {
		return nil, "", err
	}
	contentType := defaultContentType
	if err == nil && strings.TrimSpace(metadata.ContentType) != "" {
		contentType = strings.TrimSpace(metadata.ContentType)
	}
	return data, contentType, nil
}

func (s *FilesystemStore) Head(_ context.Context, key string) (ObjectInfo, error) {
	dataPath, _, normalizedKey, err := s.objectPaths(key)
	if err != nil {
		return ObjectInfo{}, err
	}
	info, err := os.Stat(dataPath)
	if err != nil {
		return ObjectInfo{}, fmt.Errorf("head object %q: %w", normalizedKey, err)
	}
	metadata, metadataPath, err := s.readMetadata(normalizedKey)
	if err != nil && !IsNotFound(err) {
		return ObjectInfo{}, err
	}
	object := ObjectInfo{
		Key:         normalizedKey,
		ContentType: defaultContentType,
		Size:        info.Size(),
	}
	if err == nil {
		object.ContentType = strings.TrimSpace(metadata.ContentType)
		if object.ContentType == "" {
			object.ContentType = defaultContentType
		}
		object.Metadata = normalizeMetadata(metadata.Metadata)
		object.ETag = strings.TrimSpace(metadata.ETag)
		if metadata.Size > 0 {
			object.Size = metadata.Size
		}
	}
	if object.ETag == "" {
		etag, digestErr := fileETag(dataPath)
		if digestErr != nil {
			return ObjectInfo{}, fmt.Errorf("head object %q: %w", normalizedKey, digestErr)
		}
		object.ETag = etag
	}
	_ = metadataPath
	return object, nil
}

func (s *FilesystemStore) Delete(_ context.Context, key string) error {
	dataPath, metadataPath, normalizedKey, err := s.objectPaths(key)
	if err != nil {
		return err
	}
	if err := os.Remove(dataPath); err != nil {
		return fmt.Errorf("delete object %q: %w", normalizedKey, err)
	}
	if err := os.Remove(metadataPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete metadata %q: %w", normalizedKey, err)
	}
	s.cleanupEmptyParents(s.objectsRoot(), filepath.Dir(dataPath))
	s.cleanupEmptyParents(s.metaRoot(), filepath.Dir(metadataPath))
	return nil
}

func (s *FilesystemStore) ListPrefix(_ context.Context, prefix string) ([]ObjectInfo, error) {
	normalizedPrefix, err := validateObjectPrefix(prefix)
	if err != nil {
		return nil, err
	}
	objects := make([]ObjectInfo, 0)
	err = filepath.WalkDir(s.objectsRoot(), func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(s.objectsRoot(), current)
		if err != nil {
			return err
		}
		key := filepath.ToSlash(rel)
		if normalizedPrefix != "" && !strings.HasPrefix(key, normalizedPrefix) {
			return nil
		}
		info, err := s.Head(context.Background(), key)
		if err != nil {
			return err
		}
		objects = append(objects, info)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list objects with prefix %q: %w", normalizedPrefix, err)
	}
	return objects, nil
}

func (s *FilesystemStore) WriteJSONAtomic(ctx context.Context, key string, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal json %q: %w", key, err)
	}
	return s.PutObject(ctx, key, payload, PutOptions{ContentType: "application/json"})
}

func (s *FilesystemStore) SetLifecycleExpirationDays(ctx context.Context, days int) error {
	_ = ctx
	_ = days
	return nil
}

func (s *FilesystemStore) readMetadata(key string) (filesystemObjectMetadata, string, error) {
	_, metadataPath, normalizedKey, err := s.objectPaths(key)
	if err != nil {
		return filesystemObjectMetadata{}, "", err
	}
	data, err := os.ReadFile(metadataPath)
	if err != nil {
		return filesystemObjectMetadata{}, metadataPath, fmt.Errorf("head object %q: %w", normalizedKey, err)
	}
	var metadata filesystemObjectMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return filesystemObjectMetadata{}, metadataPath, fmt.Errorf("decode metadata %q: %w", normalizedKey, err)
	}
	metadata.ContentType = strings.TrimSpace(metadata.ContentType)
	metadata.Metadata = normalizeMetadata(metadata.Metadata)
	metadata.ETag = strings.TrimSpace(metadata.ETag)
	return metadata, metadataPath, nil
}

func (s *FilesystemStore) objectPaths(key string) (string, string, string, error) {
	normalizedKey, err := validateObjectKey(key)
	if err != nil {
		return "", "", "", err
	}
	dataPath, err := safeJoin(s.objectsRoot(), normalizedKey)
	if err != nil {
		return "", "", "", err
	}
	metadataPath, err := safeJoin(s.metaRoot(), normalizedKey+".json")
	if err != nil {
		return "", "", "", err
	}
	return dataPath, metadataPath, normalizedKey, nil
}

func (s *FilesystemStore) bucketRoot() string {
	return filepath.Join(s.rootDir, s.bucket)
}

func (s *FilesystemStore) objectsRoot() string {
	return filepath.Join(s.bucketRoot(), filesystemObjectsDir)
}

func (s *FilesystemStore) metaRoot() string {
	return filepath.Join(s.bucketRoot(), filesystemMetaDir)
}

func validateBucketName(bucket string) (string, error) {
	bucket = strings.TrimSpace(bucket)
	if bucket == "" {
		return "", fmt.Errorf("bucket must not be empty")
	}
	if strings.Contains(bucket, "/") || strings.Contains(bucket, "\\") || bucket == "." || bucket == ".." {
		return "", fmt.Errorf("bucket must be a simple name")
	}
	return bucket, nil
}

func validateObjectKey(key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", fmt.Errorf("object key must not be empty")
	}
	if strings.HasPrefix(key, "/") || strings.Contains(key, "\\") {
		return "", fmt.Errorf("invalid object key: %s", key)
	}
	segments := strings.Split(key, "/")
	for _, segment := range segments {
		if segment == "" || segment == "." || segment == ".." {
			return "", fmt.Errorf("invalid object key: %s", key)
		}
	}
	return key, nil
}

func validateObjectPrefix(prefix string) (string, error) {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return "", nil
	}
	if strings.HasPrefix(prefix, "/") || strings.Contains(prefix, "\\") {
		return "", fmt.Errorf("invalid object prefix: %s", prefix)
	}
	segments := strings.Split(prefix, "/")
	for index, segment := range segments {
		if segment == "" {
			if index == len(segments)-1 {
				continue
			}
			return "", fmt.Errorf("invalid object prefix: %s", prefix)
		}
		if segment == "." || segment == ".." {
			return "", fmt.Errorf("invalid object prefix: %s", prefix)
		}
	}
	return prefix, nil
}

func safeJoin(root, name string) (string, error) {
	joined := filepath.Join(root, filepath.FromSlash(name))
	rel, err := filepath.Rel(root, joined)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes root: %s", name)
	}
	return joined, nil
}

func atomicWriteFile(path string, data []byte, perm fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tempFile, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	defer func() { _ = os.Remove(tempPath) }()
	if _, err := tempFile.Write(data); err != nil {
		_ = tempFile.Close()
		return err
	}
	if err := tempFile.Chmod(perm); err != nil {
		_ = tempFile.Close()
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}

func contentETag(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func fileETag(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func (s *FilesystemStore) cleanupEmptyParents(root, start string) {
	root = filepath.Clean(root)
	current := filepath.Clean(start)
	for current != root && current != "." && current != string(os.PathSeparator) {
		entries, err := os.ReadDir(current)
		if err != nil || len(entries) > 0 {
			return
		}
		if err := os.Remove(current); err != nil {
			return
		}
		current = filepath.Dir(current)
	}
}

var _ Store = (*FilesystemStore)(nil)
var _ BlobStore = (*FilesystemStore)(nil)
var _ LifecycleConfigurator = (*FilesystemStore)(nil)
var _ BucketOpener = (*FilesystemOpener)(nil)
