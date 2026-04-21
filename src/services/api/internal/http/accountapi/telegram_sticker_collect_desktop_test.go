//go:build desktop

package accountapi

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"arkloop/services/api/internal/data"
	"arkloop/services/shared/database/sqliteadapter"
	"arkloop/services/shared/database/sqlitepgx"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestUpsertTelegramStickerPendingTx_NewStickerTriggersRegister(t *testing.T) {
	ctx := context.Background()
	pool, accountID := openStickerCollectDesktopDB(t, ctx)

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	sticker, shouldRegister, err := upsertTelegramStickerPendingTx(ctx, tx, accountID, telegramCollectedSticker{
		ContentHash:       "hash-new",
		StorageKey:        "raw.webp",
		PreviewStorageKey: "preview.jpg",
		FileSize:          128,
		MimeType:          "image/webp",
	})
	if err != nil {
		t.Fatalf("upsert pending: %v", err)
	}
	if !shouldRegister {
		t.Fatal("expected new sticker to trigger register run")
	}
	if sticker == nil || sticker.ContentHash != "hash-new" {
		t.Fatalf("unexpected sticker row: %#v", sticker)
	}
}

func TestUpsertTelegramStickerPendingTx_PendingWithinWindowDoesNotRetrigger(t *testing.T) {
	ctx := context.Background()
	pool, accountID := openStickerCollectDesktopDB(t, ctx)
	seedStickerRow(t, ctx, pool, accountID, "hash-window", time.Now().UTC().Add(-30*time.Minute))

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	sticker, shouldRegister, err := upsertTelegramStickerPendingTx(ctx, tx, accountID, telegramCollectedSticker{
		ContentHash:       "hash-window",
		StorageKey:        "raw-new.webp",
		PreviewStorageKey: "preview-new.jpg",
		FileSize:          256,
		MimeType:          "image/webp",
	})
	if err != nil {
		t.Fatalf("upsert pending: %v", err)
	}
	if shouldRegister {
		t.Fatal("expected pending sticker inside retry window to skip register run")
	}
	if sticker == nil || sticker.StorageKey != "raw-new.webp" || sticker.PreviewStorageKey != "preview-new.jpg" {
		t.Fatalf("expected metadata refresh on pending sticker, got %#v", sticker)
	}

	var updatedAt time.Time
	if err := tx.QueryRow(ctx, `
		SELECT updated_at
		  FROM account_stickers
		 WHERE account_id = $1
		   AND content_hash = $2`,
		accountID, "hash-window",
	).Scan(&updatedAt); err != nil {
		t.Fatalf("query updated_at: %v", err)
	}
	if updatedAt.After(time.Now().UTC().Add(-20 * time.Minute)) {
		t.Fatalf("expected updated_at to stay inside original retry window, got %s", updatedAt)
	}
}

func TestUpsertTelegramStickerPendingTx_StalePendingClaimsRetry(t *testing.T) {
	ctx := context.Background()
	pool, accountID := openStickerCollectDesktopDB(t, ctx)
	seedStickerRow(t, ctx, pool, accountID, "hash-stale", time.Now().UTC().Add(-2*time.Hour))

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	_, shouldRegister, err := upsertTelegramStickerPendingTx(ctx, tx, accountID, telegramCollectedSticker{
		ContentHash:       "hash-stale",
		StorageKey:        "raw-retry.webp",
		PreviewStorageKey: "preview-retry.jpg",
		FileSize:          512,
		MimeType:          "image/webp",
	})
	if err != nil {
		t.Fatalf("upsert pending: %v", err)
	}
	if !shouldRegister {
		t.Fatal("expected stale pending sticker to claim a retry")
	}

	var updatedAt time.Time
	if err := tx.QueryRow(ctx, `
		SELECT updated_at
		  FROM account_stickers
		 WHERE account_id = $1
		   AND content_hash = $2`,
		accountID, "hash-stale",
	).Scan(&updatedAt); err != nil {
		t.Fatalf("query updated_at: %v", err)
	}
	if updatedAt.Before(time.Now().UTC().Add(-5 * time.Minute)) {
		t.Fatalf("expected retry claim to refresh updated_at, got %s", updatedAt)
	}
}

func TestUpsertTelegramStickerPendingTx_GainingPreviewTriggersRegister(t *testing.T) {
	ctx := context.Background()
	pool, accountID := openStickerCollectDesktopDB(t, ctx)
	seedStickerRowWithoutPreview(t, ctx, pool, accountID, "hash-preview-late", time.Now().UTC().Add(-30*time.Minute))

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	sticker, shouldRegister, err := upsertTelegramStickerPendingTx(ctx, tx, accountID, telegramCollectedSticker{
		ContentHash:       "hash-preview-late",
		StorageKey:        "raw-late.webp",
		PreviewStorageKey: "preview-late.jpg",
		FileSize:          333,
		MimeType:          "image/webp",
	})
	if err != nil {
		t.Fatalf("upsert pending: %v", err)
	}
	if !shouldRegister {
		t.Fatal("expected preview arrival to trigger register run immediately")
	}
	if sticker == nil || sticker.PreviewStorageKey != "preview-late.jpg" {
		t.Fatalf("expected preview key updated, got %#v", sticker)
	}
}

func TestBuildTelegramStickerRegisterStartedData_UsesResolvedRouteID(t *testing.T) {
	ctx := context.Background()
	pool, accountID := openStickerCollectDesktopDB(t, ctx)
	ownerUserID := createStickerCollectUser(t, ctx, pool, "default-owner")
	routeID := seedStickerSelectorRoute(t, ctx, pool, accountID, ownerUserID, "demo-cred", "gpt-5-mini")

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	connector := telegramConnector{}
	startedData, err := connector.buildTelegramStickerRegisterStartedData(ctx, tx, data.Channel{
		AccountID:   accountID,
		ChannelType: "telegram",
		ConfigJSON:  json.RawMessage(`{"default_model":"demo-cred^gpt-5-mini"}`),
		OwnerUserID: &ownerUserID,
	}, nil, "hash-route-default")
	if err != nil {
		t.Fatalf("build started data: %v", err)
	}

	if got := startedData["model"]; got != "demo-cred^gpt-5-mini" {
		t.Fatalf("unexpected model selector: %#v", got)
	}
	if got := startedData["route_id"]; got != routeID.String() {
		t.Fatalf("unexpected route_id: %#v", got)
	}
}

func TestBuildTelegramStickerRegisterStartedData_PrefersIdentityModelRoute(t *testing.T) {
	ctx := context.Background()
	pool, accountID := openStickerCollectDesktopDB(t, ctx)
	ownerUserID := createStickerCollectUser(t, ctx, pool, "preferred-owner")
	_ = seedStickerSelectorRoute(t, ctx, pool, accountID, ownerUserID, "demo-cred", "gpt-5-mini")
	preferredRouteID := seedStickerSelectorRoute(t, ctx, pool, accountID, ownerUserID, "pref-cred", "claude-3-5-haiku")
	identityID := seedStickerChannelIdentity(t, ctx, pool, "pref-identity", "pref-cred^claude-3-5-haiku")

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	channelIdentitiesRepo, err := data.NewChannelIdentitiesRepository(pool)
	if err != nil {
		t.Fatalf("new channel identities repo: %v", err)
	}
	connector := telegramConnector{channelIdentitiesRepo: channelIdentitiesRepo}
	startedData, err := connector.buildTelegramStickerRegisterStartedData(ctx, tx, data.Channel{
		AccountID:   accountID,
		ChannelType: "telegram",
		ConfigJSON:  json.RawMessage(`{"default_model":"demo-cred^gpt-5-mini"}`),
		OwnerUserID: &ownerUserID,
	}, &identityID, "hash-route-preferred")
	if err != nil {
		t.Fatalf("build started data: %v", err)
	}

	if got := startedData["model"]; got != "pref-cred^claude-3-5-haiku" {
		t.Fatalf("unexpected preferred model selector: %#v", got)
	}
	if got := startedData["route_id"]; got != preferredRouteID.String() {
		t.Fatalf("unexpected preferred route_id: %#v", got)
	}
}

func openStickerCollectDesktopDB(t *testing.T, ctx context.Context) (*sqlitepgx.Pool, uuid.UUID) {
	t.Helper()

	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	pool := sqlitepgx.New(sqlitePool.Unwrap())

	accountRepo, err := data.NewAccountRepository(pool)
	if err != nil {
		t.Fatalf("new account repo: %v", err)
	}
	account, err := accountRepo.Create(ctx, "stickers-"+uuid.NewString(), "stickers", "personal")
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	return pool, account.ID
}

func createStickerCollectUser(t *testing.T, ctx context.Context, pool *sqlitepgx.Pool, name string) uuid.UUID {
	t.Helper()

	userRepo, err := data.NewUserRepository(pool)
	if err != nil {
		t.Fatalf("new user repo: %v", err)
	}
	user, err := userRepo.Create(ctx, "sticker-"+name, name+"@test.com", "zh")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return user.ID
}

func seedStickerSelectorRoute(t *testing.T, ctx context.Context, pool *sqlitepgx.Pool, accountID, ownerUserID uuid.UUID, credentialName, model string) uuid.UUID {
	t.Helper()

	routesRepo, err := data.NewLlmRoutesRepository(pool)
	if err != nil {
		t.Fatalf("new llm routes repo: %v", err)
	}
	credentialID := uuid.New()
	if err := pool.QueryRow(ctx, `
		INSERT INTO llm_credentials (
			id, account_id, owner_kind, owner_user_id, provider, name, advanced_json
		) VALUES (
			$1, $2, 'user', $3, 'openai', $4, '{}'
		)
		RETURNING id`,
		credentialID,
		accountID,
		ownerUserID,
		credentialName,
	).Scan(&credentialID); err != nil {
		t.Fatalf("create llm credential: %v", err)
	}
	route, err := routesRepo.Create(ctx, data.CreateLlmRouteParams{
		AccountID:    accountID,
		Scope:        data.LlmRouteScopeUser,
		CredentialID: credentialID,
		Model:        model,
		Priority:     100,
		ShowInPicker: true,
		WhenJSON:     json.RawMessage(`{}`),
		AdvancedJSON: map[string]any{},
		Multiplier:   1.0,
	})
	if err != nil {
		t.Fatalf("create llm route: %v", err)
	}
	return route.ID
}

func seedStickerChannelIdentity(t *testing.T, ctx context.Context, pool *sqlitepgx.Pool, subjectID, preferredModel string) uuid.UUID {
	t.Helper()

	repo, err := data.NewChannelIdentitiesRepository(pool)
	if err != nil {
		t.Fatalf("new channel identities repo: %v", err)
	}
	identity, err := repo.Upsert(ctx, "telegram", subjectID, nil, nil, nil)
	if err != nil {
		t.Fatalf("upsert channel identity: %v", err)
	}
	if err := repo.UpdatePreferenceConfig(ctx, identity.ID, preferredModel, ""); err != nil {
		t.Fatalf("update channel identity preference: %v", err)
	}
	return identity.ID
}

func seedStickerRow(t *testing.T, ctx context.Context, pool *sqlitepgx.Pool, accountID uuid.UUID, contentHash string, updatedAt time.Time) {
	t.Helper()

	createdAt := updatedAt.Add(-time.Minute)
	if _, err := pool.Exec(ctx, `
		INSERT INTO account_stickers (
			id, account_id, content_hash, storage_key, preview_storage_key, file_size, mime_type,
			is_animated, short_tags, long_desc, usage_count, is_registered, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, FALSE, '', '', 0, FALSE, $8, $9
		)`,
		uuid.New(),
		accountID,
		contentHash,
		"raw.webp",
		"preview.jpg",
		int64(64),
		"image/webp",
		createdAt.UTC(),
		updatedAt.UTC(),
	); err != nil {
		t.Fatalf("seed sticker row: %v", err)
	}
}

func seedStickerRowWithoutPreview(t *testing.T, ctx context.Context, pool *sqlitepgx.Pool, accountID uuid.UUID, contentHash string, updatedAt time.Time) {
	t.Helper()

	createdAt := updatedAt.Add(-time.Minute)
	if _, err := pool.Exec(ctx, `
		INSERT INTO account_stickers (
			id, account_id, content_hash, storage_key, preview_storage_key, file_size, mime_type,
			is_animated, short_tags, long_desc, usage_count, is_registered, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, '', $5, $6, FALSE, '', '', 0, FALSE, $7, $8
		)`,
		uuid.New(),
		accountID,
		contentHash,
		"raw.webp",
		int64(64),
		"image/webp",
		createdAt.UTC(),
		updatedAt.UTC(),
	); err != nil {
		t.Fatalf("seed sticker row without preview: %v", err)
	}
}
