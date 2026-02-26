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

type InviteCodeInvalidError struct {
	Reason string
}

func (e InviteCodeInvalidError) Error() string {
	return e.Reason
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
	org, err := orgRepo.Create(ctx, fmt.Sprintf("personal-%s", slugSuffix), fmt.Sprintf("%s's workspace", login), "personal")
	if err != nil {
		return RegisterResult{}, err
	}

	if _, err := membershipRepo.Create(ctx, org.ID, user.ID, "owner"); err != nil {
		return RegisterResult{}, err
	}

	notifRepo, err := data.NewNotificationsRepository(tx)
	if err != nil {
		return RegisterResult{}, err
	}
	if _, err := notifRepo.BackfillBroadcastsForMembership(ctx, user.ID, org.ID); err != nil {
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

	// 处理邀请码推荐关系
	var result RegisterResult
	inviteCode = strings.TrimSpace(inviteCode)
	if inviteCode != "" {
		existingCode, err := inviteCodeRepo.GetByCode(ctx, inviteCode)
		if err != nil {
			return RegisterResult{}, err
		}

		codeValid := existingCode != nil && existingCode.IsActive && existingCode.UseCount < existingCode.MaxUses
		if codeValid {
			referralRepo, err := data.NewReferralRepository(tx)
			if err != nil {
				return RegisterResult{}, err
			}

			// 增加使用次数
			if _, err := inviteCodeRepo.IncrementUseCount(ctx, existingCode.ID); err != nil {
				return RegisterResult{}, err
			}

			// 创建推荐关系
			referral, err := referralRepo.Create(ctx, existingCode.UserID, user.ID, existingCode.ID)
			if err != nil {
				return RegisterResult{}, err
			}

			// 查找邀请人的默认 org
			inviterMembership, err := membershipRepo.GetDefaultForUser(ctx, existingCode.UserID)
			if err == nil && inviterMembership != nil {
				// 推荐奖励积分
				referralReward := int64(100)
				if s.entitlementSvc != nil {
					val, resolveErr := s.entitlementSvc.Resolve(ctx, inviterMembership.OrgID, "credit.invite_reward")
					if resolveErr == nil {
						if v := val.Int(); v > 0 {
							referralReward = v
						}
					}
				}

				refType := "referral"
				if err := creditsRepo.Add(ctx, inviterMembership.OrgID, referralReward, "referral_reward", &refType, &referral.ID, nil); err != nil {
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
				val, resolveErr := s.entitlementSvc.Resolve(ctx, org.ID, "credit.invitee_reward")
				if resolveErr == nil {
					if v := val.Int(); v > 0 {
						inviteeReward = v
					}
				}
			}
			if inviteeReward > 0 {
				refType := "referral"
				if err := creditsRepo.Add(ctx, org.ID, inviteeReward, "invitee_reward", &refType, &referral.ID, nil); err != nil {
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

	if err := tx.Commit(ctx); err != nil {
		return RegisterResult{}, err
	}

	now := s.now()
	token, err := s.tokenService.Issue(user.ID, org.ID, now)
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
