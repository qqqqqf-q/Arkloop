package auth

import (
	"fmt"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

const defaultBcryptRounds = 12

type BcryptPasswordHasher struct {
	rounds int
}

func NewBcryptPasswordHasher(rounds int) (*BcryptPasswordHasher, error) {
	if rounds == 0 {
		rounds = defaultBcryptRounds
	}
	if rounds < 4 || rounds > 31 {
		return nil, fmt.Errorf("bcrypt rounds 必须在 [4, 31] 范围内")
	}
	return &BcryptPasswordHasher{rounds: rounds}, nil
}

func (h *BcryptPasswordHasher) HashPassword(password string) (string, error) {
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), h.rounds)
	if err != nil {
		return "", err
	}
	return string(hashed), nil
}

func (h *BcryptPasswordHasher) VerifyPassword(password string, passwordHash string) bool {
	if strings.TrimSpace(passwordHash) == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password)) == nil
}
