package auth

import (
	"context"
	"errors"
	"strings"

	"arkloop/services/api/internal/data"
)

type ResolveNextStep string

const (
	ResolveNextStepPassword      ResolveNextStep = "password"
	ResolveNextStepSetupRequired ResolveNextStep = "setup_required"
)

type ResolvedIdentity struct {
	NextStep     ResolveNextStep
	PrefillLogin string
	PrefillEmail string
}

type InvalidIdentityError struct{}

func (InvalidIdentityError) Error() string {
	return "identity is invalid"
}

// ResolveIdentity 本机身份解析:只识别已有 credential 的 owner(login 或 email),
// 以及 desktop 密码未设置的 setup 分支;远程注册语义已随 register 端点移除。
func (s *Service) ResolveIdentity(ctx context.Context, identity string) (ResolvedIdentity, error) {
	identity = strings.TrimSpace(identity)
	if identity == "" {
		return ResolvedIdentity{}, InvalidIdentityError{}
	}
	if resolved, ok, err := s.resolvePasswordUnset(ctx, identity); err != nil {
		return ResolvedIdentity{}, err
	} else if ok {
		return resolved, nil
	}

	credential, err := s.credentialRepo.GetByLogin(ctx, identity)
	if err != nil {
		return ResolvedIdentity{}, err
	}
	if credential == nil && strings.Contains(identity, "@") {
		credential, err = s.credentialRepo.GetByUserEmail(ctx, identity)
		if err != nil {
			return ResolvedIdentity{}, err
		}
	}
	if credential == nil {
		return ResolvedIdentity{}, InvalidIdentityError{}
	}

	return s.resolvePasswordStep(ctx, credential, identity)
}

func (s *Service) resolvePasswordStep(ctx context.Context, credential *data.UserCredential, fallbackLogin string) (ResolvedIdentity, error) {
	user, err := s.userRepo.GetByID(ctx, credential.UserID)
	if err != nil {
		return ResolvedIdentity{}, err
	}
	if user == nil {
		return ResolvedIdentity{}, errors.New("user not found for credential")
	}

	result := ResolvedIdentity{
		NextStep: ResolveNextStepPassword,
	}
	if credential.Login != "" {
		result.PrefillLogin = credential.Login
	} else {
		result.PrefillLogin = fallbackLogin
	}
	return result, nil
}
