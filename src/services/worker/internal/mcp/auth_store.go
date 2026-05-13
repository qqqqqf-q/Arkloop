package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	sharedmcpinstall "arkloop/services/shared/mcpinstall"
	workercrypto "arkloop/services/worker/internal/crypto"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

type AuthStore interface {
	Save(ctx context.Context, secretID uuid.UUID, payload sharedmcpinstall.AuthPayload) error
}

type authStoreDB interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

type DBAuthStore struct {
	db authStoreDB
}

func NewDBAuthStore(db authStoreDB) *DBAuthStore {
	if db == nil {
		return nil
	}
	return &DBAuthStore{db: db}
}

func (s *DBAuthStore) Save(ctx context.Context, secretID uuid.UUID, payload sharedmcpinstall.AuthPayload) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("mcp auth: marshal payload: %w", err)
	}
	if s == nil || s.db == nil {
		return nil
	}
	if secretID == uuid.Nil {
		return fmt.Errorf("mcp auth: secret id is required")
	}
	ciphertext, keyVersion, err := workercrypto.EncryptWithCurrentKey(encoded)
	if err != nil {
		return fmt.Errorf("mcp auth: encrypt payload: %w", err)
	}
	tag, err := s.db.Exec(ctx, `
		UPDATE secrets
		   SET encrypted_value = $1,
		       key_version = $2,
		       updated_at = now()
		 WHERE id = $3
	`, ciphertext, keyVersion, secretID)
	if err != nil {
		return fmt.Errorf("mcp auth: update secret: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("mcp auth: secret not found")
	}
	return nil
}
