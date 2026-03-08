package shell

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"arkloop/services/shared/objectstore"
)

const (
	shellStateVersion = 1
	defaultRestoreCwd = "/workspace"
)

type stateStore interface {
	Put(ctx context.Context, key string, data []byte) error
	Get(ctx context.Context, key string) ([]byte, error)
}

var _ stateStore = (*objectstore.Store)(nil)

type checkpointManifest struct {
	Version        int                        `json:"version"`
	Revision       string                     `json:"revision"`
	OrgID          string                     `json:"org_id"`
	SessionID      string                     `json:"session_id"`
	Cwd            string                     `json:"cwd"`
	EnvSnapshot    map[string]string          `json:"env_snapshot,omitempty"`
	LastCommandSeq int64                      `json:"last_command_seq"`
	UploadedSeq    int64                      `json:"uploaded_seq"`
	ArtifactSeen   map[string]artifactVersion `json:"artifact_seen,omitempty"`
	CreatedAt      string                     `json:"created_at"`
}

type latestCheckpointPointer struct {
	Revision  string `json:"revision"`
	UpdatedAt string `json:"updated_at"`
}

func nextCheckpointRevision(now time.Time) string {
	return fmt.Sprintf("%d", now.UTC().UnixNano())
}

func latestPointerKey(orgID, sessionID string) string {
	return strings.TrimSpace(orgID) + "/" + strings.TrimSpace(sessionID) + "/latest.json"
}

func checkpointManifestKey(orgID, sessionID, revision string) string {
	return strings.TrimSpace(orgID) + "/" + strings.TrimSpace(sessionID) + "/checkpoints/" + strings.TrimSpace(revision) + "/manifest.json"
}

func checkpointArchiveKey(orgID, sessionID, revision string) string {
	return strings.TrimSpace(orgID) + "/" + strings.TrimSpace(sessionID) + "/checkpoints/" + strings.TrimSpace(revision) + "/state.tar.zst"
}

func saveCheckpoint(ctx context.Context, store stateStore, manifest checkpointManifest, archive []byte) error {
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("marshal checkpoint manifest: %w", err)
	}
	if err := store.Put(ctx, checkpointArchiveKey(manifest.OrgID, manifest.SessionID, manifest.Revision), archive); err != nil {
		return fmt.Errorf("put checkpoint archive: %w", err)
	}
	if err := store.Put(ctx, checkpointManifestKey(manifest.OrgID, manifest.SessionID, manifest.Revision), manifestBytes); err != nil {
		return fmt.Errorf("put checkpoint manifest: %w", err)
	}
	pointerBytes, err := json.Marshal(latestCheckpointPointer{Revision: manifest.Revision, UpdatedAt: manifest.CreatedAt})
	if err != nil {
		return fmt.Errorf("marshal checkpoint pointer: %w", err)
	}
	if err := store.Put(ctx, latestPointerKey(manifest.OrgID, manifest.SessionID), pointerBytes); err != nil {
		return fmt.Errorf("put checkpoint pointer: %w", err)
	}
	return nil
}

func loadLatestCheckpoint(ctx context.Context, store stateStore, orgID, sessionID string) (*checkpointManifest, []byte, error) {
	pointerBytes, err := store.Get(ctx, latestPointerKey(orgID, sessionID))
	if err != nil {
		return nil, nil, err
	}
	var pointer latestCheckpointPointer
	if err := json.Unmarshal(pointerBytes, &pointer); err != nil {
		return nil, nil, fmt.Errorf("decode checkpoint pointer: %w", err)
	}
	if strings.TrimSpace(pointer.Revision) == "" {
		return nil, nil, fmt.Errorf("checkpoint pointer missing revision")
	}
	manifestBytes, err := store.Get(ctx, checkpointManifestKey(orgID, sessionID, pointer.Revision))
	if err != nil {
		return nil, nil, err
	}
	var manifest checkpointManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return nil, nil, fmt.Errorf("decode checkpoint manifest: %w", err)
	}
	normalizeArtifactVersions(manifest.ArtifactSeen)
	if manifest.Version != shellStateVersion {
		return nil, nil, fmt.Errorf("unsupported checkpoint version: %d", manifest.Version)
	}
	if manifest.OrgID != orgID || manifest.SessionID != sessionID {
		return nil, nil, fmt.Errorf("checkpoint identity mismatch")
	}
	archive, err := store.Get(ctx, checkpointArchiveKey(orgID, sessionID, pointer.Revision))
	if err != nil {
		return nil, nil, err
	}
	return &manifest, archive, nil
}

func normalizeArtifactVersions(versions map[string]artifactVersion) {
	for name, version := range versions {
		if version.SHA256 == "" && strings.TrimSpace(version.Data) != "" {
			if decoded, err := base64.StdEncoding.DecodeString(version.Data); err == nil {
				normalized := newArtifactVersion(decoded, version.MimeType)
				version.Size = normalized.Size
				version.SHA256 = normalized.SHA256
			}
		}
		version.Data = ""
		versions[name] = version
	}
}

func copyLatestCheckpoint(ctx context.Context, store stateStore, orgID, fromSessionID, toSessionID string) (string, error) {
	manifest, archive, err := loadLatestCheckpoint(ctx, store, orgID, fromSessionID)
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	copied := *manifest
	copied.SessionID = strings.TrimSpace(toSessionID)
	copied.Revision = nextCheckpointRevision(now)
	copied.CreatedAt = now.Format(time.RFC3339Nano)
	copied.ArtifactSeen = cloneArtifactSeen(manifest.ArtifactSeen)
	if err := saveCheckpoint(ctx, store, copied, archive); err != nil {
		return "", err
	}
	return copied.Revision, nil
}
