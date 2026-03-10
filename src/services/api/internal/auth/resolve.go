package auth

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"time"

	"arkloop/services/api/internal/data"

	"github.com/google/uuid"
)

type ResolveNextStep string

const (
	ResolveNextStepPassword ResolveNextStep = "password"
	ResolveNextStepRegister ResolveNextStep = "register"
)

type ResolvedIdentity struct {
	NextStep     ResolveNextStep
	FlowToken    string
	MaskedEmail  string
	OTPAvailable bool
	PrefillLogin string
	PrefillEmail string
}

type ResolvedFlow struct {
	UserID uuid.UUID
	Email  string
	Login  string
}

type InvalidIdentityError struct{}

func (InvalidIdentityError) Error() string {
	return "identity is invalid"
}

type FlowTokenInvalidError struct{}

func (FlowTokenInvalidError) Error() string {
	return "flow token invalid or expired"
}

type OTPUnavailableError struct{}

func (OTPUnavailableError) Error() string {
	return "otp unavailable"
}

func (s *Service) ResolveIdentity(ctx context.Context, identity string) (ResolvedIdentity, error) {
	identity = strings.TrimSpace(identity)
	if identity == "" {
		return ResolvedIdentity{}, InvalidIdentityError{}
	}

	credential, err := s.credentialRepo.GetByLogin(ctx, identity)
	if err != nil {
		return ResolvedIdentity{}, err
	}
	if credential != nil {
		return s.resolvePasswordStep(ctx, credential, identity)
	}

	if strings.Contains(identity, "@") {
		if !isValidResolveEmail(identity) {
			return ResolvedIdentity{}, InvalidIdentityError{}
		}
		credential, err = s.credentialRepo.GetByUserEmail(ctx, identity)
		if err != nil {
			return ResolvedIdentity{}, err
		}
		if credential != nil {
			return s.resolvePasswordStep(ctx, credential, identity)
		}
		return ResolvedIdentity{
			NextStep:     ResolveNextStepRegister,
			PrefillEmail: identity,
		}, nil
	}

	return ResolvedIdentity{
		NextStep:     ResolveNextStepRegister,
		PrefillLogin: identity,
	}, nil
}

func (s *Service) ResolveFlow(ctx context.Context, flowToken string) (ResolvedFlow, error) {
	verified, err := s.tokenService.VerifyAuthFlowToken(strings.TrimSpace(flowToken))
	if err != nil {
		var invalid TokenInvalidError
		var expired TokenExpiredError
		if errors.As(err, &invalid) || errors.As(err, &expired) {
			return ResolvedFlow{}, FlowTokenInvalidError{}
		}
		return ResolvedFlow{}, err
	}

	user, err := s.userRepo.GetByID(ctx, verified.UserID)
	if err != nil {
		return ResolvedFlow{}, err
	}
	if user == nil || user.DeletedAt != nil {
		return ResolvedFlow{}, FlowTokenInvalidError{}
	}

	credential, err := s.credentialRepo.GetByUserID(ctx, verified.UserID)
	if err != nil {
		return ResolvedFlow{}, err
	}
	login := ""
	if credential != nil {
		login = credential.Login
	}

	if user.Email == nil || strings.TrimSpace(*user.Email) == "" || !isValidResolveEmail(strings.TrimSpace(*user.Email)) {
		return ResolvedFlow{}, OTPUnavailableError{}
	}

	return ResolvedFlow{
		UserID: user.ID,
		Email:  strings.TrimSpace(*user.Email),
		Login:  login,
	}, nil
}

func (s *Service) resolvePasswordStep(ctx context.Context, credential *data.UserCredential, fallbackLogin string) (ResolvedIdentity, error) {
	user, err := s.userRepo.GetByID(ctx, credential.UserID)
	if err != nil {
		return ResolvedIdentity{}, err
	}
	if user == nil {
		return ResolvedIdentity{}, errors.New("user not found for credential")
	}

	flowToken, err := s.tokenService.IssueAuthFlowToken(user.ID, time.Now().UTC())
	if err != nil {
		return ResolvedIdentity{}, err
	}

	result := ResolvedIdentity{
		NextStep:  ResolveNextStepPassword,
		FlowToken: flowToken,
	}
	if credential.Login != "" {
		result.PrefillLogin = credential.Login
	} else {
		result.PrefillLogin = fallbackLogin
	}
	if user.Email != nil {
		email := strings.TrimSpace(*user.Email)
		if email != "" && isValidResolveEmail(email) {
			result.MaskedEmail = maskResolvedEmail(email)
			result.OTPAvailable = true
		}
	}
	return result, nil
}

func maskResolvedEmail(email string) string {
	parts := strings.SplitN(email, "@", 2)
	if len(parts) != 2 {
		return email
	}
	local, domain := parts[0], parts[1]
	if len(local) <= 1 {
		return local + "***@" + domain
	}
	if len(local) == 2 {
		return local[:1] + "***@" + domain
	}
	return local[:1] + "***" + local[len(local)-1:] + "@" + domain
}

func isValidResolveEmail(value string) bool {
	if strings.ContainsAny(value, "\r\n") {
		return false
	}
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	addr, err := mail.ParseAddress(trimmed)
	if err != nil || addr == nil {
		return false
	}
	return addr.Address == trimmed
}
