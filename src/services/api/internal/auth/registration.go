package auth

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

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

type InviteCodeInvalidError struct {
	Reason string
}

func (e InviteCodeInvalidError) Error() string {
	return e.Reason
}

const (
	minRegistrationPasswordBytes = 8
	maxRegistrationPasswordBytes = 72
	passwordPolicyMessage        = "password must be 8-72 characters and include letters and numbers"
)

type PasswordPolicyError struct{}

func (PasswordPolicyError) Error() string {
	return passwordPolicyMessage
}

func ValidateRegistrationPassword(password string) error {
	if len(password) < minRegistrationPasswordBytes || len(password) > maxRegistrationPasswordBytes {
		return PasswordPolicyError{}
	}

	hasLetter := false
	hasDigit := false
	for _, char := range password {
		if unicode.IsLetter(char) {
			hasLetter = true
		}
		if unicode.IsDigit(char) {
			hasDigit = true
		}
		if hasLetter && hasDigit {
			return nil
		}
	}

	return PasswordPolicyError{}
}

type RegisterResult struct {
	UserID        uuid.UUID
	AccessToken   string
	RefreshToken  string
	Warning       string
	ReferralID    *uuid.UUID
	InviterUserID uuid.UUID
	InviteCodeID  uuid.UUID
}

type createdLocalAccount struct {
	User    data.User
	Account data.Account
}

type BootstrapAlreadyInitializedError struct{}

func (BootstrapAlreadyInitializedError) Error() string {
	return "bootstrap already initialized"
}

type BootstrapInvalidTokenError struct{}

func (BootstrapInvalidTokenError) Error() string {
	return "invalid bootstrap token"
}

type BootstrapInitResult struct {
	Token     string
	ExpiresAt time.Time
}

type BootstrapVerifyResult struct {
	Valid     bool
	ExpiresAt time.Time
}

type BootstrapSetupResult struct {
	UserID       uuid.UUID
	AccessToken  string
	RefreshToken string
}

const (
	bootstrapTokenSettingKey          = "bootstrap.init.token"
	bootstrapTokenExpiresAtSettingKey = "bootstrap.init.expires_at"
	bootstrapPlatformAdminSettingKey  = "bootstrap.platform_admin.user_id"
	bootstrapTokenTTL                 = 30 * time.Minute
)

type RegistrationService struct {
	pool             *pgxpool.Pool
	passwordHasher   *BcryptPasswordHasher
	tokenService     *JwtAccessTokenService
	refreshTokenRepo *data.RefreshTokenRepository
	jobRepo          *data.JobRepository
	entitlementSvc   EntitlementResolver
	emailVerifySvc   *EmailVerifyService
	now              func() time.Time
}

// EntitlementResolver 注册时读取 entitlement 默认值。
type EntitlementResolver interface {
	Resolve(ctx context.Context, accountID uuid.UUID, key string) (EntitlementValue, error)
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
	refreshTokenRepo *data.RefreshTokenRepository,
	jobRepo *data.JobRepository,
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
	if refreshTokenRepo == nil {
		return nil, errors.New("refreshTokenRepo must not be nil")
	}
	return &RegistrationService{
		pool:             pool,
		passwordHasher:   passwordHasher,
		tokenService:     tokenService,
		refreshTokenRepo: refreshTokenRepo,
		jobRepo:          jobRepo,
		now:              func() time.Time { return time.Now().UTC() },
	}, nil
}

// SetEntitlementResolver 设置 entitlement 解析器，用于注册时读取默认配额。
func (s *RegistrationService) SetEntitlementResolver(resolver EntitlementResolver) {
	s.entitlementSvc = resolver
}

// SetEmailVerifyService 设置邮箱验证服务，注册完成后自动发送验证邮件。
func (s *RegistrationService) SetEmailVerifyService(svc *EmailVerifyService) {
	s.emailVerifySvc = svc
}

func (s *RegistrationService) RefreshTokenTTLSeconds() int {
	if s == nil || s.tokenService == nil {
		return 0
	}
	return s.tokenService.RefreshTokenTTLSeconds()
}

func (s *RegistrationService) Register(
	ctx context.Context,
	login string,
	password string,
	email string,
	locale string,
	inviteCode string,
	requireValidCode bool,
) (RegisterResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ValidateRegistrationPassword(password); err != nil {
		return RegisterResult{}, err
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return RegisterResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	credentialRepo, err := data.NewUserCredentialRepository(tx)
	if err != nil {
		return RegisterResult{}, err
	}
	userRepo, err := data.NewUserRepository(tx)
	if err != nil {
		return RegisterResult{}, err
	}
	accountRepo, err := data.NewAccountRepository(tx)
	if err != nil {
		return RegisterResult{}, err
	}
	membershipRepo, err := data.NewAccountMembershipRepository(tx)
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

	user, err := userRepo.Create(ctx, login, email, locale)
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
	account, err := accountRepo.Create(ctx, fmt.Sprintf("personal-%s", slugSuffix), fmt.Sprintf("%s's workspace", login), "personal")
	if err != nil {
		return RegisterResult{}, err
	}

	if _, err := membershipRepo.Create(ctx, account.ID, user.ID, RoleAccountAdmin); err != nil {
		return RegisterResult{}, err
	}

	notifRepo, err := data.NewNotificationsRepository(tx)
	if err != nil {
		return RegisterResult{}, err
	}
	if _, err := notifRepo.BackfillBroadcastsForMembership(ctx, user.ID, account.ID); err != nil {
		return RegisterResult{}, err
	}

	creditsRepo, err := data.NewCreditsRepository(tx)
	if err != nil {
		return RegisterResult{}, err
	}
	initialGrant := int64(1000)
	if s.entitlementSvc != nil {
		val, resolveErr := s.entitlementSvc.Resolve(ctx, account.ID, "credit.initial_grant")
		if resolveErr == nil {
			if v := val.Int(); v > 0 {
				initialGrant = v
			}
		}
	}
	if _, err := creditsRepo.InitBalance(ctx, account.ID, initialGrant); err != nil {
		return RegisterResult{}, err
	}

	inviteCodeRepo, err := data.NewInviteCodeRepository(tx)
	if err != nil {
		return RegisterResult{}, err
	}

	maxUses := 0
	if s.entitlementSvc != nil {
		val, resolveErr := s.entitlementSvc.Resolve(ctx, account.ID, "invite.default_max_uses")
		if resolveErr == nil {
			maxUses = data.NormalizeInviteCodeMaxUses(val.Int())
		}
	}

	code, err := data.GenerateCode()
	if err != nil {
		return RegisterResult{}, err
	}
	if _, err := inviteCodeRepo.Create(ctx, user.ID, code, maxUses); err != nil {
		return RegisterResult{}, err
	}

	var result RegisterResult
	inviteCode = strings.TrimSpace(inviteCode)
	if inviteCode != "" {
		existingCode, err := inviteCodeRepo.GetByCode(ctx, inviteCode)
		if err != nil {
			return RegisterResult{}, err
		}

		codeValid := existingCode != nil && existingCode.IsActive && data.InviteCodeHasRemainingUses(existingCode.UseCount, existingCode.MaxUses)
		if codeValid {
			referralRepo, err := data.NewReferralRepository(tx)
			if err != nil {
				return RegisterResult{}, err
			}

			if _, err := inviteCodeRepo.IncrementUseCount(ctx, existingCode.ID); err != nil {
				return RegisterResult{}, err
			}

			referral, err := referralRepo.Create(ctx, existingCode.UserID, user.ID, existingCode.ID)
			if err != nil {
				return RegisterResult{}, err
			}

			inviterMembership, err := membershipRepo.GetDefaultForUser(ctx, existingCode.UserID)
			if err == nil && inviterMembership != nil {
				referralReward := int64(100)
				if s.entitlementSvc != nil {
					val, resolveErr := s.entitlementSvc.Resolve(ctx, inviterMembership.AccountID, "credit.invite_reward")
					if resolveErr == nil {
						if v := val.Int(); v > 0 {
							referralReward = v
						}
					}
				}

				refType := "referral"
				if err := creditsRepo.Add(ctx, inviterMembership.AccountID, referralReward, "referral_reward", &refType, &referral.ID, nil); err != nil {
					return RegisterResult{}, err
				}

				if err := referralRepo.MarkCredited(ctx, referral.ID); err != nil {
					return RegisterResult{}, err
				}
			}

			result.ReferralID = &referral.ID
			result.InviterUserID = existingCode.UserID
			result.InviteCodeID = existingCode.ID

			// 被邀请人奖励
			inviteeReward := int64(0)
			if s.entitlementSvc != nil {
				val, resolveErr := s.entitlementSvc.Resolve(ctx, account.ID, "credit.invitee_reward")
				if resolveErr == nil {
					if v := val.Int(); v > 0 {
						inviteeReward = v
					}
				}
			}
			if inviteeReward > 0 {
				refType := "referral"
				if err := creditsRepo.Add(ctx, account.ID, inviteeReward, "invitee_reward", &refType, &referral.ID, nil); err != nil {
					return RegisterResult{}, err
				}
			}
		} else {
			if requireValidCode {
				return RegisterResult{}, InviteCodeInvalidError{Reason: "invite code is invalid or exhausted"}
			}
			result.Warning = "invalid invite code, skipped referral"
		}
	}

	// 注册时创建 Default Project
	projectRepo, err := data.NewProjectRepository(tx)
	if err != nil {
		return RegisterResult{}, err
	}
	if _, err := projectRepo.CreateDefaultForOwner(ctx, account.ID, user.ID); err != nil {
		return RegisterResult{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return RegisterResult{}, err
	}

	now := s.now()
	token, err := s.tokenService.Issue(user.ID, account.ID, RoleAccountAdmin, now)
	if err != nil {
		return RegisterResult{}, err
	}

	plaintext, hash, expiresAt, err := s.tokenService.IssueRefreshToken(now)
	if err != nil {
		return RegisterResult{}, err
	}
	if _, err = s.refreshTokenRepo.Create(ctx, user.ID, hash, expiresAt); err != nil {
		return RegisterResult{}, err
	}

	result.UserID = user.ID
	result.AccessToken = token
	result.RefreshToken = plaintext

	// 注册完成后异步发送验证邮件；失败不阻断注册流程。
	if email != "" && s.emailVerifySvc != nil {
		if enqErr := s.emailVerifySvc.SendVerification(ctx, user.ID, login); enqErr != nil {
			result.Warning = appendWarning(result.Warning, "verification email could not be queued")
		}
	}

	return result, nil
}

func (s *RegistrationService) createLocalAccountTx(
	ctx context.Context,
	tx pgx.Tx,
	login string,
	password string,
	email string,
	locale string,
) (createdLocalAccount, error) {
	credentialRepo, err := data.NewUserCredentialRepository(tx)
	if err != nil {
		return createdLocalAccount{}, err
	}
	userRepo, err := data.NewUserRepository(tx)
	if err != nil {
		return createdLocalAccount{}, err
	}
	accountRepo, err := data.NewAccountRepository(tx)
	if err != nil {
		return createdLocalAccount{}, err
	}
	membershipRepo, err := data.NewAccountMembershipRepository(tx)
	if err != nil {
		return createdLocalAccount{}, err
	}

	existing, err := credentialRepo.GetByLogin(ctx, login)
	if err != nil {
		return createdLocalAccount{}, err
	}
	if existing != nil {
		return createdLocalAccount{}, LoginExistsError{}
	}

	user, err := userRepo.Create(ctx, login, email, locale)
	if err != nil {
		return createdLocalAccount{}, err
	}

	passwordHash, err := s.passwordHasher.HashPassword(password)
	if err != nil {
		return createdLocalAccount{}, err
	}

	_, err = credentialRepo.Create(ctx, user.ID, login, passwordHash)
	if err != nil {
		if isUniqueViolation(err, "uq_user_credentials_login") {
			return createdLocalAccount{}, LoginExistsError{}
		}
		return createdLocalAccount{}, err
	}

	slugSuffix := uuidHexPrefix(user.ID, 8)
	account, err := accountRepo.Create(ctx, fmt.Sprintf("personal-%s", slugSuffix), fmt.Sprintf("%s's workspace", login), "personal")
	if err != nil {
		return createdLocalAccount{}, err
	}

	if _, err := membershipRepo.Create(ctx, account.ID, user.ID, RoleAccountAdmin); err != nil {
		return createdLocalAccount{}, err
	}

	notifRepo, err := data.NewNotificationsRepository(tx)
	if err != nil {
		return createdLocalAccount{}, err
	}
	if _, err := notifRepo.BackfillBroadcastsForMembership(ctx, user.ID, account.ID); err != nil {
		return createdLocalAccount{}, err
	}

	creditsRepo, err := data.NewCreditsRepository(tx)
	if err != nil {
		return createdLocalAccount{}, err
	}
	initialGrant := int64(1000)
	if s.entitlementSvc != nil {
		val, resolveErr := s.entitlementSvc.Resolve(ctx, account.ID, "credit.initial_grant")
		if resolveErr == nil {
			if v := val.Int(); v > 0 {
				initialGrant = v
			}
		}
	}
	if _, err := creditsRepo.InitBalance(ctx, account.ID, initialGrant); err != nil {
		return createdLocalAccount{}, err
	}

	inviteCodeRepo, err := data.NewInviteCodeRepository(tx)
	if err != nil {
		return createdLocalAccount{}, err
	}
	maxUses := 0
	if s.entitlementSvc != nil {
		val, resolveErr := s.entitlementSvc.Resolve(ctx, account.ID, "invite.default_max_uses")
		if resolveErr == nil {
			maxUses = data.NormalizeInviteCodeMaxUses(val.Int())
		}
	}
	code, err := data.GenerateCode()
	if err != nil {
		return createdLocalAccount{}, err
	}
	if _, err := inviteCodeRepo.Create(ctx, user.ID, code, maxUses); err != nil {
		return createdLocalAccount{}, err
	}

	return createdLocalAccount{User: user, Account: account}, nil
}

func (s *RegistrationService) InitBootstrapToken(ctx context.Context) (BootstrapInitResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if s == nil || s.pool == nil {
		return BootstrapInitResult{}, fmt.Errorf("registration service not configured")
	}

	membershipRepo, err := data.NewAccountMembershipRepository(s.pool)
	if err != nil {
		return BootstrapInitResult{}, err
	}
	hasAdmin, err := membershipRepo.HasPlatformAdmin(ctx)
	if err != nil {
		return BootstrapInitResult{}, err
	}
	if hasAdmin {
		return BootstrapInitResult{}, BootstrapAlreadyInitializedError{}
	}

	settingsRepo, err := data.NewPlatformSettingsRepository(s.pool)
	if err != nil {
		return BootstrapInitResult{}, err
	}

	token := uuid.NewString() + uuid.NewString()
	expiresAt := s.now().Add(bootstrapTokenTTL)
	if _, err := settingsRepo.Set(ctx, bootstrapTokenSettingKey, token); err != nil {
		return BootstrapInitResult{}, err
	}
	if _, err := settingsRepo.Set(ctx, bootstrapTokenExpiresAtSettingKey, expiresAt.UTC().Format(time.RFC3339Nano)); err != nil {
		return BootstrapInitResult{}, err
	}

	return BootstrapInitResult{Token: token, ExpiresAt: expiresAt.UTC()}, nil
}

func (s *RegistrationService) VerifyBootstrapToken(ctx context.Context, token string) (BootstrapVerifyResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if s == nil || s.pool == nil {
		return BootstrapVerifyResult{}, fmt.Errorf("registration service not configured")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return BootstrapVerifyResult{Valid: false}, nil
	}

	membershipRepo, err := data.NewAccountMembershipRepository(s.pool)
	if err != nil {
		return BootstrapVerifyResult{}, err
	}
	hasAdmin, err := membershipRepo.HasPlatformAdmin(ctx)
	if err != nil {
		return BootstrapVerifyResult{}, err
	}
	if hasAdmin {
		return BootstrapVerifyResult{Valid: false}, nil
	}

	settingsRepo, err := data.NewPlatformSettingsRepository(s.pool)
	if err != nil {
		return BootstrapVerifyResult{}, err
	}
	storedToken, err := settingsRepo.Get(ctx, bootstrapTokenSettingKey)
	if err != nil {
		return BootstrapVerifyResult{}, err
	}
	expiresSetting, err := settingsRepo.Get(ctx, bootstrapTokenExpiresAtSettingKey)
	if err != nil {
		return BootstrapVerifyResult{}, err
	}
	if storedToken == nil || expiresSetting == nil || strings.TrimSpace(storedToken.Value) != token {
		return BootstrapVerifyResult{Valid: false}, nil
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(expiresSetting.Value))
	if err != nil {
		return BootstrapVerifyResult{Valid: false}, nil
	}
	if !expiresAt.After(s.now()) {
		return BootstrapVerifyResult{Valid: false, ExpiresAt: expiresAt.UTC()}, nil
	}
	return BootstrapVerifyResult{Valid: true, ExpiresAt: expiresAt.UTC()}, nil
}

func (s *RegistrationService) SetupBootstrapAdmin(ctx context.Context, token string, login string, password string, email string, locale string) (BootstrapSetupResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if s == nil || s.pool == nil {
		return BootstrapSetupResult{}, fmt.Errorf("registration service not configured")
	}
	if err := ValidateRegistrationPassword(password); err != nil {
		return BootstrapSetupResult{}, err
	}
	verify, err := s.VerifyBootstrapToken(ctx, token)
	if err != nil {
		return BootstrapSetupResult{}, err
	}
	if !verify.Valid {
		return BootstrapSetupResult{}, BootstrapInvalidTokenError{}
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return BootstrapSetupResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	settingsRepo, err := data.NewPlatformSettingsRepository(tx)
	if err != nil {
		return BootstrapSetupResult{}, err
	}
	membershipRepo, err := data.NewAccountMembershipRepository(tx)
	if err != nil {
		return BootstrapSetupResult{}, err
	}
	hasAdmin, err := membershipRepo.HasPlatformAdmin(ctx)
	if err != nil {
		return BootstrapSetupResult{}, err
	}
	if hasAdmin {
		return BootstrapSetupResult{}, BootstrapAlreadyInitializedError{}
	}

	created, err := s.createLocalAccountTx(ctx, tx, login, password, email, locale)
	if err != nil {
		return BootstrapSetupResult{}, err
	}
	projectRepo, err := data.NewProjectRepository(tx)
	if err != nil {
		return BootstrapSetupResult{}, err
	}
	if _, err := projectRepo.CreateDefaultForOwner(ctx, created.Account.ID, created.User.ID); err != nil {
		return BootstrapSetupResult{}, err
	}
	if err := membershipRepo.SetRoleForUser(ctx, created.User.ID, RolePlatformAdmin); err != nil {
		return BootstrapSetupResult{}, err
	}
	if _, err := settingsRepo.Set(ctx, bootstrapPlatformAdminSettingKey, created.User.ID.String()); err != nil {
		return BootstrapSetupResult{}, err
	}
	if err := settingsRepo.Delete(ctx, bootstrapTokenSettingKey); err != nil {
		return BootstrapSetupResult{}, err
	}
	if err := settingsRepo.Delete(ctx, bootstrapTokenExpiresAtSettingKey); err != nil {
		return BootstrapSetupResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return BootstrapSetupResult{}, err
	}

	now := s.now()
	accessToken, err := s.tokenService.Issue(created.User.ID, created.Account.ID, RolePlatformAdmin, now)
	if err != nil {
		return BootstrapSetupResult{}, err
	}
	refreshPlain, refreshHash, expiresAt, err := s.tokenService.IssueRefreshToken(now)
	if err != nil {
		return BootstrapSetupResult{}, err
	}
	if _, err := s.refreshTokenRepo.Create(ctx, created.User.ID, refreshHash, expiresAt); err != nil {
		return BootstrapSetupResult{}, err
	}
	return BootstrapSetupResult{UserID: created.User.ID, AccessToken: accessToken, RefreshToken: refreshPlain}, nil
}

func uuidHexPrefix(value uuid.UUID, n int) string {
	hex := strings.ReplaceAll(value.String(), "-", "")
	if n <= 0 || n > len(hex) {
		return hex
	}
	return hex[:n]
}

func appendWarning(existing, msg string) string {
	if existing == "" {
		return msg
	}
	return existing + "; " + msg
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
