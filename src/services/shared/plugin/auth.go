package plugin

import "context"

type AuthProvider interface {
	Name() string
	AuthCodeURL(ctx context.Context, state string) (string, error)
	ExchangeToken(ctx context.Context, code string) (*ExternalIdentity, error)
	RefreshExternalToken(ctx context.Context, refreshToken string) (*ExternalIdentity, error)
}

type ExternalIdentity struct {
	ProviderName string
	ExternalID   string
	Email        string
	DisplayName  string
	AvatarURL    string
	RawClaims    map[string]any
}
