package environment

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"arkloop/services/sandbox/internal/environment/contract"
	"arkloop/services/shared/objectstore"
	"arkloop/services/shared/workspaceblob"
)

func latestPointerKey(scope, ref string) string {
	switch strings.TrimSpace(scope) {
	case ScopeProfile:
		return contract.ProfileLatestKey(ref)
	case ScopeWorkspace:
		return contract.WorkspaceLatestKey(ref)
	default:
		return ""
	}
}

func manifestKey(scope, ref, revision string) string {
	switch strings.TrimSpace(scope) {
	case ScopeProfile:
		return contract.ProfileManifestKey(ref, revision)
	case ScopeWorkspace:
		return contract.WorkspaceManifestKey(ref, revision)
	default:
		return ""
	}
}

func blobKey(scope, ref, sha256 string) string {
	switch strings.TrimSpace(scope) {
	case ScopeProfile:
		return contract.ProfileBlobKey(ref, sha256)
	case ScopeWorkspace:
		return contract.WorkspaceBlobKey(ref, sha256)
	default:
		return ""
	}
}

func loadLatestPointer(ctx context.Context, store objectstore.BlobStore, scope, ref string) (*LatestPointer, error) {
	data, err := store.Get(ctx, latestPointerKey(scope, ref))
	if err != nil {
		return nil, err
	}
	var pointer LatestPointer
	if err := json.Unmarshal(data, &pointer); err != nil {
		return nil, fmt.Errorf("decode latest pointer: %w", err)
	}
	pointer.Version = CurrentManifestVersion
	pointer.Scope = strings.TrimSpace(pointer.Scope)
	pointer.Ref = strings.TrimSpace(pointer.Ref)
	pointer.Revision = strings.TrimSpace(pointer.Revision)
	pointer.UpdatedAt = strings.TrimSpace(pointer.UpdatedAt)
	if pointer.Scope != strings.TrimSpace(scope) || pointer.Ref != strings.TrimSpace(ref) {
		return nil, fmt.Errorf("latest pointer identity mismatch")
	}
	return &pointer, nil
}

func saveLatestPointer(ctx context.Context, store objectstore.BlobStore, pointer LatestPointer) error {
	pointer.Version = CurrentManifestVersion
	pointer.Scope = strings.TrimSpace(pointer.Scope)
	pointer.Ref = strings.TrimSpace(pointer.Ref)
	pointer.Revision = strings.TrimSpace(pointer.Revision)
	pointer.UpdatedAt = strings.TrimSpace(pointer.UpdatedAt)
	if pointer.Scope == "" || pointer.Ref == "" || pointer.Revision == "" {
		return fmt.Errorf("latest pointer is incomplete")
	}
	if err := store.WriteJSONAtomic(ctx, latestPointerKey(pointer.Scope, pointer.Ref), pointer); err != nil {
		return fmt.Errorf("write latest pointer: %w", err)
	}
	return nil
}

func loadManifest(ctx context.Context, store objectstore.BlobStore, scope, ref, revision string) (*Manifest, error) {
	data, err := store.Get(ctx, manifestKey(scope, ref, revision))
	if err != nil {
		return nil, err
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}
	normalized := NormalizeManifest(manifest)
	if normalized.Scope != strings.TrimSpace(scope) || normalized.Ref != strings.TrimSpace(ref) {
		return nil, fmt.Errorf("manifest identity mismatch")
	}
	if normalized.Revision != strings.TrimSpace(revision) {
		return nil, fmt.Errorf("manifest revision mismatch")
	}
	return &normalized, nil
}

func saveManifest(ctx context.Context, store objectstore.BlobStore, manifest Manifest) error {
	normalized := NormalizeManifest(manifest)
	data, err := json.Marshal(normalized)
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	if err := store.Put(ctx, manifestKey(normalized.Scope, normalized.Ref, normalized.Revision), data); err != nil {
		return fmt.Errorf("put manifest: %w", err)
	}
	return nil
}

func loadBlob(ctx context.Context, store objectstore.BlobStore, key string) ([]byte, error) {
	encoded, err := store.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	return workspaceblob.Decode(encoded)
}

func putBlobIfMissing(ctx context.Context, store objectstore.BlobStore, key string, data []byte) error {
	if key == "" {
		return fmt.Errorf("blob key must not be empty")
	}
	encoded, err := workspaceblob.Encode(data)
	if err != nil {
		return err
	}
	if _, err := store.PutIfAbsent(ctx, key, encoded); err != nil {
		return err
	}
	return nil
}

func hydrateScope(ctx context.Context, store objectstore.BlobStore, carrier Carrier, scope, ref, revision string) error {
	manifest, files, err := loadHydratedScope(ctx, store, scope, ref, revision)
	if err != nil {
		return err
	}
	return carrier.ApplyEnvironment(ctx, scope, manifest, files, true)
}

func loadHydratedScope(ctx context.Context, store objectstore.BlobStore, scope, ref, revision string) (Manifest, []FilePayload, error) {
	manifest, err := loadManifest(ctx, store, scope, ref, revision)
	if err != nil {
		return Manifest{}, nil, err
	}
	hydrated := BuildHydrateManifest(scope, *manifest, PrepareOptions{WorkspaceMode: WorkspaceHydrationFull})
	files := make([]FilePayload, 0)
	for _, entry := range hydrated.Entries {
		if entry.Type != EntryTypeFile || strings.TrimSpace(entry.SHA256) == "" || entry.Deleted {
			continue
		}
		data, err := loadBlob(ctx, store, blobKey(scope, ref, entry.SHA256))
		if err != nil {
			return Manifest{}, nil, err
		}
		files = append(files, EncodeFilePayload(entry.Path, data, entry))
	}
	return hydrated, files, nil
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
