package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"arkloop/services/api/internal/data"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type countingRow struct {
	scan func(dest ...any) error
}

func (r *countingRow) Scan(dest ...any) error { return r.scan(dest...) }

type countingQuerier struct {
	tokensInvalidBefore time.Time
	userExists          bool

	queryRowCalls int
	execCalls     int

	execErr error
}

func (q *countingQuerier) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	_ = ctx
	_ = sql
	_ = args
	q.execCalls++
	if q.execErr != nil {
		return pgconn.CommandTag{}, q.execErr
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (q *countingQuerier) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	_ = ctx
	_ = sql
	_ = args
	return nil, errors.New("not implemented")
}

func (q *countingQuerier) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	_ = ctx
	_ = sql
	_ = args
	q.queryRowCalls++
	return &countingRow{scan: func(dest ...any) error {
		if !q.userExists {
			return pgx.ErrNoRows
		}
		if len(dest) != 1 {
			return errors.New("unexpected scan dest")
		}
		ptr, ok := dest[0].(*time.Time)
		if !ok {
			return errors.New("unexpected scan dest type")
		}
		*ptr = q.tokensInvalidBefore
		return nil
	}}
}

func TestVerifyAccessTokenForActor_QueriesDB(t *testing.T) {
	userID := uuid.New()

	q := &countingQuerier{
		tokensInvalidBefore: time.Unix(0, 0).UTC(),
		userExists:          true,
	}
	userRepo, err := data.NewUserRepository(q)
	if err != nil {
		t.Fatalf("new user repo: %v", err)
	}

	tokenSvc, err := NewJwtAccessTokenService("test-secret-should-be-long-enough-32chars", 3600, 3600)
	if err != nil {
		t.Fatalf("new token service: %v", err)
	}

	token, err := tokenSvc.Issue(userID, uuid.New(), "owner", time.Now().UTC())
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	svc := &Service{
		userRepo:     userRepo,
		tokenService: tokenSvc,
	}

	if _, err := svc.VerifyAccessTokenForActor(context.Background(), token); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if q.queryRowCalls != 1 {
		t.Fatalf("expected 1 db call, got %d", q.queryRowCalls)
	}
}

func TestBumpTokensInvalidBefore_WritesDB(t *testing.T) {
	userID := uuid.New()
	now := time.Unix(123, 456789000).UTC() // already micro aligned

	q := &countingQuerier{
		tokensInvalidBefore: time.Unix(0, 0).UTC(),
		userExists:          true,
	}
	userRepo, err := data.NewUserRepository(q)
	if err != nil {
		t.Fatalf("new user repo: %v", err)
	}

	svc := &Service{
		userRepo: userRepo,
	}

	if err := svc.BumpTokensInvalidBefore(context.Background(), userID, now); err != nil {
		t.Fatalf("bump: %v", err)
	}
	if q.execCalls != 1 {
		t.Fatalf("expected 1 db exec, got %d", q.execCalls)
	}
}
