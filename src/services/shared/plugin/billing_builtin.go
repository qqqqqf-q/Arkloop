package plugin

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// ErrWebhookNotSupported OSS 默认实现不处理外部支付 webhook。
var ErrWebhookNotSupported = errors.New("webhook handling not supported in OSS billing")

// CreditOps 积分操作抽象。
type CreditOps interface {
	GetBalance(ctx context.Context, accountID uuid.UUID) (int64, error)
	Deduct(ctx context.Context, accountID uuid.UUID, amount int64, txType string, refID uuid.UUID, metadata map[string]any) error
}

// SubscriptionOps 订阅操作抽象。
type SubscriptionOps interface {
	Create(ctx context.Context, accountID uuid.UUID, planID string) error
	Cancel(ctx context.Context, accountID uuid.UUID) error
	GetActive(ctx context.Context, accountID uuid.UUID) (*Subscription, error)
}

// QuotaOps 配额检查抽象。
type QuotaOps interface {
	Check(ctx context.Context, accountID uuid.UUID, resource string) (bool, error)
}

// CreditCalculator 将用量转换为积分扣减额。
type CreditCalculator interface {
	Calculate(ctx context.Context, accountID uuid.UUID, usage UsageRecord) (int64, error)
}

// BuiltinBillingProvider OSS 默认计费实现。
// 通过依赖注入包装现有的积分/订阅/配额逻辑。
type BuiltinBillingProvider struct {
	credits    CreditOps
	subs       SubscriptionOps
	quotas     QuotaOps
	calculator CreditCalculator
}

// NewBuiltinBillingProvider 创建 OSS 默认计费提供者。
// 所有依赖必须非 nil。
func NewBuiltinBillingProvider(credits CreditOps, subs SubscriptionOps, quotas QuotaOps, calc CreditCalculator) (*BuiltinBillingProvider, error) {
	if credits == nil {
		return nil, errors.New("credits must not be nil")
	}
	if subs == nil {
		return nil, errors.New("subs must not be nil")
	}
	if quotas == nil {
		return nil, errors.New("quotas must not be nil")
	}
	if calc == nil {
		return nil, errors.New("calculator must not be nil")
	}
	return &BuiltinBillingProvider{
		credits:    credits,
		subs:       subs,
		quotas:     quotas,
		calculator: calc,
	}, nil
}

func (p *BuiltinBillingProvider) CreateSubscription(ctx context.Context, accountID uuid.UUID, planID string) error {
	return p.subs.Create(ctx, accountID, planID)
}

func (p *BuiltinBillingProvider) CancelSubscription(ctx context.Context, accountID uuid.UUID) error {
	return p.subs.Cancel(ctx, accountID)
}

func (p *BuiltinBillingProvider) GetActiveSubscription(ctx context.Context, accountID uuid.UUID) (*Subscription, error) {
	return p.subs.GetActive(ctx, accountID)
}

func (p *BuiltinBillingProvider) ReportUsage(ctx context.Context, accountID uuid.UUID, usage UsageRecord) error {
	amount, err := p.calculator.Calculate(ctx, accountID, usage)
	if err != nil {
		return fmt.Errorf("calculate credits: %w", err)
	}
	if amount <= 0 {
		return nil
	}
	return p.credits.Deduct(ctx, accountID, amount, "run", usage.RunID, map[string]any{
		"tokens_in":   usage.TokensIn,
		"tokens_out":  usage.TokensOut,
		"tool_calls":  usage.ToolCalls,
		"duration_ms": usage.DurationMs,
	})
}

func (p *BuiltinBillingProvider) CheckQuota(ctx context.Context, accountID uuid.UUID, resource string) (bool, error) {
	return p.quotas.Check(ctx, accountID, resource)
}

func (p *BuiltinBillingProvider) HandleWebhook(ctx context.Context, provider string, payload []byte, signature string) error {
	return ErrWebhookNotSupported
}
