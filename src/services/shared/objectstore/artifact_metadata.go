package objectstore

import "strings"

const (
	ArtifactOwnerKindRun = "run"

	ArtifactMetaOwnerKind = "owner_kind"
	ArtifactMetaOwnerID   = "owner_id"
	ArtifactMetaAccountID = "org_id"
	ArtifactMetaThreadID  = "thread_id"
)

func ArtifactMetadata(ownerKind, ownerID, accountID string, threadID *string) map[string]string {
	metadata := map[string]string{}
	putArtifactMetadata(metadata, ArtifactMetaOwnerKind, ownerKind)
	putArtifactMetadata(metadata, ArtifactMetaOwnerID, ownerID)
	putArtifactMetadata(metadata, ArtifactMetaAccountID, accountID)
	if threadID != nil {
		putArtifactMetadata(metadata, ArtifactMetaThreadID, *threadID)
	}
	if len(metadata) == 0 {
		return nil
	}
	return metadata
}

func putArtifactMetadata(target map[string]string, key string, value string) {
	cleaned := strings.TrimSpace(value)
	if cleaned == "" {
		return
	}
	target[key] = cleaned
}
