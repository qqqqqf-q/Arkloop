package pipeline

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"arkloop/services/shared/telegrambot"
	"arkloop/services/worker/internal/data"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func SendTelegramStickerByID(
	ctx context.Context,
	db data.QueryDB,
	store interface {
		Get(ctx context.Context, key string) ([]byte, error)
	},
	client *telegrambot.Client,
	token string,
	target ChannelDeliveryTarget,
	accountID uuid.UUID,
	stickerID string,
) ([]string, error) {
	if db == nil || store == nil || client == nil {
		return nil, fmt.Errorf("sticker delivery unavailable")
	}
	sticker, err := data.AccountStickersRepository{}.GetByHash(ctx, db, accountID, strings.TrimSpace(stickerID))
	if err != nil {
		return nil, err
	}
	if sticker == nil {
		return nil, fmt.Errorf("sticker not found: %s", strings.TrimSpace(stickerID))
	}
	blob, err := store.Get(ctx, sticker.StorageKey)
	if err != nil {
		return nil, err
	}
	replyTo := ""
	if target.ReplyTo != nil {
		replyTo = strings.TrimSpace(target.ReplyTo.MessageID)
	}
	threadID := ""
	if target.Conversation.ThreadID != nil {
		threadID = strings.TrimSpace(*target.Conversation.ThreadID)
	}
	sent, err := client.SendStickerBytes(
		ctx,
		token,
		target.Conversation.Target,
		blob,
		filepath.Base(sticker.StorageKey),
		threadID,
		replyTo,
	)
	if err != nil {
		return nil, err
	}
	if sent == nil {
		return nil, nil
	}
	return []string{strconv.FormatInt(sent.MessageID, 10)}, nil
}

type outboxProgressBeginner interface {
	BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
}

func AdvanceOutboxProgress(
	ctx context.Context,
	db outboxProgressBeginner,
	outboxRepo data.ChannelDeliveryOutboxRepository,
	outboxID uuid.UUID,
	segmentsSent int,
	accountID uuid.UUID,
	stickerID string,
) error {
	if db == nil {
		return fmt.Errorf("progress db unavailable")
	}
	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := outboxRepo.UpdateProgress(ctx, tx, outboxID, segmentsSent); err != nil {
		return err
	}
	if strings.TrimSpace(stickerID) != "" {
		if err := incrementStickerUsageTx(ctx, tx, accountID, stickerID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func incrementStickerUsageTx(ctx context.Context, tx pgx.Tx, accountID uuid.UUID, stickerID string) error {
	_, err := tx.Exec(ctx, `
		UPDATE account_stickers
		   SET usage_count = usage_count + 1,
		       last_used_at = $3,
		       updated_at = $3
		 WHERE account_id = $1
		   AND content_hash = $2`,
		accountID,
		strings.TrimSpace(stickerID),
		time.Now().UTC(),
	)
	return err
}
