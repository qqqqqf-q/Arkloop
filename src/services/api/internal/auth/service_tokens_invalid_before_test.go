package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"arkloop/services/api/internal/data"
	"arkloop/services/shared/database"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
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

func (q *countingQuerier) Exec(ctx context.Context, sql string, args ...any) (database.Result, error) {
	_ = ctx
	_ = sql
	_ = args
	q.execCalls++
	if q.execErr != nil {
		return stubResult{}, q.execErr
	}
	return stubResult{tag: "UPDATE 1"}, nil
}

func (q *countingQuerier) Query(ctx context.Context, sql string, args ...any) (database.Rows, error) {
	_ = ctx
	_ = sql
	_ = args
	return nil, errors.New("not implemented")
}

func (q *countingQuerier) QueryRow(ctx context.Context, sql string, args ...any) database.Row {
	_ = ctx
	_ = sql
	_ = args
	q.queryRowCalls++
	return &countingRow{scan: func(dest ...any) error {
		if !q.userExists {
			return database.ErrNoRows
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

func newRedisClient(t *testing.T, addr string) *redis.Client {
	t.Helper()
	client := redis.NewClient(&redis.Options{
		Addr:                  addr,
		ContextTimeoutEnabled: true,
	})
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Fatalf("redis ping: %v", err)
	}
	return client
}

func TestVerifyAccessTokenForActor_RedisHitDoesNotQueryDB(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := newRedisClient(t, mr.Addr())

	userID := uuid.New()

	key := tokensInvalidBeforeRedisKeyPrefix + userID.String()
	mr.Set(key, "0")

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
		redisClient:  rdb,
	}

	if _, err := svc.VerifyAccessTokenForActor(context.Background(), token); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if q.queryRowCalls != 0 {
		t.Fatalf("expected 0 db calls, got %d", q.queryRowCalls)
	}
}

func TestVerifyAccessTokenForActor_RedisMissFallsBackToDBAndBackfills(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := newRedisClient(t, mr.Addr())

	userID := uuid.New()
	dbVal := time.Unix(0, 0).UTC()

	q := &countingQuerier{
		tokensInvalidBefore: dbVal,
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
		redisClient:  rdb,
	}

	if _, err := svc.VerifyAccessTokenForActor(context.Background(), token); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if q.queryRowCalls != 1 {
		t.Fatalf("expected 1 db call, got %d", q.queryRowCalls)
	}

	key := tokensInvalidBeforeRedisKeyPrefix + userID.String()
	got, err := mr.Get(key)
	if err != nil {
		t.Fatalf("get redis key: %v", err)
	}
	if got != "0" {
		t.Fatalf("unexpected redis value: got %q want %q", got, "0")
	}
}

func TestVerifyAccessTokenForActor_RedisErrorFallsBackToDB(t *testing.T) {
	userID := uuid.New()
	dbVal := time.Unix(0, 0).UTC()

	q := &countingQuerier{
		tokensInvalidBefore: dbVal,
		userExists:          true,
	}
	userRepo, err := data.NewUserRepository(q)
	if err != nil {
		t.Fatalf("new user repo: %v", err)
	}

	// 使用不可用地址模拟 Redis 错误（连接拒绝即可触发回源逻辑）
	rdb := redis.NewClient(&redis.Options{
		Addr:                  "127.0.0.1:1",
		ContextTimeoutEnabled: true,
	})

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
		redisClient:  rdb,
	}

	if _, err := svc.VerifyAccessTokenForActor(context.Background(), token); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if q.queryRowCalls != 1 {
		t.Fatalf("expected 1 db call, got %d", q.queryRowCalls)
	}
}

func TestBumpTokensInvalidBefore_WritesDBAndRedis(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := newRedisClient(t, mr.Addr())

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

	tokenSvc, err := NewJwtAccessTokenService("test-secret-should-be-long-enough-32chars", 3600, 3600)
	if err != nil {
		t.Fatalf("new token service: %v", err)
	}

	svc := &Service{
		userRepo:     userRepo,
		tokenService: tokenSvc,
		redisClient:  rdb,
	}

	if err := svc.BumpTokensInvalidBefore(context.Background(), userID, now); err != nil {
		t.Fatalf("bump: %v", err)
	}
	if q.execCalls != 1 {
		t.Fatalf("expected 1 db exec, got %d", q.execCalls)
	}

	key := tokensInvalidBeforeRedisKeyPrefix + userID.String()
	got, err := mr.Get(key)
	if err != nil {
		t.Fatalf("get redis key: %v", err)
	}
	want := "123456789"
	if got != want {
		t.Fatalf("unexpected redis value: got %q want %q", got, want)
	}
}
