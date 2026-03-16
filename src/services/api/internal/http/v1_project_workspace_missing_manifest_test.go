//go:build !desktop

package http

import (
	"context"
	"testing"
)

func TestProjectWorkspaceFilesReturnsEmptyListWhenManifestObjectMissing(t *testing.T) {
	env := buildArtifactEnv(t)
	project := mustCreateTestProject(t, context.Background(), env.appDB, env.aliceOrgID, &env.aliceUserID, "project-workspace-missing-manifest")

	workspaceResp := doArtifactRequest(t, env.handler, "/v1/projects/"+project.ID.String()+"/workspace", authHeader(env.aliceToken))
	if workspaceResp.Code != 200 {
		t.Fatalf("get workspace: %d %s", workspaceResp.Code, workspaceResp.Body.String())
	}
	workspacePayload := decodeJSONBody[projectWorkspaceResponsePayload](t, workspaceResp.Body.Bytes())
	setWorkspaceLatestManifest(t, env, workspacePayload.WorkspaceRef, "rev-missing")

	filesResp := doArtifactRequest(t, env.handler, "/v1/projects/"+project.ID.String()+"/workspace/files", authHeader(env.aliceToken))
	if filesResp.Code != 200 {
		t.Fatalf("list files with missing manifest: %d %s", filesResp.Code, filesResp.Body.String())
	}
	filesPayload := decodeJSONBody[projectWorkspaceFilesPayload](t, filesResp.Body.Bytes())
	if filesPayload.Path != "/" || len(filesPayload.Items) != 0 {
		t.Fatalf("unexpected files payload: %#v", filesPayload)
	}

	fileResp := doArtifactRequest(t, env.handler, "/v1/projects/"+project.ID.String()+"/workspace/file?path=/src/main.go", authHeader(env.aliceToken))
	assertErrorEnvelope(t, fileResp, 404, "workspace_files.not_found")
}
