package resourcecopy

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"arkloop/services/shared/objectstore"
	"arkloop/services/worker/internal/tools"
	"github.com/google/uuid"
)

type fakeStore struct {
	data        map[string][]byte
	contentType map[string]string
	metadata    map[string]map[string]string
}

func (s fakeStore) Put(context.Context, string, []byte) error { return nil }
func (s fakeStore) PutObject(context.Context, string, []byte, objectstore.PutOptions) error {
	return nil
}
func (s fakeStore) Get(_ context.Context, key string) ([]byte, error) { return s.data[key], nil }
func (s fakeStore) GetWithContentType(_ context.Context, key string) ([]byte, string, error) {
	return s.data[key], s.contentType[key], nil
}
func (s fakeStore) Head(_ context.Context, key string) (objectstore.ObjectInfo, error) {
	return objectstore.ObjectInfo{Key: key, ContentType: s.contentType[key], Metadata: s.metadata[key], Size: int64(len(s.data[key]))}, nil
}
func (s fakeStore) Delete(context.Context, string) error { return nil }
func (s fakeStore) ListPrefix(context.Context, string) ([]objectstore.ObjectInfo, error) {
	return nil, nil
}

func TestResourceCopyArtifactToWorkspace(t *testing.T) {
	accountID := uuid.New()
	runID := uuid.New()
	workDir := t.TempDir()
	key := accountID.String() + "/" + runID.String() + "/preview.html"
	targetPath := filepath.Join(workDir, "copied", "preview.html")
	store := fakeStore{
		data:        map[string][]byte{key: []byte("<h1>ok</h1>")},
		contentType: map[string]string{key: "text/html"},
		metadata: map[string]map[string]string{
			key: {objectstore.ArtifactMetaAccountID: accountID.String()},
		},
	}
	exec := NewExecutor(store, nil)

	result := exec.Execute(context.Background(), ToolName, map[string]any{
		"source_uri":  "artifact:" + key,
		"target_path": targetPath,
	}, tools.ExecutionContext{RunID: runID, AccountID: &accountID, WorkDir: workDir}, "")

	if result.Error != nil {
		t.Fatalf("unexpected error: %#v", result.Error)
	}
	got, err := os.ReadFile(filepath.Join(workDir, "copied", "preview.html"))
	if err != nil {
		t.Fatalf("read copied file: %v", err)
	}
	if string(got) != "<h1>ok</h1>" {
		t.Fatalf("unexpected copied content: %q", got)
	}
	wantPath := filepath.ToSlash(targetPath)
	if result.ResultJSON["file_path"] != wantPath {
		t.Fatalf("unexpected file path: %#v", result.ResultJSON["file_path"])
	}
	if _, ok := result.ResultJSON["workspace_uri"]; ok {
		t.Fatalf("did not expect workspace uri for local temp workdir: %#v", result.ResultJSON["workspace_uri"])
	}
}

func TestResourceCopyRejectsOtherAccount(t *testing.T) {
	accountID := uuid.New()
	otherAccountID := uuid.New()
	key := otherAccountID.String() + "/run/file.png"
	store := fakeStore{
		data:        map[string][]byte{key: []byte("png")},
		contentType: map[string]string{key: "image/png"},
		metadata: map[string]map[string]string{
			key: {objectstore.ArtifactMetaAccountID: otherAccountID.String()},
		},
	}
	exec := NewExecutor(store, nil)

	result := exec.Execute(context.Background(), ToolName, map[string]any{
		"source_uri":  "artifact:" + key,
		"target_path": filepath.Join(t.TempDir(), "file.png"),
	}, tools.ExecutionContext{RunID: uuid.New(), AccountID: &accountID, WorkDir: t.TempDir()}, "")

	if result.Error == nil || result.Error.ErrorClass != errorForbidden {
		t.Fatalf("expected forbidden error, got %#v", result.Error)
	}
}
