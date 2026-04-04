package featureflag

import (
	"context"
	"strings"
)

const WorkEnabledKey = "work_enabled"

func IsWorkEnabled(ctx context.Context, svc *Service) bool {
	if svc == nil {
		return false
	}
	enabled, err := svc.IsGloballyEnabled(ctx, WorkEnabledKey)
	if err != nil {
		return false
	}
	return enabled
}

func SupportsOrgOverrides(flagKey string) bool {
	return strings.TrimSpace(flagKey) != WorkEnabledKey
}
