//go:build !desktop

package auth

import "context"

func (s *Service) resolvePasswordUnset(_ context.Context, _ string) (ResolvedIdentity, bool, error) {
	return ResolvedIdentity{}, false, nil
}
