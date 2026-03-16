//go:build !desktop

package auth

import (
	"context"

	"arkloop/services/api/internal/data"
)

func interceptDesktopUser(_ context.Context, _ *data.UserRepository) (*data.User, bool) {
	return nil, false
}

func interceptDesktopActor(_ string) (VerifiedAccessToken, bool) {
	return VerifiedAccessToken{}, false
}
