package http

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"io"
	nethttp "net/http"
	"testing"

	"arkloop/services/api/internal/auth"
	"github.com/google/uuid"
	"github.com/klauspost/compress/zstd"
)

func TestWorkspaceFilesReadAndAuthorize(t *testing.T) {
	env := buildArtifactEnv(t)

	thread, err := env.threadRepo.Create(context.Background(), env.aliceOrgID, &env.aliceUserID, nil, false)
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	run, _, err := env.runRepo.CreateRunWithStartedEvent(context.Background(), env.aliceOrgID, thread.ID, &env.aliceUserID, "run.started", nil)
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	workspaceRef := "wsref_test_workspace_viewer"
	if _, err := env.pool.Exec(context.Background(), `UPDATE runs SET workspace_ref = $2 WHERE id = $1`, run.ID, workspaceRef); err != nil {
		t.Fatalf("update run workspace_ref: %v", err)
	}

	archive := buildWorkspaceArchive(t, map[string][]byte{
		"/workspace/report.html": []byte("<html><body>ok</body></html>"),
		"/workspace/chart.png":   []byte("\x89PNG\r\n\x1a\nPNGDATA"),
		"/workspace/notes.md":    []byte("# hello\nworld\n"),
	})
	env.store.put(workspaceArchiveKey(workspaceRef), archive, "application/zstd", nil)

	t.Run("owner can read html", func(t *testing.T) {
		resp := doArtifactRequest(t, env.handler, "/v1/workspace-files?run_id="+run.ID.String()+"&path=/report.html", authHeader(env.aliceToken))
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("workspace html read: %d %s", resp.Code, resp.Body.String())
		}
		if got := resp.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
			t.Fatalf("unexpected content-type: %q", got)
		}
		if body := resp.Body.String(); body != "<html><body>ok</body></html>" {
			t.Fatalf("unexpected body: %q", body)
		}
	})

	t.Run("owner can read markdown as text", func(t *testing.T) {
		resp := doArtifactRequest(t, env.handler, "/v1/workspace-files?run_id="+run.ID.String()+"&path=/notes.md", authHeader(env.aliceToken))
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("workspace text read: %d %s", resp.Code, resp.Body.String())
		}
		if got := resp.Header().Get("Content-Type"); got != "text/markdown; charset=utf-8" {
			t.Fatalf("unexpected content-type: %q", got)
		}
	})

	t.Run("owner can read image", func(t *testing.T) {
		resp := doArtifactRequest(t, env.handler, "/v1/workspace-files?run_id="+run.ID.String()+"&path=/chart.png", authHeader(env.aliceToken))
		if resp.Code != nethttp.StatusOK {
			t.Fatalf("workspace image read: %d %s", resp.Code, resp.Body.String())
		}
		if got := resp.Header().Get("Content-Type"); got != "image/png" {
			t.Fatalf("unexpected content-type: %q", got)
		}
	})

	t.Run("reject path traversal", func(t *testing.T) {
		resp := doArtifactRequest(t, env.handler, "/v1/workspace-files?run_id="+run.ID.String()+"&path=../secret.txt", authHeader(env.aliceToken))
		assertErrorEnvelope(t, resp, nethttp.StatusBadRequest, "workspace_files.invalid_path")
	})

	t.Run("missing file returns not found", func(t *testing.T) {
		resp := doArtifactRequest(t, env.handler, "/v1/workspace-files?run_id="+run.ID.String()+"&path=/missing.txt", authHeader(env.aliceToken))
		assertErrorEnvelope(t, resp, nethttp.StatusNotFound, "workspace_files.not_found")
	})

	t.Run("missing workspace ref returns not found", func(t *testing.T) {
		run2, _, err := env.runRepo.CreateRunWithStartedEvent(context.Background(), env.aliceOrgID, thread.ID, &env.aliceUserID, "run.started", nil)
		if err != nil {
			t.Fatalf("create run2: %v", err)
		}
		resp := doArtifactRequest(t, env.handler, "/v1/workspace-files?run_id="+run2.ID.String()+"&path=/report.html", authHeader(env.aliceToken))
		assertErrorEnvelope(t, resp, nethttp.StatusNotFound, "workspace_files.not_found")
	})

	t.Run("other org member cannot read", func(t *testing.T) {
		registerOther := doJSON(env.handler, nethttp.MethodPost, "/v1/auth/register",
			map[string]any{"login": "bob-workspace", "password": "pwdpwdpwd", "email": "bob-workspace@test.com"},
			nil,
		)
		if registerOther.Code != nethttp.StatusCreated {
			t.Fatalf("register other: %d %s", registerOther.Code, registerOther.Body.String())
		}
		otherPayload := decodeJSONBody[registerResponse](t, registerOther.Body.Bytes())
		otherUserID, err := uuid.Parse(otherPayload.UserID)
		if err != nil {
			t.Fatalf("parse other user id: %v", err)
		}
		if _, err := env.membershipRepo.Create(context.Background(), env.aliceOrgID, otherUserID, auth.RoleOrgMember); err != nil {
			t.Fatalf("add org membership: %v", err)
		}
		_, otherKey, err := env.apiKeysRepo.Create(context.Background(), env.aliceOrgID, otherUserID, "workspace-reader", []string{auth.PermDataRunsRead})
		if err != nil {
			t.Fatalf("create other key: %v", err)
		}
		resp := doArtifactRequest(t, env.handler, "/v1/workspace-files?run_id="+run.ID.String()+"&path=/report.html", authHeader(otherKey))
		assertErrorEnvelope(t, resp, nethttp.StatusForbidden, "policy.denied")
	})
}

func TestWorkspaceFilesReadFromManifestState(t *testing.T) {
	env := buildArtifactEnv(t)

	thread, err := env.threadRepo.Create(context.Background(), env.aliceOrgID, &env.aliceUserID, nil, false)
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	run, _, err := env.runRepo.CreateRunWithStartedEvent(context.Background(), env.aliceOrgID, thread.ID, &env.aliceUserID, "run.started", nil)
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	workspaceRef := "wsref_test_workspace_manifest"
	if _, err := env.pool.Exec(context.Background(), `UPDATE runs SET workspace_ref = $2 WHERE id = $1`, run.ID, workspaceRef); err != nil {
		t.Fatalf("update run workspace_ref: %v", err)
	}

	env.store.put(workspaceLatestKey(workspaceRef), mustJSON(t, workspaceLatestPointer{Revision: "rev-1"}), "application/json", nil)
	env.store.put(workspaceManifestKey(workspaceRef, "rev-1"), mustJSON(t, workspaceManifest{Entries: []workspaceManifestEntry{{Path: "chart.png", Type: workspaceEntryTypeFile, SHA256: "sha-chart"}}}), "application/json", nil)
	env.store.put(workspaceBlobKey(workspaceRef, "sha-chart"), []byte("\x89PNG\r\n\x1a\nPNGDATA"), "application/octet-stream", nil)

	resp := doArtifactRequest(t, env.handler, "/v1/workspace-files?run_id="+run.ID.String()+"&path=/chart.png", authHeader(env.aliceToken))
	if resp.Code != nethttp.StatusOK {
		t.Fatalf("workspace manifest read: %d %s", resp.Code, resp.Body.String())
	}
	if got := resp.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("unexpected content-type: %q", got)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return payload
}

func buildWorkspaceArchive(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var tarBuffer bytes.Buffer
	tw := tar.NewWriter(&tarBuffer)
	for name, content := range files {
		header := &tar.Header{
			Name: name,
			Mode: 0o644,
			Size: int64(len(content)),
		}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatalf("write tar header: %v", err)
		}
		if _, err := tw.Write(content); err != nil {
			t.Fatalf("write tar content: %v", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}

	var compressed bytes.Buffer
	zw, err := zstd.NewWriter(&compressed)
	if err != nil {
		t.Fatalf("new zstd writer: %v", err)
	}
	if _, err := io.Copy(zw, bytes.NewReader(tarBuffer.Bytes())); err != nil {
		t.Fatalf("compress tar archive: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zstd writer: %v", err)
	}
	return compressed.Bytes()
}

func TestDetectWorkspaceContentTypeFallsBackToSniffing(t *testing.T) {
	got := detectWorkspaceContentType("/file.unknown", []byte("plain text"))
	if got == "" {
		t.Fatal("expected content type")
	}
}
