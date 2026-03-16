//go:build desktop

package data

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type AccountMembershipRecord struct {
	AccountID  uuid.UUID
	UserID uuid.UUID
	Role   string
}

type AccountMembershipsRepository struct{}

func (AccountMembershipsRepository) GetByAccountAndUser(
	ctx context.Context,
	pool DesktopDB,
	accountID uuid.UUID,
	userID uuid.UUID,
) (*AccountMembershipRecord, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if pool == nil {
		return nil, errors.New("pool must not be nil")
	}
	if accountID == uuid.Nil {
		return nil, errors.New("account_id must not be empty")
	}
	if userID == uuid.Nil {
		return nil, errors.New("user_id must not be empty")
	}

	var record AccountMembershipRecord
	err := pool.QueryRow(
		ctx,
		`SELECT account_id, user_id, role
		   FROM account_memberships
		  WHERE account_id = $1
		    AND user_id = $2
		  LIMIT 1`,
		accountID,
		userID,
	).Scan(&record.AccountID, &record.UserID, &record.Role)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &record, nil
}
