package environmentref

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/google/uuid"
)

func BuildProfileRef(accountID uuid.UUID, userID *uuid.UUID) string {
	userKey := "system"
	if userID != nil && *userID != uuid.Nil {
		userKey = userID.String()
	}
	raw := "profile|" + accountID.String() + "|" + userKey
	sum := sha256.Sum256([]byte(raw))
	return "pref_" + hex.EncodeToString(sum[:16])
}
