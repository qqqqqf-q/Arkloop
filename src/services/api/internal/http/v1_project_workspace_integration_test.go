package http

import (
	"context"
	"strings"
	"testing"
	"time"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	sharedenvironmentref "arkloop/services/shared/environmentref"

	"github.com/google/uuid"
)

type projectWorkspaceResponsePayload struct {
	ProjectID     string `json:"project_id"`
	WorkspaceRef  string `json:"workspace_ref"`
	OwnerUserID   string `json:"owner_user_id"`
	Status        string `json:"status"`
	LastUsedAt    string `json:"last_used_at"`
	ActiveSession *struct {
		SessionRef  string `json:"session_ref"`
		SessionType string `json:"session_type"`
		State       string `json:"state"`
		LastUsedAt  string `json:"last_used_at"`
	} `json:"active_session,omitempty"`
}

type projectWorkspaceFilesPayload struct {
	WorkspaceRef string `json:"workspace_ref"`
	Path         string `json:"path"`
	Items        []struct {
		Name        string  `json:"name"`
		Path        string  `json:"path"`
		Type        string  `json:"type"`
		Size        *int64  `json:"size,omitempty"`
		MtimeUnixMs *int64  `json:"mtime_unix_ms,omitempty"`
		MimeType    *string `json:"mime_type,omitempty"`
		HasChildren bool    `json:"has_children,omitempty"`
	} `json:"items"`
}

func TestProjectWorkspaceLazilyCreatesDefaultWorkspace(t *testing.T) {
	env := buildArtifactEnv(t)
	project := mustCreateTestProject(t, context.Background(), env.pool, env.aliceOrgID, &env.aliceUserID, "project-workspace-lazy")

	resp := doArtifactRequest(t, env.handler, "/v1/projects/"+project.ID.String()+"/workspace", authHeader(env.aliceToken))
	if resp.Code != 200 {
		t.Fatalf("get workspace: %d %s", resp.Code, resp.Body.String())
	}
	payload := decodeJSONBody[projectWorkspaceResponsePayload](t, resp.Body.Bytes())
	if payload.ProjectID != project.ID.String() {
		t.Fatalf("unexpected project_id: %q", payload.ProjectID)
	}
	expectedProfileRef := sharedenvironmentref.BuildProfileRef(env.aliceOrgID, &env.aliceUserID)
	expectedWorkspaceRef := sharedenvironmentref.BuildWorkspaceRef(env.aliceOrgID, expectedProfileRef, data.DefaultWorkspaceBindingScopeProject, project.ID)
	if payload.WorkspaceRef != expectedWorkspaceRef {
		t.Fatalf("unexpected workspace_ref: %q", payload.WorkspaceRef)
	}
	if payload.OwnerUserID != env.aliceUserID.String() {
		t.Fatalf("unexpected owner_user_id: %q", payload.OwnerUserID)
	}
	if payload.Status != "idle" {
		t.Fatalf("unexpected status: %q", payload.Status)
	}
	if payload.ActiveSession != nil {
		t.Fatalf("expected no active_session, got %#v", payload.ActiveSession)
	}
	if _, err := time.Parse(time.RFC3339Nano, payload.LastUsedAt); err != nil {
		t.Fatalf("invalid last_used_at: %v", err)
	}

	repeat := doArtifactRequest(t, env.handler, "/v1/projects/"+project.ID.String()+"/workspace", authHeader(env.aliceToken))
	if repeat.Code != 200 {
		t.Fatalf("repeat get workspace: %d %s", repeat.Code, repeat.Body.String())
	}
	repeatPayload := decodeJSONBody[projectWorkspaceResponsePayload](t, repeat.Body.Bytes())
	if repeatPayload.WorkspaceRef != payload.WorkspaceRef {
		t.Fatalf("expected same workspace_ref, got %q vs %q", payload.WorkspaceRef, repeatPayload.WorkspaceRef)
	}

	bindingsRepo, err := data.NewDefaultWorkspaceBindingsRepository(env.pool)
	if err != nil {
		t.Fatalf("new bindings repo: %v", err)
	}
	binding, err := bindingsRepo.Get(context.Background(), env.aliceOrgID, expectedProfileRef, data.DefaultWorkspaceBindingScopeProject, project.ID)
	if err != nil {
		t.Fatalf("get binding: %v", err)
	}
	if binding == nil || binding.WorkspaceRef != payload.WorkspaceRef {
		t.Fatalf("unexpected binding: %#v", binding)
	}

	profileRepo, err := data.NewProfileRegistriesRepository(env.pool)
	if err != nil {
		t.Fatalf("new profile repo: %v", err)
	}
	profileRegistry, err := profileRepo.Get(context.Background(), expectedProfileRef)
	if err != nil {
		t.Fatalf("get profile registry: %v", err)
	}
	if profileRegistry == nil || profileRegistry.DefaultWorkspaceRef == nil || *profileRegistry.DefaultWorkspaceRef != payload.WorkspaceRef {
		t.Fatalf("unexpected profile registry: %#v", profileRegistry)
	}

	workspaceRepo, err := data.NewWorkspaceRegistriesRepository(env.pool)
	if err != nil {
		t.Fatalf("new workspace repo: %v", err)
	}
	workspaceRegistry, err := workspaceRepo.Get(context.Background(), payload.WorkspaceRef)
	if err != nil {
		t.Fatalf("get workspace registry: %v", err)
	}
	if workspaceRegistry == nil || workspaceRegistry.ProjectID == nil || *workspaceRegistry.ProjectID != project.ID {
		t.Fatalf("unexpected workspace registry: %#v", workspaceRegistry)
	}
}

func TestProjectWorkspaceIsolatedPerUser(t *testing.T) {
	env := buildArtifactEnv(t)
	project := mustCreateTestProject(t, context.Background(), env.pool, env.aliceOrgID, &env.aliceUserID, "project-workspace-isolation")

	ownerResp := doArtifactRequest(t, env.handler, "/v1/projects/"+project.ID.String()+"/workspace", authHeader(env.aliceToken))
	if ownerResp.Code != 200 {
		t.Fatalf("owner workspace: %d %s", ownerResp.Code, ownerResp.Body.String())
	}
	ownerPayload := decodeJSONBody[projectWorkspaceResponsePayload](t, ownerResp.Body.Bytes())

	registerOther := doJSON(env.handler, "POST", "/v1/auth/register", map[string]any{
		"login":    "bob-project-workspace",
		"password": "pwdpwdpwd",
		"email":    "bob-project-workspace@test.com",
	}, nil)
	if registerOther.Code != 201 {
		t.Fatalf("register other: %d %s", registerOther.Code, registerOther.Body.String())
	}
	otherPayload := decodeJSONBody[registerResponse](t, registerOther.Body.Bytes())
	otherUserID, err := uuid.Parse(otherPayload.UserID)
	if err != nil {
		t.Fatalf("parse other user id: %v", err)
	}
	if _, err := env.membershipRepo.Create(context.Background(), env.aliceOrgID, otherUserID, auth.RoleOrgMember); err != nil {
		t.Fatalf("add membership: %v", err)
	}
	_, otherKey, err := env.apiKeysRepo.Create(context.Background(), env.aliceOrgID, otherUserID, "project-workspace-reader", []string{auth.PermDataProjectsRead, auth.PermDataRunsRead})
	if err != nil {
		t.Fatalf("create other key: %v", err)
	}

	otherResp := doArtifactRequest(t, env.handler, "/v1/projects/"+project.ID.String()+"/workspace", authHeader(otherKey))
	if otherResp.Code != 200 {
		t.Fatalf("other workspace: %d %s", otherResp.Code, otherResp.Body.String())
	}
	otherWorkspacePayload := decodeJSONBody[projectWorkspaceResponsePayload](t, otherResp.Body.Bytes())
	if otherWorkspacePayload.WorkspaceRef == ownerPayload.WorkspaceRef {
		t.Fatalf("expected different workspace_ref per user, got %q", otherWorkspacePayload.WorkspaceRef)
	}
}

func TestProjectWorkspaceStatusAndFilesFlow(t *testing.T) {
	env := buildArtifactEnv(t)
	project := mustCreateTestProject(t, context.Background(), env.pool, env.aliceOrgID, &env.aliceUserID, "project-workspace-files")

	workspaceResp := doArtifactRequest(t, env.handler, "/v1/projects/"+project.ID.String()+"/workspace", authHeader(env.aliceToken))
	if workspaceResp.Code != 200 {
		t.Fatalf("get workspace: %d %s", workspaceResp.Code, workspaceResp.Body.String())
	}
	workspacePayload := decodeJSONBody[projectWorkspaceResponsePayload](t, workspaceResp.Body.Bytes())

	emptyFilesResp := doArtifactRequest(t, env.handler, "/v1/projects/"+project.ID.String()+"/workspace/files", authHeader(env.aliceToken))
	if emptyFilesResp.Code != 200 {
		t.Fatalf("list empty files: %d %s", emptyFilesResp.Code, emptyFilesResp.Body.String())
	}
	emptyFiles := decodeJSONBody[projectWorkspaceFilesPayload](t, emptyFilesResp.Body.Bytes())
	if emptyFiles.Path != "/" || len(emptyFiles.Items) != 0 {
		t.Fatalf("unexpected empty files payload: %#v", emptyFiles)
	}

	setWorkspaceLatestManifest(t, env, workspacePayload.WorkspaceRef, "rev-project-1")
	if _, err := env.pool.Exec(context.Background(), `
		UPDATE workspace_registries
		   SET default_shell_session_ref = $2, last_used_at = now()
		 WHERE workspace_ref = $1`, workspacePayload.WorkspaceRef, "shref_project_active"); err != nil {
		t.Fatalf("update workspace registry: %v", err)
	}
	liveSessionID := "live-project-shell"
	if _, err := env.pool.Exec(context.Background(), `
		INSERT INTO shell_sessions (
			session_ref, session_type, org_id, profile_ref, workspace_ref,
			share_scope, state, live_session_id, last_used_at, metadata_json
		) VALUES ($1, 'shell', $2, $3, $4, 'workspace', 'ready', $5, now(), '{}'::jsonb)
		ON CONFLICT (session_ref) DO UPDATE SET
			live_session_id = EXCLUDED.live_session_id,
			state = EXCLUDED.state,
			last_used_at = now()`,
		"shref_project_active",
		env.aliceOrgID,
		sharedenvironmentref.BuildProfileRef(env.aliceOrgID, &env.aliceUserID),
		workspacePayload.WorkspaceRef,
		liveSessionID,
	); err != nil {
		t.Fatalf("insert shell session: %v", err)
	}

	env.store.put("workspaces/"+workspacePayload.WorkspaceRef+"/manifests/rev-project-1.json", mustJSON(t, map[string]any{
		"entries": []map[string]any{
			{"path": "src/main.go", "type": "file", "size": 14, "mtime_unix_ms": 1710000000000, "sha256": "sha-main"},
			{"path": "docs/readme.md", "type": "file", "size": 9, "mtime_unix_ms": 1710000001000, "sha256": "sha-docs"},
			{"path": "top.txt", "type": "file", "size": 4, "mtime_unix_ms": 1710000002000, "sha256": "sha-top"},
		},
	}), "application/json", nil)
	env.store.put("workspaces/"+workspacePayload.WorkspaceRef+"/blobs/sha-main", mustWorkspaceBlob(t, []byte("package main\n")), "application/octet-stream", nil)
	env.store.put("workspaces/"+workspacePayload.WorkspaceRef+"/blobs/sha-docs", mustWorkspaceBlob(t, []byte("# arkloop")), "application/octet-stream", nil)
	env.store.put("workspaces/"+workspacePayload.WorkspaceRef+"/blobs/sha-top", mustWorkspaceBlob(t, []byte("top\n")), "application/octet-stream", nil)

	activeWorkspaceResp := doArtifactRequest(t, env.handler, "/v1/projects/"+project.ID.String()+"/workspace", authHeader(env.aliceToken))
	if activeWorkspaceResp.Code != 200 {
		t.Fatalf("get active workspace: %d %s", activeWorkspaceResp.Code, activeWorkspaceResp.Body.String())
	}
	activeWorkspace := decodeJSONBody[projectWorkspaceResponsePayload](t, activeWorkspaceResp.Body.Bytes())
	if activeWorkspace.Status != "active" || activeWorkspace.ActiveSession == nil {
		t.Fatalf("unexpected active workspace payload: %#v", activeWorkspace)
	}

	rootFilesResp := doArtifactRequest(t, env.handler, "/v1/projects/"+project.ID.String()+"/workspace/files?path=/", authHeader(env.aliceToken))
	if rootFilesResp.Code != 200 {
		t.Fatalf("list root files: %d %s", rootFilesResp.Code, rootFilesResp.Body.String())
	}
	rootFiles := decodeJSONBody[projectWorkspaceFilesPayload](t, rootFilesResp.Body.Bytes())
	if len(rootFiles.Items) != 3 {
		t.Fatalf("unexpected root item count: %#v", rootFiles.Items)
	}
	if rootFiles.Items[0].Type != "dir" || rootFiles.Items[0].Name != "docs" {
		t.Fatalf("unexpected first root item: %#v", rootFiles.Items[0])
	}
	if rootFiles.Items[1].Type != "dir" || rootFiles.Items[1].Name != "src" {
		t.Fatalf("unexpected second root item: %#v", rootFiles.Items[1])
	}
	if rootFiles.Items[2].Type != "file" || rootFiles.Items[2].Name != "top.txt" {
		t.Fatalf("unexpected third root item: %#v", rootFiles.Items[2])
	}

	srcFilesResp := doArtifactRequest(t, env.handler, "/v1/projects/"+project.ID.String()+"/workspace/files?path=/src", authHeader(env.aliceToken))
	if srcFilesResp.Code != 200 {
		t.Fatalf("list src files: %d %s", srcFilesResp.Code, srcFilesResp.Body.String())
	}
	srcFiles := decodeJSONBody[projectWorkspaceFilesPayload](t, srcFilesResp.Body.Bytes())
	if len(srcFiles.Items) != 1 || srcFiles.Items[0].Name != "main.go" || srcFiles.Items[0].MimeType == nil || !strings.Contains(*srcFiles.Items[0].MimeType, "text") {
		t.Fatalf("unexpected src files payload: %#v", srcFiles)
	}

	fileResp := doArtifactRequest(t, env.handler, "/v1/projects/"+project.ID.String()+"/workspace/file?path=/src/main.go", authHeader(env.aliceToken))
	if fileResp.Code != 200 || fileResp.Body.String() != "package main\n" {
		t.Fatalf("read file: %d %q", fileResp.Code, fileResp.Body.String())
	}
	if got := fileResp.Header().Get("Content-Type"); !strings.Contains(got, "text") {
		t.Fatalf("unexpected content-type: %q", got)
	}

	dirResp := doArtifactRequest(t, env.handler, "/v1/projects/"+project.ID.String()+"/workspace/file?path=/src", authHeader(env.aliceToken))
	assertErrorEnvelope(t, dirResp, 404, "workspace_files.not_found")

	invalidResp := doArtifactRequest(t, env.handler, "/v1/projects/"+project.ID.String()+"/workspace/file?path=../secret.txt", authHeader(env.aliceToken))
	assertErrorEnvelope(t, invalidResp, 400, "workspace_files.invalid_path")
}

func TestProjectWorkspaceMissingDefaultSessionStaysIdleAndReadsFiles(t *testing.T) {
	env := buildArtifactEnv(t)
	project := mustCreateTestProject(t, context.Background(), env.pool, env.aliceOrgID, &env.aliceUserID, "project-workspace-missing-default-session")

	workspaceResp := doArtifactRequest(t, env.handler, "/v1/projects/"+project.ID.String()+"/workspace", authHeader(env.aliceToken))
	if workspaceResp.Code != 200 {
		t.Fatalf("get workspace: %d %s", workspaceResp.Code, workspaceResp.Body.String())
	}
	workspacePayload := decodeJSONBody[projectWorkspaceResponsePayload](t, workspaceResp.Body.Bytes())

	setWorkspaceLatestManifest(t, env, workspacePayload.WorkspaceRef, "rev-project-idle-1")
	if _, err := env.pool.Exec(context.Background(), `
		UPDATE workspace_registries
		   SET default_shell_session_ref = $2, last_used_at = now()
		 WHERE workspace_ref = $1`, workspacePayload.WorkspaceRef, "shref_missing_default"); err != nil {
		t.Fatalf("update workspace registry: %v", err)
	}

	env.store.put("workspaces/"+workspacePayload.WorkspaceRef+"/manifests/rev-project-idle-1.json", mustJSON(t, map[string]any{
		"entries": []map[string]any{{"path": "notes/todo.md", "type": "file", "size": 7, "mtime_unix_ms": 1710000003000, "sha256": "sha-todo"}},
	}), "application/json", nil)
	env.store.put("workspaces/"+workspacePayload.WorkspaceRef+"/blobs/sha-todo", mustWorkspaceBlob(t, []byte("todo\n1\n")), "application/octet-stream", nil)

	idleWorkspaceResp := doArtifactRequest(t, env.handler, "/v1/projects/"+project.ID.String()+"/workspace", authHeader(env.aliceToken))
	if idleWorkspaceResp.Code != 200 {
		t.Fatalf("get idle workspace: %d %s", idleWorkspaceResp.Code, idleWorkspaceResp.Body.String())
	}
	idleWorkspace := decodeJSONBody[projectWorkspaceResponsePayload](t, idleWorkspaceResp.Body.Bytes())
	if idleWorkspace.Status != "idle" {
		t.Fatalf("expected idle status, got %#v", idleWorkspace)
	}
	if idleWorkspace.ActiveSession != nil {
		t.Fatalf("expected no active session, got %#v", idleWorkspace.ActiveSession)
	}

	filesResp := doArtifactRequest(t, env.handler, "/v1/projects/"+project.ID.String()+"/workspace/files?path=/notes", authHeader(env.aliceToken))
	if filesResp.Code != 200 {
		t.Fatalf("list idle files: %d %s", filesResp.Code, filesResp.Body.String())
	}
	filesPayload := decodeJSONBody[projectWorkspaceFilesPayload](t, filesResp.Body.Bytes())
	if len(filesPayload.Items) != 1 || filesPayload.Items[0].Name != "todo.md" {
		t.Fatalf("unexpected files payload: %#v", filesPayload)
	}

	fileResp := doArtifactRequest(t, env.handler, "/v1/projects/"+project.ID.String()+"/workspace/file?path=/notes/todo.md", authHeader(env.aliceToken))
	if fileResp.Code != 200 || fileResp.Body.String() != "todo\n1\n" {
		t.Fatalf("read idle file: %d %q", fileResp.Code, fileResp.Body.String())
	}
}

func TestProjectWorkspaceClosedDefaultSessionStaysIdle(t *testing.T) {
	env := buildArtifactEnv(t)
	project := mustCreateTestProject(t, context.Background(), env.pool, env.aliceOrgID, &env.aliceUserID, "project-workspace-closed-default-session")

	workspaceResp := doArtifactRequest(t, env.handler, "/v1/projects/"+project.ID.String()+"/workspace", authHeader(env.aliceToken))
	if workspaceResp.Code != 200 {
		t.Fatalf("get workspace: %d %s", workspaceResp.Code, workspaceResp.Body.String())
	}
	workspacePayload := decodeJSONBody[projectWorkspaceResponsePayload](t, workspaceResp.Body.Bytes())
	profileRef := sharedenvironmentref.BuildProfileRef(env.aliceOrgID, &env.aliceUserID)

	if _, err := env.pool.Exec(context.Background(), `
		UPDATE workspace_registries
		   SET default_shell_session_ref = $2, last_used_at = now()
		 WHERE workspace_ref = $1`, workspacePayload.WorkspaceRef, "shref_closed_default"); err != nil {
		t.Fatalf("update workspace registry: %v", err)
	}
	if _, err := env.pool.Exec(context.Background(), `
		INSERT INTO shell_sessions (
			session_ref, session_type, org_id, profile_ref, workspace_ref,
			share_scope, state, live_session_id, last_used_at, metadata_json
		) VALUES ($1, 'shell', $2, $3, $4, 'workspace', 'closed', NULL, now(), '{}'::jsonb)
		ON CONFLICT (session_ref) DO UPDATE SET
			state = EXCLUDED.state,
			live_session_id = EXCLUDED.live_session_id,
			last_used_at = now()`,
		"shref_closed_default",
		env.aliceOrgID,
		profileRef,
		workspacePayload.WorkspaceRef,
	); err != nil {
		t.Fatalf("insert closed session: %v", err)
	}

	idleWorkspaceResp := doArtifactRequest(t, env.handler, "/v1/projects/"+project.ID.String()+"/workspace", authHeader(env.aliceToken))
	if idleWorkspaceResp.Code != 200 {
		t.Fatalf("get idle workspace: %d %s", idleWorkspaceResp.Code, idleWorkspaceResp.Body.String())
	}
	idleWorkspace := decodeJSONBody[projectWorkspaceResponsePayload](t, idleWorkspaceResp.Body.Bytes())
	if idleWorkspace.Status != "idle" {
		t.Fatalf("expected idle status, got %#v", idleWorkspace)
	}
	if idleWorkspace.ActiveSession != nil {
		t.Fatalf("expected no active session, got %#v", idleWorkspace.ActiveSession)
	}
}

func TestProjectWorkspaceUsesLatestLiveSessionAcrossWorkspace(t *testing.T) {
	env := buildArtifactEnv(t)
	project := mustCreateTestProject(t, context.Background(), env.pool, env.aliceOrgID, &env.aliceUserID, "project-workspace-latest-live-session")

	workspaceResp := doArtifactRequest(t, env.handler, "/v1/projects/"+project.ID.String()+"/workspace", authHeader(env.aliceToken))
	if workspaceResp.Code != 200 {
		t.Fatalf("get workspace: %d %s", workspaceResp.Code, workspaceResp.Body.String())
	}
	workspacePayload := decodeJSONBody[projectWorkspaceResponsePayload](t, workspaceResp.Body.Bytes())
	profileRef := sharedenvironmentref.BuildProfileRef(env.aliceOrgID, &env.aliceUserID)
	olderUsedAt := time.Now().UTC().Add(-time.Hour)
	newerUsedAt := time.Now().UTC()

	if _, err := env.pool.Exec(context.Background(), `
		UPDATE workspace_registries
		   SET default_shell_session_ref = $2, last_used_at = now()
		 WHERE workspace_ref = $1`, workspacePayload.WorkspaceRef, "shref_stale_default"); err != nil {
		t.Fatalf("update workspace registry: %v", err)
	}
	if _, err := env.pool.Exec(context.Background(), `
		INSERT INTO shell_sessions (
			session_ref, session_type, org_id, profile_ref, workspace_ref,
			share_scope, state, live_session_id, last_used_at, metadata_json
		) VALUES
			($1, 'shell', $2, $3, $4, 'workspace', 'ready', $5, $6, '{}'::jsonb),
			($7, 'browser', $2, $3, $4, 'workspace', 'busy', $8, $9, '{}'::jsonb)
		ON CONFLICT (session_ref) DO UPDATE SET
			state = EXCLUDED.state,
			live_session_id = EXCLUDED.live_session_id,
			last_used_at = EXCLUDED.last_used_at,
			updated_at = now()`,
		"shref_workspace_old_live",
		env.aliceOrgID,
		profileRef,
		workspacePayload.WorkspaceRef,
		"live-shell-old",
		olderUsedAt,
		"brref_workspace_new_live",
		"live-browser-new",
		newerUsedAt,
	); err != nil {
		t.Fatalf("insert live sessions: %v", err)
	}

	activeWorkspaceResp := doArtifactRequest(t, env.handler, "/v1/projects/"+project.ID.String()+"/workspace", authHeader(env.aliceToken))
	if activeWorkspaceResp.Code != 200 {
		t.Fatalf("get active workspace: %d %s", activeWorkspaceResp.Code, activeWorkspaceResp.Body.String())
	}
	activeWorkspace := decodeJSONBody[projectWorkspaceResponsePayload](t, activeWorkspaceResp.Body.Bytes())
	if activeWorkspace.Status != "active" || activeWorkspace.ActiveSession == nil {
		t.Fatalf("unexpected active workspace payload: %#v", activeWorkspace)
	}
	if activeWorkspace.ActiveSession.SessionRef != "brref_workspace_new_live" {
		t.Fatalf("expected latest live session, got %#v", activeWorkspace.ActiveSession)
	}
	if activeWorkspace.ActiveSession.SessionType != "browser" {
		t.Fatalf("expected browser session type, got %#v", activeWorkspace.ActiveSession)
	}
}

func TestProjectWorkspacePermissionsAndOrgIsolation(t *testing.T) {
	env := buildArtifactEnv(t)
	project := mustCreateTestProject(t, context.Background(), env.pool, env.aliceOrgID, &env.aliceUserID, "project-workspace-policy")

	_, projectsOnlyKey, err := env.apiKeysRepo.Create(context.Background(), env.aliceOrgID, env.aliceUserID, "projects-only", []string{auth.PermDataProjectsRead})
	if err != nil {
		t.Fatalf("create projects-only key: %v", err)
	}
	workspaceResp := doArtifactRequest(t, env.handler, "/v1/projects/"+project.ID.String()+"/workspace", authHeader(projectsOnlyKey))
	if workspaceResp.Code != 200 {
		t.Fatalf("projects-only workspace: %d %s", workspaceResp.Code, workspaceResp.Body.String())
	}
	filesResp := doArtifactRequest(t, env.handler, "/v1/projects/"+project.ID.String()+"/workspace/files", authHeader(projectsOnlyKey))
	assertErrorEnvelope(t, filesResp, 403, "auth.forbidden")

	registerOther := doJSON(env.handler, "POST", "/v1/auth/register", map[string]any{
		"login":    "outsider-project-workspace",
		"password": "pwdpwdpwd",
		"email":    "outsider-project-workspace@test.com",
	}, nil)
	if registerOther.Code != 201 {
		t.Fatalf("register outsider: %d %s", registerOther.Code, registerOther.Body.String())
	}
	outsider := decodeJSONBody[registerResponse](t, registerOther.Body.Bytes())
	otherOrgResp := doArtifactRequest(t, env.handler, "/v1/projects/"+project.ID.String()+"/workspace", authHeader(outsider.AccessToken))
	assertErrorEnvelope(t, otherOrgResp, 404, "projects.not_found")
}
