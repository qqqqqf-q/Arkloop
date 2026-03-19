package data

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type ChannelGroupThread struct {
	ID             uuid.UUID
	ChannelID      uuid.UUID
	PlatformChatID string
	PersonaID      uuid.UUID
	ThreadID       uuid.UUID
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type ChannelGroupThreadsRepository struct {
	db Querier
}

func NewChannelGroupThreadsRepository(db Querier) (*ChannelGroupThreadsRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &ChannelGroupThreadsRepository{db: db}, nil
}

func (r *ChannelGroupThreadsRepository) WithTx(tx pgx.Tx) *ChannelGroupThreadsRepository {
	return &ChannelGroupThreadsRepository{db: tx}
}

const channelGroupThreadColumns = `id, channel_id, platform_chat_id, persona_id, thread_id, created_at, updated_at`

func scanChannelGroupThread(row interface{ Scan(dest ...any) error }) (ChannelGroupThread, error) {
	var item ChannelGroupThread
	err := row.Scan(
		&item.ID,
		&item.ChannelID,
		&item.PlatformChatID,
		&item.PersonaID,
		&item.ThreadID,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	return item, err
}

func (r *ChannelGroupThreadsRepository) GetByBinding(
	ctx context.Context,
	channelID uuid.UUID,
	platformChatID string,
	personaID uuid.UUID,
) (*ChannelGroupThread, error) {
	item, err := scanChannelGroupThread(r.db.QueryRow(
		ctx,
		`SELECT `+channelGroupThreadColumns+`
		 FROM channel_group_threads
		 WHERE channel_id = $1 AND platform_chat_id = $2 AND persona_id = $3`,
		channelID,
		platformChatID,
		personaID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("channel_group_threads.GetByBinding: %w", err)
	}
	return &item, nil
}

func (r *ChannelGroupThreadsRepository) Create(
	ctx context.Context,
	channelID uuid.UUID,
	platformChatID string,
	personaID uuid.UUID,
	threadID uuid.UUID,
) (ChannelGroupThread, error) {
	if channelID == uuid.Nil || personaID == uuid.Nil || threadID == uuid.Nil {
		return ChannelGroupThread{}, fmt.Errorf("channel_group_threads: ids must not be empty")
	}
	if platformChatID == "" {
		return ChannelGroupThread{}, fmt.Errorf("channel_group_threads: platform_chat_id must not be empty")
	}
	item, err := scanChannelGroupThread(r.db.QueryRow(
		ctx,
		`INSERT INTO channel_group_threads (channel_id, platform_chat_id, persona_id, thread_id)
		 VALUES ($1, $2, $3, $4)
		 RETURNING `+channelGroupThreadColumns,
		channelID,
		platformChatID,
		personaID,
		threadID,
	))
	if err != nil {
		return ChannelGroupThread{}, fmt.Errorf("channel_group_threads.Create: %w", err)
	}
	return item, nil
}
