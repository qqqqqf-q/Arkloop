package data

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type ShellSessionRepository struct {
	db Querier
}

func NewShellSessionRepository(db Querier) (*ShellSessionRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &ShellSessionRepository{db: db}, nil
}

func (r *ShellSessionRepository) GetRunIDBySessionRef(ctx context.Context, accountID uuid.UUID, sessionRef string) (*uuid.UUID, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if accountID == uuid.Nil {
		return nil, fmt.Errorf("account_id must not be empty")
	}
	sessionRef = strings.TrimSpace(sessionRef)
	if sessionRef == "" {
		return nil, fmt.Errorf("session_ref must not be empty")
	}

	var runID *uuid.UUID
	err := r.db.QueryRow(
		ctx,
		`SELECT run_id
		   FROM shell_sessions
		  WHERE account_id = $1
		    AND session_ref = $2`,
		accountID,
		sessionRef,
	).Scan(&runID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return runID, nil
}
