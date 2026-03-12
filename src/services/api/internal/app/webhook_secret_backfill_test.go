//go:build !desktop

package app

import (
	"context"
	"encoding/hex"
	"io"
	"testing"

	apicrypto "arkloop/services/api/internal/crypto"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/migrate"
	"arkloop/services/api/internal/observability"
	"arkloop/services/api/internal/testutil"

	"github.com/google/uuid"
)

func TestBackfillWebhookSecrets(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "webhook_backfill")
	if _, err := migrate.Up(context.Background(), db.DSN); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	ctx := context.Background()
	appDB, _, err := data.NewPool(ctx, db.DSN, data.PoolLimits{MaxConns: 32, MinConns: 0})
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	defer appDB.Close()

	keyBytes, _ := hex.DecodeString("00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff")
	keyRing, err := apicrypto.NewKeyRing(map[int][]byte{1: keyBytes})
	if err != nil {
		t.Fatalf("new key ring: %v", err)
	}
	webhookRepo, err := data.NewWebhookEndpointRepository(appDB)
	if err != nil {
		t.Fatalf("new webhook repo: %v", err)
	}
	secretsRepo, err := data.NewSecretsRepository(appDB, keyRing)
	if err != nil {
		t.Fatalf("new secrets repo: %v", err)
	}

	orgID := uuid.New()
	endpointID := uuid.New()
	legacySecret := "legacy-secret"
	if _, err := appDB.Exec(ctx, "INSERT INTO orgs (id, slug, name, type) VALUES ($1, $2, $3, 'personal')", orgID, "org-backfill", "org"); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	if _, err := appDB.Exec(ctx,
		`INSERT INTO webhook_endpoints (id, org_id, url, signing_secret, events)
		 VALUES ($1, $2, $3, $4, $5)`,
		endpointID, orgID, "https://example.com/hook", legacySecret, []string{"run.completed"},
	); err != nil {
		t.Fatalf("insert legacy webhook: %v", err)
	}

	logger := observability.NewJSONLogger("test", io.Discard)
	if err := backfillWebhookSecrets(ctx, appDB, webhookRepo, secretsRepo, logger); err != nil {
		t.Fatalf("backfill webhook secrets: %v", err)
	}

	var secretID *uuid.UUID
	var signingSecret *string
	if err := appDB.QueryRow(ctx,
		"SELECT secret_id, signing_secret FROM webhook_endpoints WHERE id = $1",
		endpointID,
	).Scan(&secretID, &signingSecret); err != nil {
		t.Fatalf("query webhook endpoint: %v", err)
	}
	if secretID == nil || *secretID == uuid.Nil {
		t.Fatal("expected secret_id to be backfilled")
	}
	if signingSecret != nil {
		t.Fatalf("expected signing_secret to be cleared, got %q", *signingSecret)
	}

	decrypted, err := secretsRepo.DecryptByID(ctx, *secretID)
	if err != nil {
		t.Fatalf("decrypt secret: %v", err)
	}
	if decrypted == nil || *decrypted != legacySecret {
		t.Fatalf("unexpected decrypted secret: %#v", decrypted)
	}
}
