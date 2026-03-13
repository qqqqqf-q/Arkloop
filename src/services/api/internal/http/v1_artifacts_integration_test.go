//go:build !desktop

package http

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	nethttp "net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/migrate"
	"arkloop/services/api/internal/observability"
	"arkloop/services/api/internal/testutil"
	"arkloop/services/shared/objectstore"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type artifactTestEnv struct {
	handler         nethttp.Handler
	pool            *pgxpool.Pool
	apiKeysRepo     *data.APIKeysRepository
	membershipRepo  *data.AccountMembershipRepository
	threadRepo      *data.ThreadRepository
	threadShareRepo *data.ThreadShareRepository
	runRepo         *data.RunEventRepository
	tokenService    *auth.JwtAccessTokenService
	aliceToken      string
	aliceUserID     uuid.UUID
	aliceAccountID      uuid.UUID
	store           *fakeHTTPArtifactStore
}

type fakeArtifactObject struct {
	data        []byte
	contentType string
	metadata    map[string]string
}

type fakeHTTPArtifactStore struct {
	objects map[string]fakeArtifactObject
}

func newFakeHTTPArtifactStore() *fakeHTTPArtifactStore {
	return &fakeHTTPArtifactStore{objects: map[string]fakeArtifactObject{}}
}

func (s *fakeHTTPArtifactStore) put(key string, data []byte, contentType string, metadata map[string]string) {
	copied := make([]byte, len(data))
	copy(copied, data)
	metaCopy := map[string]string{}
	for k, v := range metadata {
		metaCopy[k] = v
	}
	s.objects[key] = fakeArtifactObject{data: copied, contentType: contentType, metadata: metaCopy}
}

func (s *fakeHTTPArtifactStore) PutObject(_ context.Context, key string, data []byte, options objectstore.PutOptions) error {
	contentType := options.ContentType
	if strings.TrimSpace(contentType) == "" {
		contentType = "application/octet-stream"
	}
	s.put(key, data, contentType, options.Metadata)
	return nil
}

func (s *fakeHTTPArtifactStore) Put(_ context.Context, key string, data []byte) error {
	s.put(key, data, "application/octet-stream", nil)
	return nil
}

func (s *fakeHTTPArtifactStore) Head(_ context.Context, key string) (objectstore.ObjectInfo, error) {
	obj, ok := s.objects[key]
	if !ok {
		return objectstore.ObjectInfo{}, os.ErrNotExist
	}
	return objectstore.ObjectInfo{Key: key, ContentType: obj.contentType, Metadata: obj.metadata, Size: int64(len(obj.data))}, nil
}

func (s *fakeHTTPArtifactStore) Get(_ context.Context, key string) ([]byte, error) {
	obj, ok := s.objects[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	copied := make([]byte, len(obj.data))
	copy(copied, obj.data)
	return copied, nil
}

func (s *fakeHTTPArtifactStore) GetWithContentType(_ context.Context, key string) ([]byte, string, error) {
	obj, ok := s.objects[key]
	if !ok {
		return nil, "", os.ErrNotExist
	}
	copied := make([]byte, len(obj.data))
	copy(copied, obj.data)
	return copied, obj.contentType, nil
}

func (s *fakeHTTPArtifactStore) Delete(_ context.Context, key string) error {
	delete(s.objects, key)
	return nil
}

func buildArtifactEnv(t *testing.T) artifactTestEnv {
	t.Helper()

	db := testutil.SetupPostgresDatabase(t, "artifacts")
	if _, err := migrate.Up(context.Background(), db.DSN); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	ctx := context.Background()
	pool, err := data.NewPool(ctx, db.DSN, data.PoolLimits{MaxConns: 32, MinConns: 0})
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	t.Cleanup(pool.Close)

	logger := observability.NewJSONLogger("test", io.Discard)
	passwordHasher, err := auth.NewBcryptPasswordHasher(0)
	if err != nil {
		t.Fatalf("new password hasher: %v", err)
	}
	tokenService, err := auth.NewJwtAccessTokenService(apiKeysTestJWTSecret, 3600, 2592000)
	if err != nil {
		t.Fatalf("new token service: %v", err)
	}

	userRepo, err := data.NewUserRepository(pool)
	if err != nil {
		t.Fatalf("new user repo: %v", err)
	}
	credRepo, err := data.NewUserCredentialRepository(pool)
	if err != nil {
		t.Fatalf("new cred repo: %v", err)
	}
	membershipRepo, err := data.NewAccountMembershipRepository(pool)
	if err != nil {
		t.Fatalf("new membership repo: %v", err)
	}
	refreshTokenRepo, err := data.NewRefreshTokenRepository(pool)
	if err != nil {
		t.Fatalf("new refresh token repo: %v", err)
	}
	auditRepo, err := data.NewAuditLogRepository(pool)
	if err != nil {
		t.Fatalf("new audit repo: %v", err)
	}
	threadRepo, err := data.NewThreadRepository(pool)
	if err != nil {
		t.Fatalf("new thread repo: %v", err)
	}
	projectRepo, err := data.NewProjectRepository(pool)
	if err != nil {
		t.Fatalf("new project repo: %v", err)
	}
	threadShareRepo, err := data.NewThreadShareRepository(pool)
	if err != nil {
		t.Fatalf("new thread share repo: %v", err)
	}
	runRepo, err := data.NewRunEventRepository(pool)
	if err != nil {
		t.Fatalf("new run repo: %v", err)
	}
	shellSessionRepo, err := data.NewShellSessionRepository(pool)
	if err != nil {
		t.Fatalf("new shell session repo: %v", err)
	}
	apiKeysRepo, err := data.NewAPIKeysRepository(pool)
	if err != nil {
		t.Fatalf("new api keys repo: %v", err)
	}
	jobRepo, err := data.NewJobRepository(pool)
	if err != nil {
		t.Fatalf("new job repo: %v", err)
	}
	authService, err := auth.NewService(userRepo, credRepo, membershipRepo, passwordHasher, tokenService, refreshTokenRepo, nil)
	if err != nil {
		t.Fatalf("new auth service: %v", err)
	}
	registrationService, err := auth.NewRegistrationService(pool, passwordHasher, tokenService, refreshTokenRepo, jobRepo)
	if err != nil {
		t.Fatalf("new registration service: %v", err)
	}

	store := newFakeHTTPArtifactStore()
	auditWriter := audit.NewWriter(auditRepo, membershipRepo, logger)
	handler := NewHandler(HandlerConfig{
		Pool:                   pool,
		Logger:                 logger,
		AuthService:            authService,
		RegistrationService:    registrationService,
		AccountMembershipRepo:      membershipRepo,
		ThreadRepo:             threadRepo,
		ProjectRepo:            projectRepo,
		ThreadShareRepo:        threadShareRepo,
		RunEventRepo:           runRepo,
		ShellSessionRepo:       shellSessionRepo,
		AuditWriter:            auditWriter,
		APIKeysRepo:            apiKeysRepo,
		ArtifactStore:          store,
		MessageAttachmentStore: store,
		EnvironmentStore:       store,
	})

	regResp := doJSON(handler, nethttp.MethodPost, "/v1/auth/register",
		map[string]any{"login": "alice-artifacts", "password": "pwd12345", "email": "alice-artifacts@test.com"},
		nil,
	)
	if regResp.Code != nethttp.StatusCreated {
		t.Fatalf("register: %d %s", regResp.Code, regResp.Body.String())
	}
	regPayload := decodeJSONBody[registerResponse](t, regResp.Body.Bytes())
	aliceUserID, err := uuid.Parse(regPayload.UserID)
	if err != nil {
		t.Fatalf("parse user id: %v", err)
	}
	membership, err := membershipRepo.GetDefaultForUser(ctx, aliceUserID)
	if err != nil {
		t.Fatalf("lookup membership: %v", err)
	}
	if membership == nil {
		t.Fatal("expected default membership")
	}

	return artifactTestEnv{
		handler:         handler,
		pool:            pool,
		apiKeysRepo:     apiKeysRepo,
		membershipRepo:  membershipRepo,
		threadRepo:      threadRepo,
		threadShareRepo: threadShareRepo,
		runRepo:         runRepo,
		tokenService:    tokenService,
		aliceToken:      regPayload.AccessToken,
		aliceUserID:     aliceUserID,
		aliceAccountID:      membership.AccountID,
		store:           store,
	}
}

func doArtifactRequest(t *testing.T, handler nethttp.Handler, path string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(nethttp.MethodGet, path, nil)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	return resp
}

func TestArtifactsAuthorizationByRunOwnerAndShare(t *testing.T) {
	env := buildArtifactEnv(t)
	project := mustCreateTestProject(t, context.Background(), env.pool, env.aliceAccountID, &env.aliceUserID, "artifact-thread-read")

	thread, err := env.threadRepo.Create(context.Background(), env.aliceAccountID, &env.aliceUserID, project.ID, nil, false)
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	run, _, err := env.runRepo.CreateRunWithStartedEvent(context.Background(), env.aliceAccountID, thread.ID, &env.aliceUserID, "run.started", nil)
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	artifactKey := env.aliceAccountID.String() + "/" + run.ID.String() + "/report.txt"
	env.store.put(artifactKey, []byte("owner-visible"), "text/plain", objectstore.ArtifactMetadata(objectstore.ArtifactOwnerKindRun, run.ID.String(), env.aliceAccountID.String(), nil))

	ownerResp := doArtifactRequest(t, env.handler, "/v1/artifacts/"+artifactKey, authHeader(env.aliceToken))
	if ownerResp.Code != nethttp.StatusOK || ownerResp.Body.String() != "owner-visible" {
		t.Fatalf("owner read: %d %q", ownerResp.Code, ownerResp.Body.String())
	}

	registerOther := doJSON(env.handler, nethttp.MethodPost, "/v1/auth/register",
		map[string]any{"login": "bob-artifacts", "password": "pwd12345", "email": "bob-artifacts@test.com"},
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
	if _, err := env.membershipRepo.Create(context.Background(), env.aliceAccountID, otherUserID, auth.RoleAccountMember); err != nil {
		t.Fatalf("add org membership: %v", err)
	}
	_, otherKey, err := env.apiKeysRepo.Create(context.Background(), env.aliceAccountID, otherUserID, "other-reader", []string{auth.PermDataRunsRead})
	if err != nil {
		t.Fatalf("create other key: %v", err)
	}
	forbiddenResp := doArtifactRequest(t, env.handler, "/v1/artifacts/"+artifactKey, authHeader(otherKey))
	assertErrorEnvelope(t, forbiddenResp, nethttp.StatusForbidden, "policy.denied")

	shareToken, err := data.GenerateShareToken()
	if err != nil {
		t.Fatalf("generate share token: %v", err)
	}
	password := "topsecret"
	share, err := env.threadShareRepo.Create(context.Background(), thread.ID, shareToken, "password", &password, 1, false, 1, env.aliceUserID)
	if err != nil {
		t.Fatalf("create password share: %v", err)
	}

	missingSession := doArtifactRequest(t, env.handler, "/v1/artifacts/"+artifactKey+"?share_token="+share.Token, nil)
	assertErrorEnvelope(t, missingSession, nethttp.StatusForbidden, "artifacts.forbidden")

	validSession := generateShareSession(share)
	sharedResp := doArtifactRequest(t, env.handler, "/v1/artifacts/"+artifactKey+"?share_token="+share.Token+"&session_token="+validSession, nil)
	if sharedResp.Code != nethttp.StatusOK || sharedResp.Body.String() != "owner-visible" {
		t.Fatalf("shared read: %d %q", sharedResp.Code, sharedResp.Body.String())
	}

	wrongShare := doArtifactRequest(t, env.handler, "/v1/artifacts/"+artifactKey+"?share_token=wrong-token", nil)
	assertErrorEnvelope(t, wrongShare, nethttp.StatusForbidden, "artifacts.forbidden")

	legacyKey := env.aliceAccountID.String() + "/" + run.ID.String() + "/legacy.txt"
	env.store.put(legacyKey, []byte("legacy-visible"), "text/plain", nil)
	legacyResp := doArtifactRequest(t, env.handler, "/v1/artifacts/"+legacyKey, authHeader(env.aliceToken))
	if legacyResp.Code != nethttp.StatusOK || legacyResp.Body.String() != "legacy-visible" {
		t.Fatalf("legacy read: %d %q", legacyResp.Code, legacyResp.Body.String())
	}
}

func TestThreadAttachmentsUploadAndRead(t *testing.T) {
	env := buildArtifactEnv(t)
	project := mustCreateTestProject(t, context.Background(), env.pool, env.aliceAccountID, &env.aliceUserID, "artifact-attachments")

	thread, err := env.threadRepo.Create(context.Background(), env.aliceAccountID, &env.aliceUserID, project.ID, nil, false)
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "note.txt")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write([]byte("hello attachment")); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	req := httptest.NewRequest(nethttp.MethodPost, "/v1/threads/"+thread.ID.String()+"/attachments", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+env.aliceToken)
	resp := httptest.NewRecorder()
	env.handler.ServeHTTP(resp, req)
	if resp.Code != nethttp.StatusCreated {
		t.Fatalf("upload attachment: %d %s", resp.Code, resp.Body.String())
	}
	payload := decodeJSONBody[messageAttachmentUploadResponse](t, resp.Body.Bytes())
	if payload.Key == "" || payload.Kind != "file" || payload.ExtractedText != "hello attachment" {
		t.Fatalf("unexpected upload payload: %#v", payload)
	}

	ownerResp := doArtifactRequest(t, env.handler, "/v1/attachments/"+payload.Key, authHeader(env.aliceToken))
	if ownerResp.Code != nethttp.StatusOK {
		t.Fatalf("owner attachment read: %d %s", ownerResp.Code, ownerResp.Body.String())
	}
	if body := ownerResp.Body.String(); body != "hello attachment" {
		t.Fatalf("unexpected owner attachment body: %q", body)
	}

	bobRegister := doJSON(
		env.handler,
		nethttp.MethodPost,
		"/v1/auth/register",
		map[string]any{"login": "bob-attachments", "password": "pwd12345", "email": "bob-attachments@test.com"},
		nil,
	)
	if bobRegister.Code != nethttp.StatusCreated {
		t.Fatalf("register bob: %d %s", bobRegister.Code, bobRegister.Body.String())
	}
	bob := decodeJSONBody[registerResponse](t, bobRegister.Body.Bytes())
	forbidden := doArtifactRequest(t, env.handler, "/v1/attachments/"+payload.Key, authHeader(bob.AccessToken))
	assertErrorEnvelope(t, forbidden, nethttp.StatusForbidden, "policy.denied")
}

func TestArtifactsAuthorizationByBrowserSessionOwnerFallback(t *testing.T) {
	env := buildArtifactEnv(t)
	project := mustCreateTestProject(t, context.Background(), env.pool, env.aliceAccountID, &env.aliceUserID, "artifact-browser-session")

	thread, err := env.threadRepo.Create(context.Background(), env.aliceAccountID, &env.aliceUserID, project.ID, nil, false)
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	run, _, err := env.runRepo.CreateRunWithStartedEvent(context.Background(), env.aliceAccountID, thread.ID, &env.aliceUserID, "run.started", nil)
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	sessionRef := "brref_browserartifact"
	if _, err := env.pool.Exec(context.Background(), `
		INSERT INTO shell_sessions (
			session_ref, session_type, account_id, profile_ref, workspace_ref,
			thread_id, run_id, share_scope, state, metadata_json
		) VALUES ($1, 'browser', $2, $3, $4, $5, $6, $7, $8, '{}'::jsonb)
	`, sessionRef, env.aliceAccountID, "pref_test", "wsref_test", thread.ID, run.ID, "thread", "idle"); err != nil {
		t.Fatalf("insert shell session: %v", err)
	}

	artifactKey := env.aliceAccountID.String() + "/" + sessionRef + "/1/browser-screenshot.png"
	env.store.put(
		artifactKey,
		[]byte("png-bytes"),
		"image/png",
		objectstore.ArtifactMetadata(objectstore.ArtifactOwnerKindRun, sessionRef, env.aliceAccountID.String(), nil),
	)

	resp := doArtifactRequest(t, env.handler, "/v1/artifacts/"+artifactKey, authHeader(env.aliceToken))
	if resp.Code != nethttp.StatusOK {
		t.Fatalf("browser artifact read: %d %s", resp.Code, resp.Body.String())
	}
	if got := resp.Body.String(); got != "png-bytes" {
		t.Fatalf("unexpected browser artifact body: %q", got)
	}
}
