package webhook

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"testing"

	"arkloop/services/worker/internal/testutil"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestGetWebhookEndpointDecryptsSecretReference(t *testing.T) {
	t.Setenv("ARKLOOP_ENCRYPTION_KEY", "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff")

	db := testutil.SetupPostgresDatabase(t, "arkloop_webhook_db")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	defer pool.Close()

	if _, err := pool.Exec(context.Background(), `
		CREATE TABLE webhook_endpoints (
			id UUID PRIMARY KEY,
			org_id UUID NOT NULL,
			url TEXT NOT NULL,
			secret_id UUID NULL,
			events TEXT[] NOT NULL DEFAULT '{}',
			enabled BOOLEAN NOT NULL DEFAULT true,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`); err != nil {
		t.Fatalf("create webhook_endpoints: %v", err)
	}

	secretPlain := "hook-secret"
	encrypted, err := encryptTestSecret(secretPlain)
	if err != nil {
		t.Fatalf("encrypt secret: %v", err)
	}

	orgID := uuid.New()
	secretID := uuid.New()
	endpointID := uuid.New()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO secrets (id, org_id, scope, encrypted_value, key_version)
		 VALUES ($1, $2, 'org', $3, 1)`,
		secretID, orgID, encrypted,
	); err != nil {
		t.Fatalf("insert secret: %v", err)
	}
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO webhook_endpoints (id, org_id, url, secret_id, events, enabled)
		 VALUES ($1, $2, $3, $4, $5, TRUE)`,
		endpointID, orgID, "https://example.com/hook", secretID, []string{"run.completed"},
	); err != nil {
		t.Fatalf("insert endpoint: %v", err)
	}

	ep, disabled, err := getWebhookEndpoint(context.Background(), pool, endpointID)
	if err != nil {
		t.Fatalf("get webhook endpoint: %v", err)
	}
	if disabled {
		t.Fatal("expected endpoint to be enabled")
	}
	if ep == nil || ep.SigningSecret != secretPlain {
		t.Fatalf("unexpected endpoint secret: %#v", ep)
	}

	payload := []byte(`{"event":"run.completed"}`)
	legacySig := computeHMAC(1000, payload, secretPlain)
	migratedSig := computeHMAC(1000, payload, ep.SigningSecret)
	if legacySig != migratedSig {
		t.Fatalf("signature mismatch: %s vs %s", legacySig, migratedSig)
	}
}

func encryptTestSecret(plaintext string) (string, error) {
	keyBytes, err := hex.DecodeString("00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff")
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(keyBytes)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}
