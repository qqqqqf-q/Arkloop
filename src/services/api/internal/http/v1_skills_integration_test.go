//go:build !desktop

package http

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"io"
	nethttp "net/http"
	"testing"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/migrate"
	"arkloop/services/api/internal/observability"
	"arkloop/services/api/internal/testutil"
	sharedenvironmentref "arkloop/services/shared/environmentref"
	"arkloop/services/shared/skillstore"
	"arkloop/services/shared/workspaceblob"
	"arkloop/services/shared/database"
	"github.com/google/uuid"
)

type skillsTestEnv struct {
	handler     nethttp.Handler
	appDB        database.DB
	aliceToken  string
	aliceUserID uuid.UUID
	aliceOrgID  uuid.UUID
	skillStore  *fakeHTTPArtifactStore
}

func TestSkillPackageLifecycle(t *testing.T) {
	env := buildSkillsEnv(t)
	seedSkillPackageObjects(t, env.skillStore, "grep-helper", "1")
	promoteToPlatformAdmin(t, env.appDB, env.aliceUserID)

	registerResp := doJSON(env.handler, nethttp.MethodPost, "/v1/admin/skill-packages", map[string]any{
		"skill_key": "grep-helper",
		"version":   "1",
	}, authHeader(env.aliceToken))
	if registerResp.Code != nethttp.StatusCreated {
		t.Fatalf("register skill package: %d %s", registerResp.Code, registerResp.Body.String())
	}

	installResp := doJSON(env.handler, nethttp.MethodPost, "/v1/profiles/me/skills/install", map[string]any{
		"skill_key": "grep-helper",
		"version":   "1",
	}, authHeader(env.aliceToken))
	if installResp.Code != nethttp.StatusCreated {
		t.Fatalf("install skill package: %d %s", installResp.Code, installResp.Body.String())
	}

	workspaceRef := "wsref_skills_test"
	if _, err := env.appDB.Exec(context.Background(), `
		INSERT INTO workspace_registries (workspace_ref, org_id, owner_user_id, metadata_json)
		VALUES ($1, $2, $3, '{}'::jsonb)
	`, workspaceRef, env.aliceOrgID, env.aliceUserID); err != nil {
		t.Fatalf("insert workspace registry: %v", err)
	}

	enableResp := doJSON(env.handler, nethttp.MethodPut, "/v1/workspaces/"+workspaceRef+"/skills", map[string]any{
		"skills": []map[string]any{{"skill_key": "grep-helper", "version": "1"}},
	}, authHeader(env.aliceToken))
	if enableResp.Code != nethttp.StatusOK {
		t.Fatalf("enable skill package: %d %s", enableResp.Code, enableResp.Body.String())
	}

	listResp := doJSON(env.handler, nethttp.MethodGet, "/v1/workspaces/"+workspaceRef+"/skills", nil, authHeader(env.aliceToken))
	if listResp.Code != nethttp.StatusOK {
		t.Fatalf("list workspace skills: %d %s", listResp.Code, listResp.Body.String())
	}
	body := decodeJSONBody[map[string]any](t, listResp.Body.Bytes())
	items, ok := body["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("unexpected workspace skills payload: %#v", body)
	}

	profileRef := sharedenvironmentref.BuildProfileRef(env.aliceOrgID, &env.aliceUserID)
	var profileMetadata []byte
	if err := env.appDB.QueryRow(context.Background(), `SELECT metadata_json FROM profile_registries WHERE profile_ref = $1`, profileRef).Scan(&profileMetadata); err != nil {
		t.Fatalf("load profile metadata: %v", err)
	}
	if !bytes.Contains(profileMetadata, []byte("grep-helper@1")) {
		t.Fatalf("expected installed skill ref in profile metadata, got %s", string(profileMetadata))
	}
	var defaultWorkspaceRef *string
	if err := env.appDB.QueryRow(context.Background(), `SELECT default_workspace_ref FROM profile_registries WHERE profile_ref = $1`, profileRef).Scan(&defaultWorkspaceRef); err != nil {
		t.Fatalf("load profile default workspace: %v", err)
	}
	if defaultWorkspaceRef == nil || *defaultWorkspaceRef != workspaceRef {
		t.Fatalf("expected profile default workspace %q, got %#v", workspaceRef, defaultWorkspaceRef)
	}
}

func buildSkillsEnv(t *testing.T) skillsTestEnv {
	t.Helper()
	db := testutil.SetupPostgresDatabase(t, "skills_http")
	if _, err := migrate.Up(context.Background(), db.DSN); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ctx := context.Background()
	appDB, _, err := data.NewPool(ctx, db.DSN, data.PoolLimits{MaxConns: 16, MinConns: 0})
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	t.Cleanup(func() { appDB.Close() })
	logger := observability.NewJSONLogger("test", io.Discard)
	passwordHasher, err := auth.NewBcryptPasswordHasher(0)
	if err != nil {
		t.Fatalf("new password hasher: %v", err)
	}
	tokenService, err := auth.NewJwtAccessTokenService(apiKeysTestJWTSecret, 3600, 2592000)
	if err != nil {
		t.Fatalf("new token service: %v", err)
	}
	userRepo, _ := data.NewUserRepository(appDB)
	credRepo, _ := data.NewUserCredentialRepository(appDB)
	membershipRepo, _ := data.NewOrgMembershipRepository(appDB)
	refreshTokenRepo, _ := data.NewRefreshTokenRepository(appDB)
	auditRepo, _ := data.NewAuditLogRepository(appDB)
	apiKeysRepo, _ := data.NewAPIKeysRepository(appDB)
	personasRepo, _ := data.NewPersonasRepository(appDB)
	skillPackagesRepo, _ := data.NewSkillPackagesRepository(appDB)
	profileSkillInstallsRepo, _ := data.NewProfileSkillInstallsRepository(appDB)
	workspaceSkillEnableRepo, _ := data.NewWorkspaceSkillEnablementsRepository(appDB)
	profileRegistriesRepo, _ := data.NewProfileRegistriesRepository(appDB)
	workspaceRegistriesRepo, _ := data.NewWorkspaceRegistriesRepository(appDB)
	platformSettingsRepo, _ := data.NewPlatformSettingsRepository(appDB)
	jobRepo, _ := data.NewJobRepository(appDB)
	authService, _ := auth.NewService(userRepo, credRepo, membershipRepo, passwordHasher, tokenService, refreshTokenRepo, nil)
	registrationService, _ := auth.NewRegistrationService(appDB, passwordHasher, tokenService, refreshTokenRepo, jobRepo)
	store := newFakeHTTPArtifactStore()
	auditWriter := audit.NewWriter(auditRepo, membershipRepo, logger)
	handler := NewHandler(HandlerConfig{
		DB:                appDB,
		Logger:                   logger,
		AuthService:              authService,
		RegistrationService:      registrationService,
		OrgMembershipRepo:        membershipRepo,
		APIKeysRepo:              apiKeysRepo,
		AuditWriter:              auditWriter,
		PersonasRepo:             personasRepo,
		SkillPackagesRepo:        skillPackagesRepo,
		ProfileSkillInstallsRepo: profileSkillInstallsRepo,
		WorkspaceSkillEnableRepo: workspaceSkillEnableRepo,
		ProfileRegistriesRepo:    profileRegistriesRepo,
		WorkspaceRegistriesRepo:  workspaceRegistriesRepo,
		PlatformSettingsRepo:     platformSettingsRepo,
		ArtifactStore:            store,
		MessageAttachmentStore:   store,
		EnvironmentStore:         store,
		SkillStore:               store,
	})
	regResp := doJSON(handler, nethttp.MethodPost, "/v1/auth/register", map[string]any{
		"login":    "alice-skills",
		"password": "pwd12345",
		"email":    "alice-skills@test.com",
	}, nil)
	if regResp.Code != nethttp.StatusCreated {
		t.Fatalf("register: %d %s", regResp.Code, regResp.Body.String())
	}
	regPayload := decodeJSONBody[registerResponse](t, regResp.Body.Bytes())
	aliceUserID := uuid.MustParse(regPayload.UserID)
	meResp := doJSON(handler, nethttp.MethodGet, "/v1/me", nil, authHeader(regPayload.AccessToken))
	if meResp.Code != nethttp.StatusOK {
		t.Fatalf("me: %d %s", meResp.Code, meResp.Body.String())
	}
	mePayload := decodeJSONBody[meResponse](t, meResp.Body.Bytes())
	aliceOrgID := uuid.MustParse(mePayload.OrgID)
	loginResp := doJSON(handler, nethttp.MethodPost, "/v1/auth/login", map[string]any{"login": "alice-skills", "password": "pwd12345"}, nil)
	if loginResp.Code != nethttp.StatusOK {
		t.Fatalf("login: %d %s", loginResp.Code, loginResp.Body.String())
	}
	loginPayload := decodeJSONBody[loginResponse](t, loginResp.Body.Bytes())
	return skillsTestEnv{handler: handler, appDB: appDB, aliceToken: loginPayload.AccessToken, aliceUserID: aliceUserID, aliceOrgID: aliceOrgID, skillStore: store}
}

func seedSkillPackageObjects(t *testing.T, store *fakeHTTPArtifactStore, skillKey, version string) {
	t.Helper()
	manifest := skillstore.PackageManifest{
		SkillKey:        skillKey,
		Version:         version,
		DisplayName:     "Grep Helper",
		InstructionPath: skillstore.InstructionPathDefault,
	}
	manifest = skillstore.NormalizeManifest(manifest)
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	store.put(skillstore.DerivedManifestKey(skillKey, version), manifestBytes, "application/json", nil)

	var bundle bytes.Buffer
	tarWriter := tar.NewWriter(&bundle)
	writeTarFile(t, tarWriter, "skill.yaml", []byte("skill_key: grep-helper\nversion: \"1\"\ndisplay_name: Grep Helper\ninstruction_path: SKILL.md\n"), 0o644)
	writeTarFile(t, tarWriter, "SKILL.md", []byte("Use grep-helper skill."), 0o644)
	writeTarFile(t, tarWriter, "scripts/run.sh", []byte("#!/bin/sh\necho ok\n"), 0o755)
	if err := tarWriter.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}
	encoded, err := workspaceblob.Encode(bundle.Bytes())
	if err != nil {
		t.Fatalf("encode bundle: %v", err)
	}
	store.put(skillstore.DerivedBundleKey(skillKey, version), encoded, "application/zstd", nil)
}

func writeTarFile(t *testing.T, writer *tar.Writer, name string, data []byte, mode int64) {
	t.Helper()
	if err := writer.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(data))}); err != nil {
		t.Fatalf("write tar header: %v", err)
	}
	if _, err := writer.Write(data); err != nil {
		t.Fatalf("write tar body: %v", err)
	}
}

func promoteToPlatformAdmin(t *testing.T, db database.DB, userID uuid.UUID) {
	t.Helper()
	if _, err := db.Exec(context.Background(), `UPDATE org_memberships SET role = 'platform_admin' WHERE user_id = $1`, userID); err != nil {
		t.Fatalf("promote platform admin: %v", err)
	}
}
