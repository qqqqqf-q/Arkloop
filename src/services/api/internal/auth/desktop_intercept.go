//go:build desktop

package auth

import (
	"context"
	"strings"
	"time"

	"arkloop/services/api/internal/data"
)

func interceptDesktopUser(ctx context.Context, repo *data.UserRepository) (*data.User, bool) {
	if repo != nil {
		if user, err := repo.GetByID(ctx, DesktopUserID); err == nil && user != nil {
			return user, true
		}
	}
	return &data.User{
		ID:        DesktopUserID,
		Username:  DesktopPreferredUsername(),
		Status:    "active",
		CreatedAt: time.Now(),
	}, true
}

func interceptDesktopActor(token string) (VerifiedAccessToken, bool) {
	if strings.TrimSpace(token) != DesktopToken() {
		return VerifiedAccessToken{}, false
	}
	return DesktopVerifiedAccessToken(), true
}
