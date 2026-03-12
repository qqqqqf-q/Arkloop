package featureflag

import (
	"context"
	"strings"
)

const ClawEnabledKey = "claw_enabled"

func IsClawEnabled(ctx context.Context, svc *Service) bool {
	if svc == nil {
		return false
	}
	enabled, err := svc.IsGloballyEnabled(ctx, ClawEnabledKey)
	if err != nil {
		return false
	}
	return enabled
}

func SupportsOrgOverrides(flagKey string) bool {
	return strings.TrimSpace(flagKey) != ClawEnabledKey
}
