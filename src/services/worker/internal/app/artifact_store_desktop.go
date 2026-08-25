package app

import (
	"context"
	"fmt"

	"arkloop/services/shared/desktop"
	"arkloop/services/shared/objectstore"
)

func openDesktopArtifactStore(ctx context.Context) (objectstore.Store, error) {
	dataDir, err := desktop.ResolveDataDir("")
	if err != nil {
		return nil, err
	}
	return objectstore.NewFilesystemOpener(desktop.StorageRoot(dataDir)).Open(ctx, objectstore.ArtifactBucket)
}

func openDesktopMessageAttachmentStore(ctx context.Context) (objectstore.Store, error) {
	dataDir, err := desktop.ResolveDataDir("")
	if err != nil {
		return nil, err
	}
	return objectstore.NewFilesystemOpener(desktop.StorageRoot(dataDir)).Open(ctx, "message-attachments")
}

func openDesktopRolloutStore(ctx context.Context) (objectstore.BlobStore, error) {
	dataDir, err := desktop.ResolveDataDir("")
	if err != nil {
		return nil, err
	}
	store, err := objectstore.NewFilesystemOpener(desktop.StorageRoot(dataDir)).Open(ctx, objectstore.RolloutBucket)
	if err != nil {
		return nil, err
	}
	blobStore, ok := store.(objectstore.BlobStore)
	if !ok {
		return nil, fmt.Errorf("rollout store does not implement blob store")
	}
	return blobStore, nil
}
