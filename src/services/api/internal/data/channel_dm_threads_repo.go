package data

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type ChannelDMThread struct {
	ID                uuid.UUID
	ChannelID         uuid.UUID
	ChannelIdentityID uuid.UUID
	PersonaID         uuid.UUID
	ThreadID          uuid.UUID
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type ChannelDMThreadsRepository struct {
	db Querier
}

func NewChannelDMThreadsRepository(db Querier) (*ChannelDMThreadsRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &ChannelDMThreadsRepository{db: db}, nil
}

func (r *ChannelDMThreadsRepository) WithTx(tx pgx.Tx) *ChannelDMThreadsRepository {
	return &ChannelDMThreadsRepository{db: tx}
}

const channelDMThreadColumns = `id, channel_id, channel_identity_id, persona_id, thread_id, created_at, updated_at`

func scanChannelDMThread(row interface{ Scan(dest ...any) error }) (ChannelDMThread, error) {
	var item ChannelDMThread
	err := row.Scan(
		&item.ID,
		&item.ChannelID,
		&item.ChannelIdentityID,
		&item.PersonaID,
		&item.ThreadID,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	return item, err
}

func (r *ChannelDMThreadsRepository) GetByBinding(
	ctx context.Context,
	channelID uuid.UUID,
	channelIdentityID uuid.UUID,
	personaID uuid.UUID,
) (*ChannelDMThread, error) {
	item, err := scanChannelDMThread(r.db.QueryRow(
		ctx,
		`SELECT `+channelDMThreadColumns+`
		 FROM channel_dm_threads
		 WHERE channel_id = $1 AND channel_identity_id = $2 AND persona_id = $3`,
		channelID,
		channelIdentityID,
		personaID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("channel_dm_threads.GetByBinding: %w", err)
	}
	return &item, nil
}

func (r *ChannelDMThreadsRepository) ListByChannelIdentity(
	ctx context.Context,
	channelID uuid.UUID,
	channelIdentityID uuid.UUID,
) ([]ChannelDMThread, error) {
	rows, err := r.db.Query(
		ctx,
		`SELECT `+channelDMThreadColumns+`
		 FROM channel_dm_threads
		 WHERE channel_id = $1 AND channel_identity_id = $2
		 ORDER BY created_at ASC`,
		channelID,
		channelIdentityID,
	)
	if err != nil {
		return nil, fmt.Errorf("channel_dm_threads.ListByChannelIdentity: %w", err)
	}
	defer rows.Close()

	items := make([]ChannelDMThread, 0)
	for rows.Next() {
		item, err := scanChannelDMThread(rows)
		if err != nil {
			return nil, fmt.Errorf("channel_dm_threads.ListByChannelIdentity scan: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *ChannelDMThreadsRepository) Create(
	ctx context.Context,
	channelID uuid.UUID,
	channelIdentityID uuid.UUID,
	personaID uuid.UUID,
	threadID uuid.UUID,
) (ChannelDMThread, error) {
	if channelID == uuid.Nil || channelIdentityID == uuid.Nil || personaID == uuid.Nil || threadID == uuid.Nil {
		return ChannelDMThread{}, fmt.Errorf("channel_dm_threads: ids must not be empty")
	}
	item, err := scanChannelDMThread(r.db.QueryRow(
		ctx,
		`INSERT INTO channel_dm_threads (channel_id, channel_identity_id, persona_id, thread_id)
		 VALUES ($1, $2, $3, $4)
		 RETURNING `+channelDMThreadColumns,
		channelID,
		channelIdentityID,
		personaID,
		threadID,
	))
	if err != nil {
		return ChannelDMThread{}, fmt.Errorf("channel_dm_threads.Create: %w", err)
	}
	return item, nil
}

func (r *ChannelDMThreadsRepository) GetOrCreate(
	ctx context.Context,
	channelID uuid.UUID,
	channelIdentityID uuid.UUID,
	personaID uuid.UUID,
	threadID uuid.UUID,
) (ChannelDMThread, error) {
	existing, err := r.GetByBinding(ctx, channelID, channelIdentityID, personaID)
	if err != nil {
		return ChannelDMThread{}, err
	}
	if existing != nil {
		return *existing, nil
	}
	created, err := r.Create(ctx, channelID, channelIdentityID, personaID, threadID)
	if err == nil {
		return created, nil
	}
	if isUniqueViolation(err) {
		existing, getErr := r.GetByBinding(ctx, channelID, channelIdentityID, personaID)
		if getErr != nil {
			return ChannelDMThread{}, getErr
		}
		if existing != nil {
			return *existing, nil
		}
	}
	return ChannelDMThread{}, err
}
