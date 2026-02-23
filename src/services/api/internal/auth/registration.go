package auth

import (
	"context"
	"errors"
	"fmt"
	"strconv"
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
	entitlementSvc EntitlementResolver
	now            func() time.Time
}

// EntitlementResolver 注册时读取 entitlement 默认值。
type EntitlementResolver interface {
	Resolve(ctx context.Context, orgID uuid.UUID, key string) (EntitlementValue, error)
}

// EntitlementValue 对 entitlement.EntitlementValue 的镜像，避免循环依赖。
type EntitlementValue struct {
	Raw  string
	Type string
}

func (v EntitlementValue) Int() int64 {
	n, _ := strconv.ParseInt(v.Raw, 10, 64)
	return n
}

func NewRegistrationService(
	pool *pgxpool.Pool,
	passwordHasher *BcryptPasswordHasher,
	tokenService *JwtAccessTokenService,
) (*RegistrationService, error) {
	if pool == nil {
		return nil, errors.New("pool must not be nil")
	}
	if passwordHasher == nil {
		return nil, errors.New("passwordHasher must not be nil")
	}
	if tokenService == nil {
		return nil, errors.New("tokenService must not be nil")
	}
	return &RegistrationService{
		pool:           pool,
		passwordHasher: passwordHasher,
		tokenService:   tokenService,
		now:            func() time.Time { return time.Now().UTC() },
	}, nil
}

// SetEntitlementResolver 设置 entitlement 解析器，用于注册时读取默认配额。
func (s *RegistrationService) SetEntitlementResolver(resolver EntitlementResolver) {
	s.entitlementSvc = resolver
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
	org, err := orgRepo.Create(ctx, fmt.Sprintf("user-%s", slugSuffix), fmt.Sprintf("%s's workspace", displayName))
	if err != nil {
		return RegisterResult{}, err
	}

	if _, err := membershipRepo.Create(ctx, org.ID, user.ID, "owner"); err != nil {
		return RegisterResult{}, err
	}

	// 初始化积分余额
	creditsRepo, err := data.NewCreditsRepository(tx)
	if err != nil {
		return RegisterResult{}, err
	}
	initialGrant := int64(1000)
	if s.entitlementSvc != nil {
		val, resolveErr := s.entitlementSvc.Resolve(ctx, org.ID, "credit.initial_grant")
		if resolveErr == nil {
			if v := val.Int(); v > 0 {
				initialGrant = v
			}
		}
	}
	if _, err := creditsRepo.InitBalance(ctx, org.ID, initialGrant); err != nil {
		return RegisterResult{}, err
	}

	// 自动为新用户生成邀请码
	inviteCodeRepo, err := data.NewInviteCodeRepository(tx)
	if err != nil {
		return RegisterResult{}, err
	}

	maxUses := 1
	if s.entitlementSvc != nil {
		val, resolveErr := s.entitlementSvc.Resolve(ctx, org.ID, "invite.default_max_uses")
		if resolveErr == nil {
			if v := val.Int(); v > 0 {
				maxUses = int(v)
			}
		}
	}

	code, err := data.GenerateCode()
	if err != nil {
		return RegisterResult{}, err
	}
	if _, err := inviteCodeRepo.Create(ctx, user.ID, code, maxUses); err != nil {
		return RegisterResult{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return RegisterResult{}, err
	}

	token, err := s.tokenService.Issue(user.ID, org.ID, s.now())
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
