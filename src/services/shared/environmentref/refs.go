package environmentref

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

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

func BuildWorkspaceRef(orgID uuid.UUID, profileRef string, bindingScope string, bindingTargetID uuid.UUID) string {
	parts := []string{
		"workspace",
		orgID.String(),
		strings.TrimSpace(profileRef),
		strings.TrimSpace(bindingScope),
		bindingTargetID.String(),
	}
	raw := strings.Join(parts, "|")
	sum := sha256.Sum256([]byte(raw))
	return "wsref_" + hex.EncodeToString(sum[:16])
}
