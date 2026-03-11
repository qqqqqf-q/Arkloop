package http

import (
	"context"
	"encoding/json"
	nethttp "net/http"
	"testing"

	"arkloop/services/api/internal/auth"
	"arkloop/services/shared/workspaceblob"
	"github.com/google/uuid"
)

func TestWorkspaceFilesReadAndAuthorize(t *testing.T) {
	env := buildArtifactEnv(t)
	project := mustCreateTestProject(t, context.Background(), env.pool, env.aliceOrgID, &env.aliceUserID, "workspace-files-owner")

	thread, err := env.threadRepo.Create(context.Background(), env.aliceOrgID, &env.aliceUserID, project.ID, data.ThreadModeChat, nil, false)
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
	setWorkspaceLatestManifest(t, env, workspaceRef, "rev-1")

	env.store.put(workspaceManifestKey(workspaceRef, "rev-1"), mustJSON(t, workspaceManifest{Entries: []workspaceManifestEntry{
		{Path: "report.html", Type: workspaceEntryTypeFile, SHA256: "sha-report"},
		{Path: "chart.png", Type: workspaceEntryTypeFile, SHA256: "sha-chart"},
		{Path: "notes.md", Type: workspaceEntryTypeFile, SHA256: "sha-notes"},
	}}), "application/json", nil)
	env.store.put(workspaceBlobKey(workspaceRef, "sha-report"), mustWorkspaceBlob(t, []byte("<html><body>ok</body></html>")), "application/octet-stream", nil)
	env.store.put(workspaceBlobKey(workspaceRef, "sha-chart"), mustWorkspaceBlob(t, []byte("\x89PNG\r\n\x1a\nPNGDATA")), "application/octet-stream", nil)
	env.store.put(workspaceBlobKey(workspaceRef, "sha-notes"), mustWorkspaceBlob(t, []byte("# hello\nworld\n")), "application/octet-stream", nil)

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
	project := mustCreateTestProject(t, context.Background(), env.pool, env.aliceOrgID, &env.aliceUserID, "workspace-files-manifest")

	thread, err := env.threadRepo.Create(context.Background(), env.aliceOrgID, &env.aliceUserID, project.ID, data.ThreadModeChat, nil, false)
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
	setWorkspaceLatestManifest(t, env, workspaceRef, "rev-1")

	env.store.put(workspaceManifestKey(workspaceRef, "rev-1"), mustJSON(t, workspaceManifest{Entries: []workspaceManifestEntry{{Path: "chart.png", Type: workspaceEntryTypeFile, SHA256: "sha-chart"}}}), "application/json", nil)
	env.store.put(workspaceBlobKey(workspaceRef, "sha-chart"), mustWorkspaceBlob(t, []byte("\x89PNG\r\n\x1a\nPNGDATA")), "application/octet-stream", nil)

	resp := doArtifactRequest(t, env.handler, "/v1/workspace-files?run_id="+run.ID.String()+"&path=/chart.png", authHeader(env.aliceToken))
	if resp.Code != nethttp.StatusOK {
		t.Fatalf("workspace manifest read: %d %s", resp.Code, resp.Body.String())
	}
	if got := resp.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("unexpected content-type: %q", got)
	}
}

func TestWorkspaceFilesRejectLegacyArchiveOnly(t *testing.T) {
	env := buildArtifactEnv(t)
	project := mustCreateTestProject(t, context.Background(), env.pool, env.aliceOrgID, &env.aliceUserID, "workspace-files-json")

	thread, err := env.threadRepo.Create(context.Background(), env.aliceOrgID, &env.aliceUserID, project.ID, data.ThreadModeChat, nil, false)
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	run, _, err := env.runRepo.CreateRunWithStartedEvent(context.Background(), env.aliceOrgID, thread.ID, &env.aliceUserID, "run.started", nil)
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	workspaceRef := "wsref_test_workspace_legacy_only"
	if _, err := env.pool.Exec(context.Background(), `UPDATE runs SET workspace_ref = $2 WHERE id = $1`, run.ID, workspaceRef); err != nil {
		t.Fatalf("update run workspace_ref: %v", err)
	}

	env.store.put("workspaces/"+workspaceRef+"/state.tar.zst", []byte("legacy-only"), "application/zstd", nil)

	resp := doArtifactRequest(t, env.handler, "/v1/workspace-files?run_id="+run.ID.String()+"&path=/report.html", authHeader(env.aliceToken))
	assertErrorEnvelope(t, resp, nethttp.StatusNotFound, "workspace_files.not_found")
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return payload
}

func mustWorkspaceBlob(t *testing.T, data []byte) []byte {
	t.Helper()
	encoded, err := workspaceblob.Encode(data)
	if err != nil {
		t.Fatalf("encode workspace blob: %v", err)
	}
	return encoded
}

func setWorkspaceLatestManifest(t *testing.T, env artifactTestEnv, workspaceRef, revision string) {
	t.Helper()
	if _, err := env.pool.Exec(context.Background(), `
		INSERT INTO workspace_registries (workspace_ref, org_id, latest_manifest_rev)
		VALUES ($1, $2, $3)
		ON CONFLICT (workspace_ref) DO UPDATE SET
			latest_manifest_rev = EXCLUDED.latest_manifest_rev,
			updated_at = now()`, workspaceRef, env.aliceOrgID, revision); err != nil {
		t.Fatalf("upsert workspace registry: %v", err)
	}
}

func TestDetectWorkspaceContentTypeFallsBackToSniffing(t *testing.T) {
	got := detectWorkspaceContentType("/file.unknown", []byte("plain text"))
	if got == "" {
		t.Fatal("expected content type")
	}
}
