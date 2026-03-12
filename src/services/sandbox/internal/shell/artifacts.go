package shell

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"

	"arkloop/services/sandbox/internal/logging"
	"arkloop/services/sandbox/internal/session"
	"arkloop/services/shared/objectstore"
	"github.com/google/uuid"
)

type artifactVersion struct {
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
	Data     string `json:"data,omitempty"`
}

type artifactUploadResult struct {
	Refs             []ArtifactRef
	NextKnown        map[string]artifactVersion
	CanAdvanceSeq    bool
	RetryableFailure bool
}

func collectArtifacts(
	ctx context.Context,
	sn *session.Session,
	sessionID string,
	commandSeq int64,
	store artifactStore,
	known map[string]artifactVersion,
	logger *logging.JSONLogger,
) artifactUploadResult {
	currentKnown := cloneArtifactSeen(known)
	if store == nil {
		return artifactUploadResult{NextKnown: currentKnown, CanAdvanceSeq: true}
	}

	fetchResult, err := sn.FetchArtifacts(ctx)
	if err != nil {
		logger.Warn("fetch shell artifacts failed", logging.LogFields{SessionID: &sessionID}, map[string]any{"error": err.Error()})
		return artifactUploadResult{NextKnown: currentKnown, RetryableFailure: true}
	}
	if fetchResult.Truncated {
		logger.Warn("shell artifacts truncated", logging.LogFields{SessionID: &sessionID}, map[string]any{"command_seq": commandSeq})
	}

	nextKnown := make(map[string]artifactVersion, len(fetchResult.Artifacts))
	refs := make([]ArtifactRef, 0, len(fetchResult.Artifacts))
	retryableFailure := false
	seen := make(map[string]struct{}, len(fetchResult.Artifacts))
	for _, entry := range fetchResult.Artifacts {
		safeName := filepath.Base(entry.Filename)
		if safeName == "." || safeName == ".." || safeName == "" {
			logger.Warn("shell artifact filename rejected", logging.LogFields{SessionID: &sessionID}, map[string]any{"filename": entry.Filename})
			continue
		}
		seen[safeName] = struct{}{}

		data, err := base64.StdEncoding.DecodeString(entry.Data)
		if err != nil {
			logger.Warn("decode shell artifact failed", logging.LogFields{SessionID: &sessionID}, map[string]any{"filename": safeName, "error": err.Error()})
			continue
		}

		version := newArtifactVersion(data, entry.MimeType)
		if current, ok := currentKnown[safeName]; ok && sameArtifactVersion(current, version) {
			nextKnown[safeName] = version
			continue
		}

		key := artifactObjectKey(sn.OrgID, sessionID, commandSeq, safeName)
		metadata := objectstore.ArtifactMetadata(objectstore.ArtifactOwnerKindRun, resolveArtifactOwnerRunID(sessionID), sn.OrgID, nil)
		if err := store.PutObject(ctx, key, data, objectstore.PutOptions{ContentType: entry.MimeType, Metadata: metadata}); err != nil {
			logger.Warn("upload shell artifact failed", logging.LogFields{SessionID: &sessionID}, map[string]any{"key": key, "error": err.Error()})
			retryableFailure = true
			continue
		}
		nextKnown[safeName] = version

		refs = append(refs, ArtifactRef{
			Key:      key,
			Filename: safeName,
			Size:     version.Size,
			MimeType: entry.MimeType,
		})
	}
	if fetchResult.Truncated {
		retainKnownArtifacts(nextKnown, currentKnown, seen)
	}

	return artifactUploadResult{
		Refs:             refs,
		NextKnown:        nextKnown,
		CanAdvanceSeq:    !retryableFailure,
		RetryableFailure: retryableFailure,
	}
}

func artifactObjectKey(orgID, sessionID string, commandSeq int64, filename string) string {
	return fmt.Sprintf("%s/%s/%d/%s", orgID, sessionID, commandSeq, filename)
}

func newArtifactVersion(data []byte, mimeType string) artifactVersion {
	sum := sha256.Sum256(data)
	return artifactVersion{
		Size:     int64(len(data)),
		SHA256:   hex.EncodeToString(sum[:]),
		MimeType: mimeType,
	}
}

func sameArtifactVersion(left, right artifactVersion) bool {
	return left.Size == right.Size && left.SHA256 == right.SHA256 && left.MimeType == right.MimeType
}

func retainKnownArtifacts(nextKnown, known map[string]artifactVersion, seen map[string]struct{}) {
	for name, version := range known {
		if _, ok := seen[name]; ok {
			continue
		}
		if _, exists := nextKnown[name]; exists {
			continue
		}
		nextKnown[name] = version
	}
}

type artifactStore interface {
	PutObject(ctx context.Context, key string, data []byte, options objectstore.PutOptions) error
}

func resolveArtifactOwnerRunID(sessionID string) string {
	if sessionID == "" {
		return ""
	}
	segments := strings.Split(sessionID, "/")
	if len(segments) == 0 {
		return sessionID
	}
	if _, err := uuid.Parse(segments[0]); err == nil {
		return segments[0]
	}
	return sessionID
}
