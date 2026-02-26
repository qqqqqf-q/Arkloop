package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"arkloop/services/api/internal/data"

	"github.com/google/uuid"
)

const emailVerifyTokenTTL = 1 * time.Hour

type TokenAlreadyUsedOrExpiredError struct{}

func (TokenAlreadyUsedOrExpiredError) Error() string {
	return "verification token invalid or expired"
}

const settingAppBaseURL = "app.base_url"

// EmailVerifyService 管理邮箱验证 token 的生成与消耗。
type EmailVerifyService struct {
	tokenRepo    *data.EmailVerificationTokenRepository
	userRepo     *data.UserRepository
	jobRepo      *data.JobRepository
	envBaseURL   string
	settingsRepo *data.PlatformSettingsRepository
}

func NewEmailVerifyService(
	tokenRepo *data.EmailVerificationTokenRepository,
	userRepo *data.UserRepository,
	jobRepo *data.JobRepository,
) (*EmailVerifyService, error) {
	if tokenRepo == nil {
		return nil, errors.New("tokenRepo must not be nil")
	}
	if userRepo == nil {
		return nil, errors.New("userRepo must not be nil")
	}
	if jobRepo == nil {
		return nil, errors.New("jobRepo must not be nil")
	}
	return &EmailVerifyService{
		tokenRepo: tokenRepo,
		userRepo:  userRepo,
		jobRepo:   jobRepo,
	}, nil
}

// SetAppBaseURL 设置 AppBaseURL 的 env 兜底值和 DB 配置源（DB 优先）。
func (s *EmailVerifyService) SetAppBaseURL(envBaseURL string, repo *data.PlatformSettingsRepository) {
	s.envBaseURL = strings.TrimSpace(envBaseURL)
	s.settingsRepo = repo
}

// resolveBaseURL 发送时动态读取：DB 中 app.base_url 优先，env 兜底。
func (s *EmailVerifyService) resolveBaseURL(ctx context.Context) string {
	if s.settingsRepo != nil {
		if setting, err := s.settingsRepo.Get(ctx, settingAppBaseURL); err == nil && setting != nil {
			if v := strings.TrimSpace(setting.Value); v != "" {
				return strings.TrimRight(v, "/")
			}
		}
	}
	return strings.TrimRight(s.envBaseURL, "/")
}

// SendVerification 为用户生成验证 token 并入队发送邮件。
// 若用户没有邮箱则返回错误。
func (s *EmailVerifyService) SendVerification(ctx context.Context, userID uuid.UUID, username string) error {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return err
	}
	if user == nil {
		return fmt.Errorf("user not found")
	}
	if user.Email == nil || *user.Email == "" {
		return fmt.Errorf("user has no email address")
	}

	plaintext, tokenHash, err := generateVerifyToken()
	if err != nil {
		return fmt.Errorf("generate token: %w", err)
	}

	expiresAt := time.Now().UTC().Add(emailVerifyTokenTTL)
	if _, err := s.tokenRepo.Create(ctx, userID, tokenHash, expiresAt); err != nil {
		return fmt.Errorf("create verification token: %w", err)
	}

	baseURL := s.resolveBaseURL(ctx)

	locale := ""
	if user.Locale != nil {
		locale = *user.Locale
	}

	var subject, html, text string
	codeBlock := fmt.Sprintf("<strong style=\"font-size:28px;letter-spacing:6px\">%s</strong>", plaintext)

	if locale == "zh" {
		subject = "验证您的邮箱"
		if baseURL != "" {
			link := baseURL + "/verify?token=" + plaintext
			html = fmt.Sprintf(
				`<p>你好 %s，</p><p>您的邮箱验证码：</p><p>%s</p><p>或点击链接自动完成验证：<br><a href="%s">%s</a></p><p>有效期 1 小时。</p>`,
				username, codeBlock, link, link,
			)
			text = fmt.Sprintf("你好 %s，\n\n邮箱验证码：%s\n\n或点击链接验证：\n%s\n\n有效期 1 小时。", username, plaintext, link)
		} else {
			html = fmt.Sprintf(
				`<p>你好 %s，</p><p>您的邮箱验证码：</p><p>%s</p><p>有效期 1 小时。</p>`,
				username, codeBlock,
			)
			text = fmt.Sprintf("你好 %s，\n\n邮箱验证码：%s\n\n有效期 1 小时。", username, plaintext)
		}
	} else {
		subject = "Verify your email address"
		if baseURL != "" {
			link := baseURL + "/verify?token=" + plaintext
			html = fmt.Sprintf(
				`<p>Hi %s,</p><p>Your email verification code:</p><p>%s</p><p>Or click to verify automatically:<br><a href="%s">%s</a></p><p>Expires in 1 hour.</p>`,
				username, codeBlock, link, link,
			)
			text = fmt.Sprintf(
				"Hi %s,\n\nYour email verification code: %s\n\nOr verify automatically:\n%s\n\nExpires in 1 hour.",
				username, plaintext, link,
			)
		} else {
			html = fmt.Sprintf(
				`<p>Hi %s,</p><p>Your email verification code:</p><p>%s</p><p>Expires in 1 hour.</p>`,
				username, codeBlock,
			)
			text = fmt.Sprintf("Hi %s,\n\nYour email verification code: %s\n\nExpires in 1 hour.", username, plaintext)
		}
	}

	if _, err := s.jobRepo.EnqueueEmail(ctx, *user.Email, subject, html, text); err != nil {
		return fmt.Errorf("enqueue verification email: %w", err)
	}
	return nil
}

// ConfirmVerification 消耗 token 并将用户邮箱标记为已验证。
func (s *EmailVerifyService) ConfirmVerification(ctx context.Context, plaintext string) error {
	if plaintext == "" {
		return TokenAlreadyUsedOrExpiredError{}
	}

	tokenHash := hashVerifyToken(plaintext)
	userID, ok, err := s.tokenRepo.Consume(ctx, tokenHash)
	if err != nil {
		return err
	}
	if !ok {
		return TokenAlreadyUsedOrExpiredError{}
	}

	return s.userRepo.SetEmailVerified(ctx, userID)
}

func generateVerifyToken() (plaintext, hash string, err error) {
	var b [4]byte
	if _, err = rand.Read(b[:]); err != nil {
		return
	}
	// 100000–999999：均匀分布的6位数字
	n := binary.BigEndian.Uint32(b[:])%900000 + 100000
	plaintext = fmt.Sprintf("%06d", n)
	hash = hashVerifyToken(plaintext)
	return
}

func hashVerifyToken(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}
