package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"arkloop/services/api/internal/data"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type LoginExistsError struct{}

func (LoginExistsError) Error() string {
	return "login exists"
}

type RegisterResult struct {
	UserID      uuid.UUID
	AccessToken string
}

type RegistrationService struct {
	pool           *pgxpool.Pool
	passwordHasher *BcryptPasswordHasher
	tokenService   *JwtAccessTokenService
	now            func() time.Time
}

func NewRegistrationService(
	pool *pgxpool.Pool,
	passwordHasher *BcryptPasswordHasher,
	tokenService *JwtAccessTokenService,
) (*RegistrationService, error) {
	if pool == nil {
		return nil, errors.New("pool 不能为空")
	}
	if passwordHasher == nil {
		return nil, errors.New("passwordHasher 不能为空")
	}
	if tokenService == nil {
		return nil, errors.New("tokenService 不能为空")
	}
	return &RegistrationService{
		pool:           pool,
		passwordHasher: passwordHasher,
		tokenService:   tokenService,
		now:            func() time.Time { return time.Now().UTC() },
	}, nil
}

func (s *RegistrationService) Register(
	ctx context.Context,
	login string,
	password string,
	displayName string,
) (RegisterResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return RegisterResult{}, err
	}
	defer tx.Rollback(ctx)

	credentialRepo, err := data.NewUserCredentialRepository(tx)
	if err != nil {
		return RegisterResult{}, err
	}
	userRepo, err := data.NewUserRepository(tx)
	if err != nil {
		return RegisterResult{}, err
	}
	orgRepo, err := data.NewOrgRepository(tx)
	if err != nil {
		return RegisterResult{}, err
	}
	membershipRepo, err := data.NewOrgMembershipRepository(tx)
	if err != nil {
		return RegisterResult{}, err
	}

	existing, err := credentialRepo.GetByLogin(ctx, login)
	if err != nil {
		return RegisterResult{}, err
	}
	if existing != nil {
		return RegisterResult{}, LoginExistsError{}
	}

	user, err := userRepo.Create(ctx, displayName)
	if err != nil {
		return RegisterResult{}, err
	}

	passwordHash, err := s.passwordHasher.HashPassword(password)
	if err != nil {
		return RegisterResult{}, err
	}

	_, err = credentialRepo.Create(ctx, user.ID, login, passwordHash)
	if err != nil {
		if isUniqueViolation(err, "uq_user_credentials_login") {
			return RegisterResult{}, LoginExistsError{}
		}
		return RegisterResult{}, err
	}

	slugSuffix := uuidHexPrefix(user.ID, 8)
	org, err := orgRepo.Create(ctx, fmt.Sprintf("user-%s", slugSuffix), fmt.Sprintf("%s 的空间", displayName))
	if err != nil {
		return RegisterResult{}, err
	}

	if _, err := membershipRepo.Create(ctx, org.ID, user.ID, "owner"); err != nil {
		return RegisterResult{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return RegisterResult{}, err
	}

	token, err := s.tokenService.Issue(user.ID, s.now())
	if err != nil {
		return RegisterResult{}, err
	}
	return RegisterResult{
		UserID:      user.ID,
		AccessToken: token,
	}, nil
}

func uuidHexPrefix(value uuid.UUID, n int) string {
	hex := strings.ReplaceAll(value.String(), "-", "")
	if n <= 0 || n > len(hex) {
		return hex
	}
	return hex[:n]
}

func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	if pgErr.Code != "23505" {
		return false
	}
	if constraint == "" {
		return true
	}
	return pgErr.ConstraintName == constraint
}
