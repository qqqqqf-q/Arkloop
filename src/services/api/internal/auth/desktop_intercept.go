//go:build desktop

package auth

import (
	"context"
	"time"

	"arkloop/services/api/internal/data"
)

func interceptDesktopUser(_ context.Context, _ *data.UserRepository) (*data.User, bool) {
	return &data.User{
		ID:        DesktopUserID,
		Username:  "desktop",
		Status:    "active",
		CreatedAt: time.Now(),
	}, true
}

func interceptDesktopActor() (VerifiedAccessToken, bool) {
	return DesktopVerifiedAccessToken(), true
}
