package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"arkloop/services/api/internal/data"

	"github.com/google/uuid"
)

const emailVerifyTokenTTL = 24 * time.Hour

type TokenAlreadyUsedOrExpiredError struct{}

func (TokenAlreadyUsedOrExpiredError) Error() string {
	return "verification token invalid or expired"
}

// EmailVerifyService 管理邮箱验证 token 的生成与消耗。
type EmailVerifyService struct {
	tokenRepo *data.EmailVerificationTokenRepository
	userRepo  *data.UserRepository
	jobRepo   *data.JobRepository
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

	subject := "Verify your email address"
	html := fmt.Sprintf(
		`<p>Hi %s,</p><p>Your email verification token: <strong>%s</strong></p><p>This token expires in 24 hours.</p>`,
		username, plaintext,
	)
	text := fmt.Sprintf("Hi %s,\n\nYour email verification token: %s\n\nThis token expires in 24 hours.", username, plaintext)

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
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return
	}
	plaintext = base64.RawURLEncoding.EncodeToString(b)
	hash = hashVerifyToken(plaintext)
	return
}

func hashVerifyToken(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}
