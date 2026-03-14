package plugin

import (
	"context"

	"github.com/google/uuid"
)

type BillingProvider interface {
	CreateSubscription(ctx context.Context, orgID uuid.UUID, planID string) error
	CancelSubscription(ctx context.Context, orgID uuid.UUID) error
	GetActiveSubscription(ctx context.Context, orgID uuid.UUID) (*Subscription, error)
	ReportUsage(ctx context.Context, orgID uuid.UUID, usage UsageRecord) error
	CheckQuota(ctx context.Context, orgID uuid.UUID, resource string) (allowed bool, err error)
	HandleWebhook(ctx context.Context, provider string, payload []byte, signature string) error
}

type Subscription struct {
	ID     string
	AccountID  uuid.UUID
	PlanID string
	Status string
}

type UsageRecord struct {
	RunID      uuid.UUID
	TokensIn   int64
	TokensOut  int64
	ToolCalls  int
	DurationMs int64
}
