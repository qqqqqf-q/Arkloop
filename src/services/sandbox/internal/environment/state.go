package environment

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"arkloop/services/shared/objectstore"
	"github.com/klauspost/compress/zstd"
)

func manifestKey(scope, ref string) string {
	prefix := scopePrefix(scope, ref)
	if prefix == "" {
		return ""
	}
	return prefix + "/manifest.json"
}

func blobKey(scope, ref, sha256 string) string {
	prefix := scopePrefix(scope, ref)
	sha256 = strings.TrimSpace(sha256)
	if prefix == "" || sha256 == "" {
		return ""
	}
	return prefix + "/blobs/" + sha256 + ".zst"
}

func scopePrefix(scope, ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	switch strings.TrimSpace(scope) {
	case ScopeProfile:
		return "profiles/" + ref
	case ScopeWorkspace:
		return "workspaces/" + ref
	default:
		return ""
	}
}

func loadLatestManifest(ctx context.Context, store Store, scope, ref string) (*Manifest, error) {
	data, err := store.Get(ctx, manifestKey(scope, ref))
	if err != nil {
		return nil, err
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("decode %s manifest: %w", scope, err)
	}
	normalized := NormalizeManifest(manifest)
	if normalized.Scope != strings.TrimSpace(scope) || normalized.Ref != strings.TrimSpace(ref) {
		return nil, fmt.Errorf("%s manifest identity mismatch", scope)
	}
	if normalized.Version != CurrentManifestVersion {
		return nil, fmt.Errorf("unsupported %s manifest version: %d", scope, normalized.Version)
	}
	return &normalized, nil
}

func saveManifest(ctx context.Context, store Store, manifest Manifest) error {
	normalized := NormalizeManifest(manifest)
	data, err := json.Marshal(normalized)
	if err != nil {
		return fmt.Errorf("marshal %s manifest: %w", normalized.Scope, err)
	}
	if err := store.Put(ctx, manifestKey(normalized.Scope, normalized.Ref), data); err != nil {
		return fmt.Errorf("put %s manifest: %w", normalized.Scope, err)
	}
	return nil
}

func loadBlob(ctx context.Context, store Store, key string) ([]byte, error) {
	encoded, err := store.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	return decompressBlob(encoded)
}

func putBlobIfMissing(ctx context.Context, store Store, key string, data []byte) error {
	if key == "" {
		return fmt.Errorf("blob key must not be empty")
	}
	if _, err := store.Head(ctx, key); err == nil {
		return nil
	} else if !objectstore.IsNotFound(err) {
		return err
	}
	encoded, err := compressBlob(data)
	if err != nil {
		return err
	}
	if err := store.Put(ctx, key, encoded); err != nil {
		return err
	}
	return nil
}

func loadLegacyArchive(ctx context.Context, store Store, scope, ref string) ([]byte, error) {
	return store.Get(ctx, legacyArchiveKey(scope, ref))
}

func legacyArchiveKey(scope, ref string) string {
	switch strings.TrimSpace(scope) {
	case ScopeProfile:
		return profileKey(ref)
	case ScopeWorkspace:
		return workspaceKey(ref)
	default:
		return ""
	}
}

func compressBlob(data []byte) ([]byte, error) {
	var buffer bytes.Buffer
	encoder, err := zstd.NewWriter(&buffer)
	if err != nil {
		return nil, fmt.Errorf("create zstd writer: %w", err)
	}
	if _, err := encoder.Write(data); err != nil {
		encoder.Close()
		return nil, fmt.Errorf("compress blob: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("close zstd writer: %w", err)
	}
	return buffer.Bytes(), nil
}

func decompressBlob(data []byte) ([]byte, error) {
	decoder, err := zstd.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("open zstd blob: %w", err)
	}
	defer decoder.Close()
	decoded, err := io.ReadAll(decoder)
	if err != nil {
		return nil, fmt.Errorf("decompress blob: %w", err)
	}
	return decoded, nil
}

func nextRevision(now time.Time) string {
	return fmt.Sprintf("%d", now.UTC().UnixNano())
}
