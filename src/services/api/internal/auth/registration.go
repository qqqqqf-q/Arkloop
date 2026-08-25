package auth

import (
	"unicode"
)

const (
	minRegistrationPasswordBytes = 8
	maxRegistrationPasswordBytes = 72
	passwordPolicyMessage        = "password must be 8-72 characters and include letters and numbers"
)

type PasswordPolicyError struct{}

func (PasswordPolicyError) Error() string {
	return passwordPolicyMessage
}

// ValidateRegistrationPassword 校验本机 owner 密码策略，local-owner-password 端点使用。
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
