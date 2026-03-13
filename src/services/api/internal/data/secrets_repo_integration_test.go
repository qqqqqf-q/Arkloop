//go:build !desktop

package data

import (
	"context"
	"errors"
	"testing"

	"arkloop/services/api/internal/crypto"
	"arkloop/services/api/internal/migrate"
	"arkloop/services/api/internal/testutil"
)

func setupSecretsTestRepo(t *testing.T) (*SecretsRepository, *AccountRepository, context.Context) {
	t.Helper()

	db := testutil.SetupPostgresDatabase(t, "api_go_secrets")
	ctx := context.Background()

	if _, err := migrate.Up(ctx, db.DSN); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	pool, err := NewPool(ctx, db.DSN, PoolLimits{MaxConns: 32, MinConns: 0})
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	t.Cleanup(pool.Close)

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 7)
	}
	ring, err := crypto.NewKeyRing(map[int][]byte{1: key})
	if err != nil {
		t.Fatalf("new key ring: %v", err)
	}

	repo, err := NewSecretsRepository(pool, ring)
	if err != nil {
		t.Fatalf("new secrets repo: %v", err)
	}

	orgRepo, err := NewAccountRepository(pool)
	if err != nil {
		t.Fatalf("new org repo: %v", err)
	}

	return repo, orgRepo, ctx
}

func TestSecretsCreate(t *testing.T) {
	repo, orgRepo, ctx := setupSecretsTestRepo(t)

	org, err := orgRepo.Create(ctx, "test-org-create", "Test Org Create", "personal")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}

	s, err := repo.Create(ctx, org.ID, "api-key", "sk-supersecret")
	if err != nil {
		t.Fatalf("create secret: %v", err)
	}

	if s.ID.String() == "" {
		t.Fatal("secret id must not be empty")
	}
	if s.Name != "api-key" {
		t.Fatalf("unexpected name: %q", s.Name)
	}
	// 密文不能等于明文
	if s.EncryptedValue == "sk-supersecret" {
		t.Fatal("encrypted_value must not equal plaintext")
	}
	if s.KeyVersion != 1 {
		t.Fatalf("expected key_version=1, got %d", s.KeyVersion)
	}
}

func TestSecretsDecrypt(t *testing.T) {
	repo, orgRepo, ctx := setupSecretsTestRepo(t)

	org, err := orgRepo.Create(ctx, "test-org-decrypt", "Test Org Decrypt", "personal")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}

	_, err = repo.Create(ctx, org.ID, "token", "my-plaintext-value")
	if err != nil {
		t.Fatalf("create secret: %v", err)
	}

	plain, err := repo.DecryptByName(ctx, org.ID, "token")
	if err != nil {
		t.Fatalf("decrypt by name: %v", err)
	}
	if plain == nil {
		t.Fatal("expected plaintext, got nil")
	}
	if *plain != "my-plaintext-value" {
		t.Fatalf("unexpected plaintext: %q", *plain)
	}
}

func TestSecretsDecryptNotFound(t *testing.T) {
	repo, orgRepo, ctx := setupSecretsTestRepo(t)

	org, err := orgRepo.Create(ctx, "test-org-notfound", "Test Org NotFound", "personal")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}

	plain, err := repo.DecryptByName(ctx, org.ID, "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plain != nil {
		t.Fatalf("expected nil for missing secret, got %q", *plain)
	}
}

func TestSecretsUniqueConstraint(t *testing.T) {
	repo, orgRepo, ctx := setupSecretsTestRepo(t)

	org, err := orgRepo.Create(ctx, "test-org-unique", "Test Org Unique", "personal")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}

	if _, err := repo.Create(ctx, org.ID, "dup-key", "value1"); err != nil {
		t.Fatalf("first create: %v", err)
	}

	_, err = repo.Create(ctx, org.ID, "dup-key", "value2")
	if err == nil {
		t.Fatal("expected conflict error, got nil")
	}

	var conflictErr SecretNameConflictError
	if !errors.As(err, &conflictErr) {
		t.Fatalf("expected SecretNameConflictError, got: %T %v", err, err)
	}
	if conflictErr.Name != "dup-key" {
		t.Fatalf("unexpected conflict name: %q", conflictErr.Name)
	}
}

func TestSecretsUpsert(t *testing.T) {
	repo, orgRepo, ctx := setupSecretsTestRepo(t)

	org, err := orgRepo.Create(ctx, "test-org-upsert", "Test Org Upsert", "personal")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}

	_, err = repo.Upsert(ctx, org.ID, "rotating-key", "first-value")
	if err != nil {
		t.Fatalf("upsert 1: %v", err)
	}

	_, err = repo.Upsert(ctx, org.ID, "rotating-key", "second-value")
	if err != nil {
		t.Fatalf("upsert 2: %v", err)
	}

	plain, err := repo.DecryptByName(ctx, org.ID, "rotating-key")
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if plain == nil || *plain != "second-value" {
		t.Fatalf("expected second-value, got %v", plain)
	}

	// 同一 org+name 只应有一条记录
	list, err := repo.List(ctx, org.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 secret, got %d", len(list))
	}
}

func TestSecretsDelete(t *testing.T) {
	repo, orgRepo, ctx := setupSecretsTestRepo(t)

	org, err := orgRepo.Create(ctx, "test-org-delete", "Test Org Delete", "personal")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}

	_, err = repo.Create(ctx, org.ID, "to-delete", "value")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := repo.Delete(ctx, org.ID, "to-delete"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	s, err := repo.GetByName(ctx, org.ID, "to-delete")
	if err != nil {
		t.Fatalf("get after delete: %v", err)
	}
	if s != nil {
		t.Fatal("expected nil after delete, got a secret")
	}
}

func TestSecretsDeleteNotFound(t *testing.T) {
	repo, orgRepo, ctx := setupSecretsTestRepo(t)

	org, err := orgRepo.Create(ctx, "test-org-del-notfound", "Test Org Del NotFound", "personal")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}

	err = repo.Delete(ctx, org.ID, "nonexistent")
	if err == nil {
		t.Fatal("expected SecretNotFoundError, got nil")
	}
	var notFoundErr SecretNotFoundError
	if !errors.As(err, &notFoundErr) {
		t.Fatalf("expected SecretNotFoundError, got: %T %v", err, err)
	}
}

func TestSecretsList(t *testing.T) {
	repo, orgRepo, ctx := setupSecretsTestRepo(t)

	org, err := orgRepo.Create(ctx, "test-org-list", "Test Org List", "personal")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}

	names := []string{"beta-key", "alpha-key", "gamma-key"}
	for _, name := range names {
		if _, err := repo.Create(ctx, org.ID, name, "value-"+name); err != nil {
			t.Fatalf("create %q: %v", name, err)
		}
	}

	list, err := repo.List(ctx, org.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 secrets, got %d", len(list))
	}

	// 验证按 name 升序排列
	if list[0].Name != "alpha-key" || list[1].Name != "beta-key" || list[2].Name != "gamma-key" {
		t.Fatalf("unexpected order: %v", []string{list[0].Name, list[1].Name, list[2].Name})
	}

	// SecretMeta 不含 EncryptedValue，无法直接比较密文
}
