package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"arkloop/services/api/internal/data"

	"github.com/google/uuid"
)

const emailOTPTokenTTL = 1 * time.Hour

// OTPExpiredOrUsedError 表示登录 OTP 无效、已用或已过期。
type OTPExpiredOrUsedError struct{}

func (OTPExpiredOrUsedError) Error() string {
	return "otp invalid or expired"
}

// EmailOTPLoginService 处理邮箱 OTP 无密码登录。
type EmailOTPLoginService struct {
	userRepo         *data.UserRepository
	otpRepo          *data.EmailOTPTokenRepository
	jobRepo          *data.JobRepository
	tokenService     *JwtAccessTokenService
	refreshTokenRepo *data.RefreshTokenRepository
	membershipRepo   *data.OrgMembershipRepository
	settingsRepo     *data.PlatformSettingsRepository
	envBaseURL       string
}

func NewEmailOTPLoginService(
	userRepo *data.UserRepository,
	otpRepo *data.EmailOTPTokenRepository,
	jobRepo *data.JobRepository,
	tokenService *JwtAccessTokenService,
	refreshTokenRepo *data.RefreshTokenRepository,
	membershipRepo *data.OrgMembershipRepository,
) (*EmailOTPLoginService, error) {
	if userRepo == nil {
		return nil, errors.New("userRepo must not be nil")
	}
	if otpRepo == nil {
		return nil, errors.New("otpRepo must not be nil")
	}
	if jobRepo == nil {
		return nil, errors.New("jobRepo must not be nil")
	}
	if tokenService == nil {
		return nil, errors.New("tokenService must not be nil")
	}
	if refreshTokenRepo == nil {
		return nil, errors.New("refreshTokenRepo must not be nil")
	}
	if membershipRepo == nil {
		return nil, errors.New("membershipRepo must not be nil")
	}
	return &EmailOTPLoginService{
		userRepo:         userRepo,
		otpRepo:          otpRepo,
		jobRepo:          jobRepo,
		tokenService:     tokenService,
		refreshTokenRepo: refreshTokenRepo,
		membershipRepo:   membershipRepo,
	}, nil
}

func (s *EmailOTPLoginService) SetAppBaseURL(envBaseURL string, repo *data.PlatformSettingsRepository) {
	s.envBaseURL = strings.TrimSpace(envBaseURL)
	s.settingsRepo = repo
}

func (s *EmailOTPLoginService) RefreshTokenTTLSeconds() int {
	if s == nil || s.tokenService == nil {
		return 0
	}
	return s.tokenService.RefreshTokenTTLSeconds()
}

// SendLoginOTP 向指定邮箱发送登录 OTP。
// 若邮箱不存在则静默返回 nil（不暴露用户是否存在）。
func (s *EmailOTPLoginService) SendLoginOTP(ctx context.Context, email string) error {
	user, err := s.userRepo.GetByEmail(ctx, email)
	if err != nil {
		return fmt.Errorf("lookup user: %w", err)
	}
	if user == nil || user.Status != "active" {
		// 静默：不暴露账号是否存在
		return nil
	}

	plaintext, tokenHash, err := generateVerifyToken()
	if err != nil {
		return fmt.Errorf("generate otp: %w", err)
	}

	expiresAt := time.Now().UTC().Add(emailOTPTokenTTL)
	if _, err := s.otpRepo.Create(ctx, user.ID, tokenHash, expiresAt); err != nil {
		return fmt.Errorf("create otp token: %w", err)
	}

	locale := ""
	if user.Locale != nil {
		locale = *user.Locale
	}

	username := user.Username

	var subject, htmlBody, text string
	if locale == "zh" {
		subject = "登录验证码"
		htmlBody = buildEmailHTMLZh(emailParams{
			Title:    "登录验证码",
			Greeting: fmt.Sprintf("你好 %s，", username),
			Code:     plaintext,
			Notice:   "验证码有效期 1 小时，请勿泄露",
		})
		text = fmt.Sprintf("你好 %s，\n\n登录验证码：%s\n\n有效期 1 小时，请勿泄露。", username, plaintext)
	} else {
		subject = "Your login code"
		htmlBody = buildEmailHTML(emailParams{
			Title:    "Your login code",
			Greeting: fmt.Sprintf("Hi %s,", username),
			Code:     plaintext,
			Notice:   "Expires in 1 hour · Do not share this code",
		})
		text = fmt.Sprintf("Hi %s,\n\nYour login code: %s\n\nExpires in 1 hour. Do not share this code.", username, plaintext)
	}

	if _, err := s.jobRepo.EnqueueEmail(ctx, email, subject, htmlBody, text); err != nil {
		return fmt.Errorf("enqueue login otp email: %w", err)
	}
	return nil
}

// VerifyLoginOTP 验证 OTP 并签发 token 对。
// 若验证成功且邮箱未验证，同时标记邮箱已验证。
func (s *EmailOTPLoginService) VerifyLoginOTP(ctx context.Context, email string, code string) (IssuedTokenPair, error) {
	if code == "" {
		return IssuedTokenPair{}, OTPExpiredOrUsedError{}
	}

	user, err := s.userRepo.GetByEmail(ctx, email)
	if err != nil {
		return IssuedTokenPair{}, fmt.Errorf("lookup user: %w", err)
	}
	if user == nil {
		return IssuedTokenPair{}, OTPExpiredOrUsedError{}
	}
	if user.Status != "active" {
		return IssuedTokenPair{}, SuspendedUserError{UserID: user.ID, Status: user.Status}
	}

	tokenHash := hashVerifyToken(code)
	userID, ok, err := s.otpRepo.Consume(ctx, tokenHash)
	if err != nil {
		return IssuedTokenPair{}, err
	}
	if !ok || userID != user.ID {
		return IssuedTokenPair{}, OTPExpiredOrUsedError{}
	}

	// OTP 登录成功时顺带完成邮箱验证
	if user.EmailVerifiedAt == nil {
		if err := s.userRepo.SetEmailVerified(ctx, user.ID); err != nil {
			return IssuedTokenPair{}, fmt.Errorf("set email verified: %w", err)
		}
	}

	return s.issueTokenPair(ctx, user.ID)
}

func (s *EmailOTPLoginService) issueTokenPair(ctx context.Context, userID uuid.UUID) (IssuedTokenPair, error) {
	now := time.Now().UTC()

	var orgID uuid.UUID
	var orgRole string
	if membership, err := s.membershipRepo.GetDefaultForUser(ctx, userID); err == nil && membership != nil {
		orgID = membership.OrgID
		orgRole = membership.Role
	}

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
