package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"

	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/featureflag"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
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
	Status string // "suspended" | "deleted" | other non-active
}

func (e SuspendedUserError) Error() string {
	if e.Status == "deleted" {
		return "user deleted"
	}
	return "user suspended"
}

// EmailNotVerifiedError 表示用户邮箱尚未验证，且平台强制要求验证后才能登录。
type EmailNotVerifiedError struct {
	UserID uuid.UUID
}

func (e EmailNotVerifiedError) Error() string {
	return "email not verified"
}

// IssuedTokenPair 包含一次认证/刷新操作签发的 Access + Refresh Token 对。
type IssuedTokenPair struct {
	AccessToken  string
	RefreshToken string
	UserID       uuid.UUID
}

type Service struct {
	userRepo         *data.UserRepository
	credentialRepo   *data.UserCredentialRepository
	membershipRepo   *data.OrgMembershipRepository
	passwordHasher   *BcryptPasswordHasher
	tokenService     *JwtAccessTokenService
	refreshTokenRepo *data.RefreshTokenRepository
	flagService      *featureflag.Service
	redisClient      *redis.Client
}

func NewService(
	userRepo *data.UserRepository,
	credentialRepo *data.UserCredentialRepository,
	membershipRepo *data.OrgMembershipRepository,
	passwordHasher *BcryptPasswordHasher,
	tokenService *JwtAccessTokenService,
	refreshTokenRepo *data.RefreshTokenRepository,
	redisClient *redis.Client,
) (*Service, error) {
	if userRepo == nil {
		return nil, errors.New("userRepo must not be nil")
	}
	if credentialRepo == nil {
		return nil, errors.New("credentialRepo must not be nil")
	}
	if membershipRepo == nil {
		return nil, errors.New("membershipRepo must not be nil")
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
	return &Service{
		userRepo:         userRepo,
		credentialRepo:   credentialRepo,
		membershipRepo:   membershipRepo,
		passwordHasher:   passwordHasher,
		tokenService:     tokenService,
		refreshTokenRepo: refreshTokenRepo,
		redisClient:      redisClient,
	}, nil
}

// SetFlagService 注入 feature flag 服务，用于检查 auth.require_email_verification 开关。
func (s *Service) SetFlagService(svc *featureflag.Service) {
	s.flagService = svc
}

func (s *Service) RefreshTokenTTLSeconds() int {
	if s == nil || s.tokenService == nil {
		return 0
	}
	return s.tokenService.RefreshTokenTTLSeconds()
}

const (
	tokensInvalidBeforeRedisKeyPrefix = "arkloop:auth:tokens_invalid_before:"
	tokensInvalidBeforeRedisTimeout   = 50 * time.Millisecond
)

func (s *Service) IssueAccessToken(ctx context.Context, login string, password string) (IssuedTokenPair, error) {
	credential, err := s.credentialRepo.GetByLogin(ctx, login)
	if err != nil {
		return IssuedTokenPair{}, err
	}
	// 用户名查不到时，尝试用邮箱匹配（静默兜底）
	if credential == nil {
		credential, err = s.credentialRepo.GetByUserEmail(ctx, login)
		if err != nil {
			return IssuedTokenPair{}, err
		}
	}
	if credential == nil {
		return IssuedTokenPair{}, InvalidCredentialsError{}
	}
	if !s.passwordHasher.VerifyPassword(password, credential.PasswordHash) {
		return IssuedTokenPair{}, InvalidCredentialsError{}
	}

	user, err := s.userRepo.GetByID(ctx, credential.UserID)
	if err != nil {
		return IssuedTokenPair{}, err
	}
	if user == nil {
		return IssuedTokenPair{}, UserNotFoundError{UserID: credential.UserID}
	}
	if user.Status != "active" {
		return IssuedTokenPair{}, SuspendedUserError{UserID: credential.UserID, Status: user.Status}
	}

	// fail-close: 邮箱未验证时，flag 查询失败或 flagService 不可用一律拒绝登录
	if user.EmailVerifiedAt == nil {
		if s.flagService == nil {
			return IssuedTokenPair{}, EmailNotVerifiedError{UserID: user.ID}
		}
		required, err := s.flagService.IsGloballyEnabled(ctx, "auth.require_email_verification")
		if err != nil || required {
			return IssuedTokenPair{}, EmailNotVerifiedError{UserID: user.ID}
		}
	}

	return s.issueTokenPair(ctx, credential.UserID)
}

// ConsumeRefreshToken 验证并轮换 Refresh Token，返回新的 token 对。
// 轮换是原子的：旧 token 在同一 UPDATE 中被吊销并返回 user_id；若 token 无效则返回 TokenInvalidError。
func (s *Service) ConsumeRefreshToken(ctx context.Context, plaintext string) (IssuedTokenPair, error) {
	if plaintext == "" {
		return IssuedTokenPair{}, TokenInvalidError{message: "refresh token required"}
	}

	tokenHash := sha256RefreshToken(plaintext)

	userID, ok, err := s.refreshTokenRepo.ConsumeByHash(ctx, tokenHash)
	if err != nil {
		return IssuedTokenPair{}, err
	}
	if !ok {
		return IssuedTokenPair{}, TokenInvalidError{message: "refresh token invalid or expired"}
	}

	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return IssuedTokenPair{}, err
	}
	if user == nil {
		return IssuedTokenPair{}, UserNotFoundError{UserID: userID}
	}
	if user.Status != "active" {
		return IssuedTokenPair{}, SuspendedUserError{UserID: userID, Status: user.Status}
	}

	return s.issueTokenPair(ctx, userID)
}

// issueTokenPair 为指定用户签发 Access Token + Refresh Token，并将 Refresh Token 持久化到 DB。
func (s *Service) issueTokenPair(ctx context.Context, userID uuid.UUID) (IssuedTokenPair, error) {
	now := time.Now().UTC()
	orgID, orgRole := s.resolveDefaultOrg(ctx, userID)

	accessToken, err := s.tokenService.Issue(userID, orgID, orgRole, now)
	if err != nil {
		return IssuedTokenPair{}, err
	}

	plaintext, hash, expiresAt, err := s.tokenService.IssueRefreshToken(now)
	if err != nil {
		return IssuedTokenPair{}, err
	}

	if _, err = s.refreshTokenRepo.Create(ctx, userID, hash, expiresAt); err != nil {
		return IssuedTokenPair{}, err
	}

	return IssuedTokenPair{
		AccessToken:  accessToken,
		RefreshToken: plaintext,
		UserID:       userID,
	}, nil
}

// resolveDefaultOrg 查用户的默认 org；失败时静默返回 uuid.Nil，不阻断认证流程。
func (s *Service) resolveDefaultOrg(ctx context.Context, userID uuid.UUID) (orgID uuid.UUID, orgRole string) {
	membership, err := s.membershipRepo.GetDefaultForUser(ctx, userID)
	if err != nil || membership == nil {
		return uuid.Nil, ""
	}
	return membership.OrgID, membership.Role
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

	// tokens_invalid_before 是硬吊销开关，优先于 status。
	if verified.IssuedAt.Before(user.TokensInvalidBefore) {
		return nil, TokenInvalidError{message: "token revoked"}
	}
	if user.Status != "active" {
		return nil, SuspendedUserError{UserID: user.ID, Status: user.Status}
	}
	return user, nil
}

// VerifyAccessTokenForActor 仅用于 API 热路径的 Actor 解析：
// - JWT 验签 + exp/typ/iat 校验
// - tokens_invalid_before 吊销判断（Redis 优先，必要时回源 Postgres 并回填 Redis）
// 不做 user.Status 校验；封禁/删除等写路径必须 bump tokens_invalid_before。
func (s *Service) VerifyAccessTokenForActor(ctx context.Context, token string) (VerifiedAccessToken, error) {
	if s == nil || s.tokenService == nil {
		return VerifiedAccessToken{}, fmt.Errorf("tokenService not configured")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	verified, err := s.tokenService.Verify(token)
	if err != nil {
		return VerifiedAccessToken{}, err
	}

	tokensInvalidBefore, err := s.resolveTokensInvalidBefore(ctx, verified.UserID)
	if err != nil {
		return VerifiedAccessToken{}, err
	}
	if verified.IssuedAt.Before(tokensInvalidBefore) {
		return VerifiedAccessToken{}, TokenInvalidError{message: "token revoked"}
	}
	return verified, nil
}

func tokensInvalidBeforeRedisKey(userID uuid.UUID) string {
	return tokensInvalidBeforeRedisKeyPrefix + userID.String()
}

func (s *Service) refreshTokenTTL() time.Duration {
	ttlSeconds := s.RefreshTokenTTLSeconds()
	if ttlSeconds <= 0 {
		return 0
	}
	return time.Duration(ttlSeconds) * time.Second
}

func (s *Service) resolveTokensInvalidBefore(ctx context.Context, userID uuid.UUID) (time.Time, error) {
	if userID == uuid.Nil {
		return time.Time{}, fmt.Errorf("user_id must not be nil")
	}

	// 1) Redis 命中
	if s.redisClient != nil {
		redisCtx, cancel := context.WithTimeout(ctx, tokensInvalidBeforeRedisTimeout)
		raw, err := s.redisClient.Get(redisCtx, tokensInvalidBeforeRedisKey(userID)).Result()
		cancel()

		if err == nil {
			micros, parseErr := strconv.ParseInt(raw, 10, 64)
			if parseErr == nil {
				return time.UnixMicro(micros).UTC(), nil
			}
		} else if !errors.Is(err, redis.Nil) {
			// Redis 错误时 fail-open：回源 Postgres（由 DB 决定最终一致性）
		}
	}

	// 2) 回源 Postgres
	if s.userRepo == nil {
		return time.Time{}, fmt.Errorf("userRepo not configured")
	}
	val, ok, err := s.userRepo.GetTokensInvalidBefore(ctx, userID)
	if err != nil {
		return time.Time{}, err
	}
	if !ok {
		return time.Time{}, UserNotFoundError{UserID: userID}
	}

	// 3) 回填 Redis（best-effort）
	if s.redisClient != nil {
		ttl := s.refreshTokenTTL()
		if ttl > 0 {
			microAligned := val.UTC().Truncate(time.Microsecond)
			payload := strconv.FormatInt(microAligned.UnixMicro(), 10)

			redisCtx, cancel := context.WithTimeout(ctx, tokensInvalidBeforeRedisTimeout)
			_ = s.redisClient.Set(redisCtx, tokensInvalidBeforeRedisKey(userID), payload, ttl).Err()
			cancel()
		}
	}

	return val.UTC(), nil
}

// BumpTokensInvalidBefore 强制吊销指定用户的所有 access token（以及未来 refresh 生成的 access token）。
// DB 是唯一真相，Redis 仅作缓存，写 Redis 失败不会影响主流程。
func (s *Service) BumpTokensInvalidBefore(ctx context.Context, userID uuid.UUID, now time.Time) error {
	if s == nil || s.userRepo == nil {
		return fmt.Errorf("userRepo not configured")
	}
	if userID == uuid.Nil {
		return fmt.Errorf("user_id must not be nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC().Truncate(time.Microsecond)

	if err := s.userRepo.BumpTokensInvalidBefore(ctx, userID, now); err != nil {
		return err
	}

	if s.redisClient != nil {
		ttl := s.refreshTokenTTL()
		if ttl > 0 {
			payload := strconv.FormatInt(now.UnixMicro(), 10)
			redisCtx, cancel := context.WithTimeout(ctx, tokensInvalidBeforeRedisTimeout)
			_ = s.redisClient.Set(redisCtx, tokensInvalidBeforeRedisKey(userID), payload, ttl).Err()
			cancel()
		}
	}

	return nil
}

func (s *Service) Logout(ctx context.Context, userID uuid.UUID, now time.Time) error {
	if userID == uuid.Nil {
		return errors.New("user_id must not be nil")
	}
	if err := s.refreshTokenRepo.RevokeAllForUser(ctx, userID); err != nil {
		return err
	}
	return s.BumpTokensInvalidBefore(ctx, userID, now)
}

func sha256RefreshToken(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}
