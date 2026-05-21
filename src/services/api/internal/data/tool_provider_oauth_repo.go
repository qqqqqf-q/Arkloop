package data

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type ToolProviderOAuthConnection struct {
	ID            uuid.UUID
	OwnerKind     string
	OwnerUserID   *uuid.UUID
	GroupName     string
	ProviderName  string
	TokenSecretID uuid.UUID
	ClientID      *string
	Scope         *string
	ExpiresAt     *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type ToolProviderOAuthFlow struct {
	ID                   uuid.UUID
	OwnerKind            string
	OwnerUserID          *uuid.UUID
	GroupName            string
	ProviderName         string
	State                string
	RedirectURI          string
	AuthorizationURL     string
	CodeVerifierSecretID uuid.UUID
	ClientID             *string
	Scope                *string
	ExpiresAt            time.Time
	CompletedAt          *time.Time
	ConnectionID         *uuid.UUID
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type ToolProviderOAuthRepository struct {
	db Querier
}

func NewToolProviderOAuthRepository(db Querier) (*ToolProviderOAuthRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &ToolProviderOAuthRepository{db: db}, nil
}

func (r *ToolProviderOAuthRepository) WithTx(tx pgx.Tx) *ToolProviderOAuthRepository {
	return &ToolProviderOAuthRepository{db: tx}
}

func (r *ToolProviderOAuthRepository) UpsertConnection(ctx context.Context, conn ToolProviderOAuthConnection) (ToolProviderOAuthConnection, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validateToolProviderOAuthOwner(conn.OwnerKind, conn.OwnerUserID); err != nil {
		return ToolProviderOAuthConnection{}, err
	}
	conn.GroupName = strings.TrimSpace(conn.GroupName)
	conn.ProviderName = strings.TrimSpace(conn.ProviderName)
	conn.ClientID = nullableTrimmedPtr(conn.ClientID)
	conn.Scope = nullableTrimmedPtr(conn.Scope)
	if conn.GroupName == "" || conn.ProviderName == "" || conn.TokenSecretID == uuid.Nil {
		return ToolProviderOAuthConnection{}, fmt.Errorf("tool provider oauth connection is invalid")
	}

	if conn.OwnerKind == "platform" {
		err := r.db.QueryRow(ctx, `
INSERT INTO tool_provider_oauth_connections (
    owner_kind, owner_user_id, group_name, provider_name, token_secret_id,
    client_id, scope, expires_at
) VALUES ('platform', NULL, $1, $2, $3, $4, $5, $6)
ON CONFLICT (group_name, provider_name) WHERE owner_kind = 'platform'
DO UPDATE SET token_secret_id = EXCLUDED.token_secret_id,
              client_id = EXCLUDED.client_id,
              scope = EXCLUDED.scope,
              expires_at = EXCLUDED.expires_at,
              updated_at = now()
RETURNING id, owner_kind, owner_user_id, group_name, provider_name, token_secret_id,
          client_id, scope, expires_at, created_at, updated_at`,
			conn.GroupName, conn.ProviderName, conn.TokenSecretID, conn.ClientID, conn.Scope, conn.ExpiresAt,
		).Scan(&conn.ID, &conn.OwnerKind, &conn.OwnerUserID, &conn.GroupName, &conn.ProviderName, &conn.TokenSecretID, &conn.ClientID, &conn.Scope, &conn.ExpiresAt, &conn.CreatedAt, &conn.UpdatedAt)
		return conn, err
	}

	err := r.db.QueryRow(ctx, `
INSERT INTO tool_provider_oauth_connections (
    owner_kind, owner_user_id, group_name, provider_name, token_secret_id,
    client_id, scope, expires_at
) VALUES ('user', $1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (owner_user_id, group_name, provider_name) WHERE owner_kind = 'user' AND owner_user_id IS NOT NULL
DO UPDATE SET token_secret_id = EXCLUDED.token_secret_id,
              client_id = EXCLUDED.client_id,
              scope = EXCLUDED.scope,
              expires_at = EXCLUDED.expires_at,
              updated_at = now()
RETURNING id, owner_kind, owner_user_id, group_name, provider_name, token_secret_id,
          client_id, scope, expires_at, created_at, updated_at`,
		*conn.OwnerUserID, conn.GroupName, conn.ProviderName, conn.TokenSecretID, conn.ClientID, conn.Scope, conn.ExpiresAt,
	).Scan(&conn.ID, &conn.OwnerKind, &conn.OwnerUserID, &conn.GroupName, &conn.ProviderName, &conn.TokenSecretID, &conn.ClientID, &conn.Scope, &conn.ExpiresAt, &conn.CreatedAt, &conn.UpdatedAt)
	return conn, err
}

func (r *ToolProviderOAuthRepository) CreateFlow(ctx context.Context, flow ToolProviderOAuthFlow) (ToolProviderOAuthFlow, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validateToolProviderOAuthOwner(flow.OwnerKind, flow.OwnerUserID); err != nil {
		return ToolProviderOAuthFlow{}, err
	}
	flow.GroupName = strings.TrimSpace(flow.GroupName)
	flow.ProviderName = strings.TrimSpace(flow.ProviderName)
	flow.State = strings.TrimSpace(flow.State)
	flow.RedirectURI = strings.TrimSpace(flow.RedirectURI)
	flow.AuthorizationURL = strings.TrimSpace(flow.AuthorizationURL)
	flow.ClientID = nullableTrimmedPtr(flow.ClientID)
	flow.Scope = nullableTrimmedPtr(flow.Scope)
	if flow.GroupName == "" || flow.ProviderName == "" || flow.State == "" || flow.RedirectURI == "" ||
		flow.AuthorizationURL == "" || flow.CodeVerifierSecretID == uuid.Nil || flow.ExpiresAt.IsZero() {
		return ToolProviderOAuthFlow{}, fmt.Errorf("tool provider oauth flow is invalid")
	}
	err := r.db.QueryRow(ctx, `
INSERT INTO tool_provider_oauth_flows (
    owner_kind, owner_user_id, group_name, provider_name, state, redirect_uri,
    authorization_url, code_verifier_secret_id, client_id, scope, expires_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING id, owner_kind, owner_user_id, group_name, provider_name, state, redirect_uri,
          authorization_url, code_verifier_secret_id, client_id, scope, expires_at,
          completed_at, connection_id, created_at, updated_at`,
		flow.OwnerKind, flow.OwnerUserID, flow.GroupName, flow.ProviderName, flow.State, flow.RedirectURI,
		flow.AuthorizationURL, flow.CodeVerifierSecretID, flow.ClientID, flow.Scope, flow.ExpiresAt.UTC(),
	).Scan(&flow.ID, &flow.OwnerKind, &flow.OwnerUserID, &flow.GroupName, &flow.ProviderName, &flow.State, &flow.RedirectURI,
		&flow.AuthorizationURL, &flow.CodeVerifierSecretID, &flow.ClientID, &flow.Scope, &flow.ExpiresAt,
		&flow.CompletedAt, &flow.ConnectionID, &flow.CreatedAt, &flow.UpdatedAt)
	return flow, err
}

func (r *ToolProviderOAuthRepository) GetFlowByState(ctx context.Context, state string) (*ToolProviderOAuthFlow, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var flow ToolProviderOAuthFlow
	err := r.db.QueryRow(ctx, `
SELECT id, owner_kind, owner_user_id, group_name, provider_name, state, redirect_uri,
       authorization_url, code_verifier_secret_id, client_id, scope, expires_at,
       completed_at, connection_id, created_at, updated_at
  FROM tool_provider_oauth_flows
 WHERE state = $1`, strings.TrimSpace(state),
	).Scan(&flow.ID, &flow.OwnerKind, &flow.OwnerUserID, &flow.GroupName, &flow.ProviderName, &flow.State, &flow.RedirectURI,
		&flow.AuthorizationURL, &flow.CodeVerifierSecretID, &flow.ClientID, &flow.Scope, &flow.ExpiresAt,
		&flow.CompletedAt, &flow.ConnectionID, &flow.CreatedAt, &flow.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &flow, nil
}

func (r *ToolProviderOAuthRepository) CompleteFlow(ctx context.Context, state string, connectionID uuid.UUID) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if connectionID == uuid.Nil {
		return fmt.Errorf("connection_id must not be empty")
	}
	_, err := r.db.Exec(ctx, `
UPDATE tool_provider_oauth_flows
   SET completed_at = now(), connection_id = $2, updated_at = now()
 WHERE state = $1`, strings.TrimSpace(state), connectionID)
	return err
}

func validateToolProviderOAuthOwner(ownerKind string, ownerUserID *uuid.UUID) error {
	switch strings.TrimSpace(ownerKind) {
	case "platform":
		if ownerUserID != nil {
			return fmt.Errorf("owner_user_id must be empty for platform owner")
		}
	case "user":
		if ownerUserID == nil || *ownerUserID == uuid.Nil {
			return fmt.Errorf("owner_user_id must not be empty for user owner")
		}
	default:
		return fmt.Errorf("owner_kind must be user or platform")
	}
	return nil
}
