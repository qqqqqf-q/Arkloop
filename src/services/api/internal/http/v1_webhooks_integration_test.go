//go:build !desktop

package http

import (
	"context"
	"encoding/hex"
	"io"
	nethttp "net/http"
	"testing"

	"arkloop/services/api/internal/auth"
	apicrypto "arkloop/services/api/internal/crypto"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/migrate"
	"arkloop/services/api/internal/observability"
	"arkloop/services/api/internal/testutil"
)

func TestWebhookCreateStoresSecretReference(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "webhooks")
	if _, err := migrate.Up(context.Background(), db.DSN); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	ctx := context.Background()
	appDB, _, err := data.NewPool(ctx, db.DSN, data.PoolLimits{MaxConns: 32, MinConns: 0})
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	defer appDB.Close()

	logger := observability.NewJSONLogger("test", io.Discard)
	passwordHasher, err := auth.NewBcryptPasswordHasher(0)
	if err != nil {
		t.Fatalf("new password hasher: %v", err)
	}
	tokenService, err := auth.NewJwtAccessTokenService(apiKeysTestJWTSecret, 3600, 2592000)
	if err != nil {
		t.Fatalf("new token service: %v", err)
	}
	keyBytes, _ := hex.DecodeString("00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff")
	keyRing, err := apicrypto.NewKeyRing(map[int][]byte{1: keyBytes})
	if err != nil {
		t.Fatalf("new key ring: %v", err)
	}

	userRepo, _ := data.NewUserRepository(appDB)
	credRepo, _ := data.NewUserCredentialRepository(appDB)
	membershipRepo, _ := data.NewOrgMembershipRepository(appDB)
	refreshTokenRepo, _ := data.NewRefreshTokenRepository(appDB)
	jobRepo, _ := data.NewJobRepository(appDB)
	webhookRepo, _ := data.NewWebhookEndpointRepository(appDB)
	secretsRepo, _ := data.NewSecretsRepository(appDB, keyRing)
	authService, _ := auth.NewService(userRepo, credRepo, membershipRepo, passwordHasher, tokenService, refreshTokenRepo, nil)
	registrationService, _ := auth.NewRegistrationService(appDB, passwordHasher, tokenService, refreshTokenRepo, jobRepo)

	handler := NewHandler(HandlerConfig{
		DB:                appDB,
		Logger:              logger,
		AuthService:         authService,
		RegistrationService: registrationService,
		OrgMembershipRepo:   membershipRepo,
		WebhookRepo:         webhookRepo,
		APIKeysRepo:         nil,
		SecretsRepo:         secretsRepo,
	})

	regResp := doJSON(handler, nethttp.MethodPost, "/v1/auth/register",
		map[string]any{"login": "alice-webhook", "password": "pwd12345", "email": "alice-webhook@test.com"},
		nil,
	)
	if regResp.Code != nethttp.StatusCreated {
		t.Fatalf("register: %d %s", regResp.Code, regResp.Body.String())
	}
	regPayload := decodeJSONBody[registerResponse](t, regResp.Body.Bytes())

	createResp := doJSON(handler, nethttp.MethodPost, "/v1/webhook-endpoints",
		map[string]any{"url": "https://example.com/webhook", "events": []string{"run.completed"}},
		authHeader(regPayload.AccessToken),
	)
	if createResp.Code != nethttp.StatusCreated {
		t.Fatalf("create webhook: %d %s", createResp.Code, createResp.Body.String())
	}
	created := decodeJSONBody[webhookEndpointResponse](t, createResp.Body.Bytes())

	var secretID *string
	var signingSecret *string
	if err := appDB.QueryRow(ctx,
		"SELECT secret_id::text, signing_secret FROM webhook_endpoints WHERE id = $1",
		created.ID,
	).Scan(&secretID, &signingSecret); err != nil {
		t.Fatalf("query webhook row: %v", err)
	}
	if secretID == nil || *secretID == "" {
		t.Fatal("expected secret_id to be set")
	}
	if signingSecret != nil {
		t.Fatalf("expected signing_secret to be null, got %q", *signingSecret)
	}
}
