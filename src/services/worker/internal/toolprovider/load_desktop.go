//go:build desktop

package toolprovider

import (
	"context"
	"fmt"
	"strings"

	"arkloop/services/shared/desktop"
	sharedencryption "arkloop/services/shared/encryption"
	sharedtoolruntime "arkloop/services/shared/toolruntime"
	"arkloop/services/worker/internal/data"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ActiveProviderConfig matches SaaS toolprovider for RunContext injection.
type ActiveProviderConfig struct {
	OwnerKind    string
	GroupName    string
	ProviderName string
	APIKeyValue  *string
	KeyPrefix    *string
	BaseURL      *string
	ConfigJSON   map[string]any
}

func ToRuntimeProviderConfig(cfg ActiveProviderConfig) sharedtoolruntime.ProviderConfig {
	return sharedtoolruntime.ProviderConfig{
		GroupName:    strings.TrimSpace(cfg.GroupName),
		ProviderName: strings.TrimSpace(cfg.ProviderName),
		BaseURL:      cfg.BaseURL,
		APIKeyValue:  cfg.APIKeyValue,
		ConfigJSON:   copyJSONMapDesktop(cfg.ConfigJSON),
	}
}

func copyJSONMapDesktop(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]any, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func LoadActivePlatformProviders(ctx context.Context, pool *pgxpool.Pool) ([]ActiveProviderConfig, error) {
	_ = ctx
	_ = pool
	return nil, nil
}

func LoadActiveUserProviders(_ context.Context, _ *pgxpool.Pool, _ uuid.UUID) ([]ActiveProviderConfig, error) {
	return nil, nil
}

func activeConfigsFromStatuses(statuses []sharedtoolruntime.ProviderRuntimeStatus) []ActiveProviderConfig {
	out := make([]ActiveProviderConfig, 0, len(statuses))
	for _, status := range statuses {
		if !status.Ready() {
			continue
		}
		out = append(out, ActiveProviderConfig{
			OwnerKind:    status.OwnerKind,
			GroupName:    status.GroupName,
			ProviderName: status.ProviderName,
			APIKeyValue:  status.APIKeyValue,
			KeyPrefix:    status.KeyPrefix,
			BaseURL:      status.BaseURL,
			ConfigJSON:   status.ConfigJSON,
		})
	}
	return out
}

// LoadDesktopActiveToolProviders returns active rows from SQLite for platform then optional user scope.
func LoadDesktopActiveToolProviders(ctx context.Context, db data.DesktopDB, userID *uuid.UUID) (platform []ActiveProviderConfig, user []ActiveProviderConfig, err error) {
	if db == nil {
		return nil, nil, nil
	}

	var keyRing *sharedencryption.KeyRing
	decrypt := func(_ context.Context, encrypted string, keyVersion *int, _ string) (*string, error) {
		if keyVersion == nil {
			return nil, fmt.Errorf("missing key version")
		}
		if keyRing == nil {
			ring, err := desktop.LoadEncryptionKeyRing(desktop.KeyRingOptions{})
			if err != nil {
				return nil, err
			}
			keyRing = ring
		}
		plain, err := keyRing.Decrypt(encrypted, *keyVersion)
		if err != nil {
			return nil, err
		}
		value := string(plain)
		return &value, nil
	}

	platformStatuses, err := sharedtoolruntime.LoadPlatformProviderStatuses(ctx, db, decrypt)
	if err != nil {
		return nil, nil, err
	}
	platform = activeConfigsFromStatuses(platformStatuses)

	if userID != nil && *userID != uuid.Nil {
		userStatuses, err := sharedtoolruntime.LoadUserProviderStatuses(ctx, db, *userID, decrypt)
		if err != nil {
			return nil, nil, err
		}
		user = activeConfigsFromStatuses(userStatuses)
	}

	return platform, user, nil
}
