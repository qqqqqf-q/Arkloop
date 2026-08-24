package featureflag

import (
	"context"
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
