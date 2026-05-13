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

type MCPOAuthConnection struct {
	ID                         uuid.UUID
	AccountID                  uuid.UUID
	ProfileRef                 string
	InstallID                  uuid.UUID
	TokenSecretID              uuid.UUID
	ClientID                   *string
	ClientSecretSecretID       *uuid.UUID
	RegistrationClientURI      *string
	RegistrationAccessSecretID *uuid.UUID
	Scope                      *string
	ExpiresAt                  *time.Time
	CreatedAt                  time.Time
	UpdatedAt                  time.Time
}

type MCPOAuthFlow struct {
	ID                   uuid.UUID
	AccountID            uuid.UUID
	ProfileRef           string
	InstallID            uuid.UUID
	State                string
	RedirectURI          string
	AuthorizationURL     string
	CodeVerifierSecretID uuid.UUID
	ClientID             *string
	ClientSecretSecretID *uuid.UUID
	Scope                *string
	ExpiresAt            time.Time
	CompletedAt          *time.Time
	ConnectionID         *uuid.UUID
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type MCPOAuthConnectionsRepository struct {
	db Querier
}

func NewMCPOAuthConnectionsRepository(db Querier) (*MCPOAuthConnectionsRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &MCPOAuthConnectionsRepository{db: db}, nil
}

func (r *MCPOAuthConnectionsRepository) WithTx(tx pgx.Tx) *MCPOAuthConnectionsRepository {
	return &MCPOAuthConnectionsRepository{db: tx}
}

func (r *MCPOAuthConnectionsRepository) UpsertConnection(ctx context.Context, conn MCPOAuthConnection) (MCPOAuthConnection, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	conn.ProfileRef = strings.TrimSpace(conn.ProfileRef)
	conn.ClientID = nullableTrimmedPtr(conn.ClientID)
	conn.RegistrationClientURI = nullableTrimmedPtr(conn.RegistrationClientURI)
	conn.Scope = nullableTrimmedPtr(conn.Scope)
	if conn.AccountID == uuid.Nil || conn.ProfileRef == "" || conn.InstallID == uuid.Nil || conn.TokenSecretID == uuid.Nil {
		return MCPOAuthConnection{}, fmt.Errorf("mcp oauth connection is invalid")
	}

	err := r.db.QueryRow(
		ctx,
		`INSERT INTO mcp_oauth_connections (
		    account_id, profile_ref, install_id, token_secret_id, client_id,
		    client_secret_secret_id, registration_client_uri, registration_access_secret_id,
		    scope, expires_at
		) VALUES (
		    $1, $2, $3, $4, $5,
		    $6, $7, $8,
		    $9, $10
		)
		ON CONFLICT (account_id, profile_ref, install_id) DO UPDATE
		SET token_secret_id = EXCLUDED.token_secret_id,
		    client_id = EXCLUDED.client_id,
		    client_secret_secret_id = EXCLUDED.client_secret_secret_id,
		    registration_client_uri = EXCLUDED.registration_client_uri,
		    registration_access_secret_id = EXCLUDED.registration_access_secret_id,
		    scope = EXCLUDED.scope,
		    expires_at = EXCLUDED.expires_at,
		    updated_at = now()
		RETURNING id, account_id, profile_ref, install_id, token_secret_id, client_id,
		          client_secret_secret_id, registration_client_uri, registration_access_secret_id,
		          scope, expires_at, created_at, updated_at`,
		conn.AccountID,
		conn.ProfileRef,
		conn.InstallID,
		conn.TokenSecretID,
		conn.ClientID,
		conn.ClientSecretSecretID,
		conn.RegistrationClientURI,
		conn.RegistrationAccessSecretID,
		conn.Scope,
		conn.ExpiresAt,
	).Scan(
		&conn.ID,
		&conn.AccountID,
		&conn.ProfileRef,
		&conn.InstallID,
		&conn.TokenSecretID,
		&conn.ClientID,
		&conn.ClientSecretSecretID,
		&conn.RegistrationClientURI,
		&conn.RegistrationAccessSecretID,
		&conn.Scope,
		&conn.ExpiresAt,
		&conn.CreatedAt,
		&conn.UpdatedAt,
	)
	return conn, err
}

func (r *MCPOAuthConnectionsRepository) GetConnectionByInstall(ctx context.Context, accountID uuid.UUID, profileRef string, installID uuid.UUID) (*MCPOAuthConnection, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var conn MCPOAuthConnection
	err := r.db.QueryRow(
		ctx,
		`SELECT id, account_id, profile_ref, install_id, token_secret_id, client_id,
		        client_secret_secret_id, registration_client_uri, registration_access_secret_id,
		        scope, expires_at, created_at, updated_at
		   FROM mcp_oauth_connections
		  WHERE account_id = $1 AND profile_ref = $2 AND install_id = $3`,
		accountID,
		strings.TrimSpace(profileRef),
		installID,
	).Scan(
		&conn.ID,
		&conn.AccountID,
		&conn.ProfileRef,
		&conn.InstallID,
		&conn.TokenSecretID,
		&conn.ClientID,
		&conn.ClientSecretSecretID,
		&conn.RegistrationClientURI,
		&conn.RegistrationAccessSecretID,
		&conn.Scope,
		&conn.ExpiresAt,
		&conn.CreatedAt,
		&conn.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &conn, nil
}

func (r *MCPOAuthConnectionsRepository) CreateFlow(ctx context.Context, flow MCPOAuthFlow) (MCPOAuthFlow, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	flow.ProfileRef = strings.TrimSpace(flow.ProfileRef)
	flow.State = strings.TrimSpace(flow.State)
	flow.RedirectURI = strings.TrimSpace(flow.RedirectURI)
	flow.AuthorizationURL = strings.TrimSpace(flow.AuthorizationURL)
	flow.ClientID = nullableTrimmedPtr(flow.ClientID)
	flow.Scope = nullableTrimmedPtr(flow.Scope)
	if flow.AccountID == uuid.Nil || flow.ProfileRef == "" || flow.InstallID == uuid.Nil || flow.State == "" ||
		flow.RedirectURI == "" || flow.AuthorizationURL == "" || flow.CodeVerifierSecretID == uuid.Nil || flow.ExpiresAt.IsZero() {
		return MCPOAuthFlow{}, fmt.Errorf("mcp oauth flow is invalid")
	}

	err := r.db.QueryRow(
		ctx,
		`INSERT INTO mcp_oauth_flows (
		    account_id, profile_ref, install_id, state, redirect_uri, authorization_url,
		    code_verifier_secret_id, client_id, client_secret_secret_id, scope, expires_at
		) VALUES (
		    $1, $2, $3, $4, $5, $6,
		    $7, $8, $9, $10, $11
		)
		RETURNING id, account_id, profile_ref, install_id, state, redirect_uri, authorization_url,
		          code_verifier_secret_id, client_id, client_secret_secret_id, scope, expires_at,
		          completed_at, connection_id, created_at, updated_at`,
		flow.AccountID,
		flow.ProfileRef,
		flow.InstallID,
		flow.State,
		flow.RedirectURI,
		flow.AuthorizationURL,
		flow.CodeVerifierSecretID,
		flow.ClientID,
		flow.ClientSecretSecretID,
		flow.Scope,
		flow.ExpiresAt.UTC(),
	).Scan(
		&flow.ID,
		&flow.AccountID,
		&flow.ProfileRef,
		&flow.InstallID,
		&flow.State,
		&flow.RedirectURI,
		&flow.AuthorizationURL,
		&flow.CodeVerifierSecretID,
		&flow.ClientID,
		&flow.ClientSecretSecretID,
		&flow.Scope,
		&flow.ExpiresAt,
		&flow.CompletedAt,
		&flow.ConnectionID,
		&flow.CreatedAt,
		&flow.UpdatedAt,
	)
	return flow, err
}

func (r *MCPOAuthConnectionsRepository) GetFlowByState(ctx context.Context, state string) (*MCPOAuthFlow, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var flow MCPOAuthFlow
	err := r.db.QueryRow(
		ctx,
		`SELECT id, account_id, profile_ref, install_id, state, redirect_uri, authorization_url,
		        code_verifier_secret_id, client_id, client_secret_secret_id, scope, expires_at,
		        completed_at, connection_id, created_at, updated_at
		   FROM mcp_oauth_flows
		  WHERE state = $1`,
		strings.TrimSpace(state),
	).Scan(
		&flow.ID,
		&flow.AccountID,
		&flow.ProfileRef,
		&flow.InstallID,
		&flow.State,
		&flow.RedirectURI,
		&flow.AuthorizationURL,
		&flow.CodeVerifierSecretID,
		&flow.ClientID,
		&flow.ClientSecretSecretID,
		&flow.Scope,
		&flow.ExpiresAt,
		&flow.CompletedAt,
		&flow.ConnectionID,
		&flow.CreatedAt,
		&flow.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &flow, nil
}

func (r *MCPOAuthConnectionsRepository) CompleteFlow(ctx context.Context, state string, connectionID uuid.UUID) (*MCPOAuthFlow, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	state = strings.TrimSpace(state)
	if state == "" || connectionID == uuid.Nil {
		return nil, fmt.Errorf("state and connection_id must not be empty")
	}

	var flow MCPOAuthFlow
	err := r.db.QueryRow(
		ctx,
		`UPDATE mcp_oauth_flows
		    SET completed_at = now(),
		        connection_id = $2,
		        updated_at = now()
		  WHERE state = $1
		    AND completed_at IS NULL
		    AND expires_at > now()
		RETURNING id, account_id, profile_ref, install_id, state, redirect_uri, authorization_url,
		          code_verifier_secret_id, client_id, client_secret_secret_id, scope, expires_at,
		          completed_at, connection_id, created_at, updated_at`,
		state,
		connectionID,
	).Scan(
		&flow.ID,
		&flow.AccountID,
		&flow.ProfileRef,
		&flow.InstallID,
		&flow.State,
		&flow.RedirectURI,
		&flow.AuthorizationURL,
		&flow.CodeVerifierSecretID,
		&flow.ClientID,
		&flow.ClientSecretSecretID,
		&flow.Scope,
		&flow.ExpiresAt,
		&flow.CompletedAt,
		&flow.ConnectionID,
		&flow.CreatedAt,
		&flow.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &flow, nil
}

func (r *MCPOAuthConnectionsRepository) DeleteExpiredFlows(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	_, err := r.db.Exec(ctx, `DELETE FROM mcp_oauth_flows WHERE expires_at < now()`)
	return err
}

func nullableTrimmedPtr(value *string) *string {
	if value == nil {
		return nil
	}
	cleaned := strings.TrimSpace(*value)
	if cleaned == "" {
		return nil
	}
	return &cleaned
}
