//go:build desktop

package toolprovider

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ActiveProviderConfig mirrors the cloud definition for compilation in desktop mode.
type ActiveProviderConfig struct {
	Scope        string
	GroupName    string
	ProviderName string
	APIKeyValue  *string
	KeyPrefix    *string
	BaseURL      *string
	ConfigJSON   map[string]any
}

func LoadActiveOrgProviders(_ context.Context, _ *pgxpool.Pool, _ uuid.UUID) ([]ActiveProviderConfig, error) {
	return nil, nil
}

func LoadActivePlatformProviders(_ context.Context, _ *pgxpool.Pool) ([]ActiveProviderConfig, error) {
	return nil, nil
}
