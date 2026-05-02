//go:build desktop

package auth

import (
	"context"
	"strings"
)

func (s *Service) resolvePasswordUnset(ctx context.Context, _ string) (ResolvedIdentity, bool, error) {
	if s == nil || s.userRepo == nil || s.credentialRepo == nil {
		return ResolvedIdentity{}, false, nil
	}
	credential, err := s.credentialRepo.GetByUserID(ctx, DesktopUserID)
	if err != nil {
		return ResolvedIdentity{}, false, err
	}
	if credential != nil {
		return ResolvedIdentity{}, false, nil
	}
	user, err := s.userRepo.GetByID(ctx, DesktopUserID)
	if err != nil {
		return ResolvedIdentity{}, false, err
	}
	if user == nil || user.DeletedAt != nil {
		return ResolvedIdentity{}, false, nil
	}

	username := strings.TrimSpace(user.Username)
	email := ""
	if user.Email != nil {
		email = strings.TrimSpace(*user.Email)
	}

	return ResolvedIdentity{
		NextStep:     ResolveNextStepSetupRequired,
		PrefillLogin: username,
		PrefillEmail: email,
	}, true, nil
}
