package auth

import (
	"context"
	"errors"
	"time"

	"arkloop/services/api/internal/data"

	"github.com/google/uuid"
)

type InvalidCredentialsError struct{}

func (InvalidCredentialsError) Error() string {
	return "invalid_credentials"
}

type UserNotFoundError struct {
	UserID uuid.UUID
}

func (e UserNotFoundError) Error() string {
	return "user not found"
}

type SuspendedUserError struct {
	UserID uuid.UUID
}

func (e SuspendedUserError) Error() string {
	return "user suspended"
}

type IssuedAccessToken struct {
	Token  string
	UserID uuid.UUID
}

type Service struct {
	userRepo       *data.UserRepository
	credentialRepo *data.UserCredentialRepository
	passwordHasher *BcryptPasswordHasher
	tokenService   *JwtAccessTokenService
}

func NewService(
	userRepo *data.UserRepository,
	credentialRepo *data.UserCredentialRepository,
	passwordHasher *BcryptPasswordHasher,
	tokenService *JwtAccessTokenService,
) (*Service, error) {
	if userRepo == nil {
		return nil, errors.New("userRepo must not be nil")
	}
	if credentialRepo == nil {
		return nil, errors.New("credentialRepo must not be nil")
	}
	if passwordHasher == nil {
		return nil, errors.New("passwordHasher must not be nil")
	}
	if tokenService == nil {
		return nil, errors.New("tokenService must not be nil")
	}
	return &Service{
		userRepo:       userRepo,
		credentialRepo: credentialRepo,
		passwordHasher: passwordHasher,
		tokenService:   tokenService,
	}, nil
}

func (s *Service) IssueAccessToken(ctx context.Context, login string, password string) (IssuedAccessToken, error) {
	credential, err := s.credentialRepo.GetByLogin(ctx, login)
	if err != nil {
		return IssuedAccessToken{}, err
	}
	if credential == nil {
		return IssuedAccessToken{}, InvalidCredentialsError{}
	}
	if !s.passwordHasher.VerifyPassword(password, credential.PasswordHash) {
		return IssuedAccessToken{}, InvalidCredentialsError{}
	}

	user, err := s.userRepo.GetByID(ctx, credential.UserID)
	if err != nil {
		return IssuedAccessToken{}, err
	}
	if user == nil {
		return IssuedAccessToken{}, UserNotFoundError{UserID: credential.UserID}
	}
	if user.Status == "suspended" {
		return IssuedAccessToken{}, SuspendedUserError{UserID: credential.UserID}
	}

	token, err := s.tokenService.Issue(credential.UserID, time.Now().UTC())
	if err != nil {
		return IssuedAccessToken{}, err
	}
	return IssuedAccessToken{
		Token:  token,
		UserID: credential.UserID,
	}, nil
}

func (s *Service) RefreshAccessToken(ctx context.Context, token string) (IssuedAccessToken, error) {
	user, err := s.AuthenticateUser(ctx, token)
	if err != nil {
		return IssuedAccessToken{}, err
	}
	refreshed, err := s.tokenService.Issue(user.ID, time.Now().UTC())
	if err != nil {
		return IssuedAccessToken{}, err
	}
	return IssuedAccessToken{
		Token:  refreshed,
		UserID: user.ID,
	}, nil
}

func (s *Service) AuthenticateUser(ctx context.Context, token string) (*data.User, error) {
	verified, err := s.tokenService.Verify(token)
	if err != nil {
		return nil, err
	}

	user, err := s.userRepo.GetByID(ctx, verified.UserID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, UserNotFoundError{UserID: verified.UserID}
	}

	if user.Status == "suspended" {
		return nil, SuspendedUserError{UserID: user.ID}
	}
	if verified.IssuedAt.Before(user.TokensInvalidBefore) {
		return nil, TokenInvalidError{message: "token revoked"}
	}
	return user, nil
}

func (s *Service) Logout(ctx context.Context, userID uuid.UUID, now time.Time) error {
	if userID == uuid.Nil {
		return errors.New("user_id must not be nil")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return s.userRepo.BumpTokensInvalidBefore(ctx, userID, now)
}
