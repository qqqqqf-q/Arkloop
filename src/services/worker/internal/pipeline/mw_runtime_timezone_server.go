//go:build !desktop

package pipeline

import (
	"context"
	"strings"
	"time"
)

func runtimeContextTimeZone(ctx context.Context, rc *RunContext) string {
	if rc == nil || rc.Pool == nil {
		return "UTC"
	}
	if rc.UserID != nil {
		var userTimeZone *string
		if err := rc.Pool.QueryRow(ctx, `SELECT timezone FROM users WHERE id = $1 LIMIT 1`, *rc.UserID).Scan(&userTimeZone); err == nil {
			if normalized := normalizeRuntimeTimeZone(userTimeZone); normalized != "" {
				return normalized
			}
		}
	}
	var accountTimeZone *string
	if err := rc.Pool.QueryRow(ctx, `SELECT timezone FROM accounts WHERE id = $1 LIMIT 1`, rc.Run.AccountID).Scan(&accountTimeZone); err == nil {
		if normalized := normalizeRuntimeTimeZone(accountTimeZone); normalized != "" {
			return normalized
		}
	}
	return "UTC"
}

func normalizeRuntimeTimeZone(value *string) string {
	if value == nil {
		return ""
	}
	cleaned := strings.TrimSpace(*value)
	if cleaned == "" {
		return ""
	}
	loc, err := time.LoadLocation(cleaned)
	if err != nil {
		return ""
	}
	return loc.String()
}
