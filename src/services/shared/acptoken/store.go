package acptoken

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	keyPrefix = "arkloop:acp_token:"
	// DefaultTokenTTL matches the default token expiration.
	DefaultTokenTTL = 30 * time.Minute
)

// TokenState represents the tracked state of an active ACP session token.
type TokenState struct {
	RunID      string   `json:"run_id"`
	AccountID  string   `json:"account_id"`
	Models     []string `json:"models"`
	Budget     int64    `json:"budget"`      // max tokens allowed (0 = unlimited)
	TokensUsed int64    `json:"tokens_used"` // total tokens consumed so far
	CreatedAt  int64    `json:"created_at"`  // unix timestamp
}

// Store manages ACP session token state in Redis.
type Store struct {
	rdb *redis.Client
}

// NewStore creates a new token store.
func NewStore(rdb *redis.Client) *Store {
	return &Store{rdb: rdb}
}

// Register records a new active token. Called when the acp_agent executor issues a token.
func (s *Store) Register(ctx context.Context, runID string, state TokenState, ttl time.Duration) error {
	if s.rdb == nil {
		return nil // no-op if Redis not configured
	}
	if ttl <= 0 {
		ttl = DefaultTokenTTL
	}

	key := keyPrefix + runID
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal token state: %w", err)
	}

	return s.rdb.Set(ctx, key, data, ttl).Err()
}

// Get retrieves the current state of a token by run_id.
func (s *Store) Get(ctx context.Context, runID string) (*TokenState, error) {
	if s.rdb == nil {
		return nil, nil
	}

	key := keyPrefix + runID
	data, err := s.rdb.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil // not found / expired
		}
		return nil, fmt.Errorf("get token state: %w", err)
	}

	var state TokenState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("unmarshal token state: %w", err)
	}
	return &state, nil
}

// RecordUsage atomically increments the tokens_used counter and checks budget.
// Returns (allowed, error). If budget exceeded, allowed=false.
func (s *Store) RecordUsage(ctx context.Context, runID string, tokensUsed int64) (bool, error) {
	if s.rdb == nil {
		return true, nil // no Redis = no budget enforcement
	}

	key := keyPrefix + runID
	data, err := s.rdb.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return false, fmt.Errorf("token not found or expired for run %s", runID)
		}
		return false, fmt.Errorf("get token state: %w", err)
	}

	var state TokenState
	if err := json.Unmarshal(data, &state); err != nil {
		return false, fmt.Errorf("unmarshal token state: %w", err)
	}

	state.TokensUsed += tokensUsed

	// Check budget (0 = unlimited)
	if state.Budget > 0 && state.TokensUsed > state.Budget {
		return false, nil
	}

	// Write back updated state
	updated, err := json.Marshal(state)
	if err != nil {
		return false, fmt.Errorf("marshal updated state: %w", err)
	}

	// Use KEEPTTL to preserve the existing expiration
	if err := s.rdb.Set(ctx, key, updated, redis.KeepTTL).Err(); err != nil {
		return false, fmt.Errorf("update token state: %w", err)
	}

	return true, nil
}

// Revoke removes the token state, effectively invalidating the token.
// Called when a run ends.
func (s *Store) Revoke(ctx context.Context, runID string) error {
	if s.rdb == nil {
		return nil
	}

	key := keyPrefix + runID
	return s.rdb.Del(ctx, key).Err()
}

// IsActive checks if a token is still active for the given run_id.
func (s *Store) IsActive(ctx context.Context, runID string) (bool, error) {
	if s.rdb == nil {
		return true, nil // no Redis = assume active
	}

	key := keyPrefix + runID
	exists, err := s.rdb.Exists(ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("check token existence: %w", err)
	}
	return exists > 0, nil
}
